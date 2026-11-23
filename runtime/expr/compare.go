package expr

//go:generate go run gencomparefuncs.go

import (
	"bytes"
	"cmp"
	"slices"

	"github.com/superdb/super"
	"github.com/superdb/super/runtime/expr/cast"
	"github.com/superdb/super/runtime/sam/expr/coerce"
	samfunc "github.com/superdb/super/runtime/sam/expr/function"
	"github.com/superdb/super/scode"
	"github.com/superdb/super/vector"
)

type Compare struct {
	sctx    *super.Context
	defuse  *SuperDefuse
	opCode  int
	lhs     Evaluator
	rhs     Evaluator
	op      string
	lenient bool

	hasFusion samfunc.FusionChecker
	// comparisonTypes caches coerce.ComparisonType for pairs of operand
	// types.
	comparisonTypes map[[2]super.Type]comparisonType
}

type comparisonType struct {
	typ super.Type
	ok  bool
}

// NewCompare returns an evaluator for comparison operator op, for which
// values whose types have no coerce.ComparisonType are an error.
func NewCompare(sctx *super.Context, op string, lhs, rhs Evaluator) *Compare {
	return &Compare{sctx: sctx, defuse: NewSuperDefuse(sctx), opCode: vector.CompareOpFromString(op), lhs: lhs, rhs: rhs, op: op}
}

// NewLenientCompare is like NewCompare but values whose types can't be
// compared are unequal rather than an error, as for the in operator.
func NewLenientCompare(sctx *super.Context, op string) *Compare {
	c := NewCompare(sctx, op, nil, nil)
	c.lenient = true
	return c
}

func (c *Compare) Compare(vec0, vec1 vector.Any) vector.Any {
	return c.eval(vec0, vec1)
}

func (c *Compare) Eval(val vector.Any) vector.Any {
	lhs := c.defuse.Eval(c.lhs.Eval(val))
	rhs := c.defuse.Eval(c.rhs.Eval(val))
	return vector.Apply(vector.ApplyRipUnions, c.eval, lhs, rhs)
}

func (c *Compare) eval(vecs ...vector.Any) vector.Any {
	if vecs[0].Kind() == vector.KindOption && vecs[1].Kind() == vector.KindOption {
		return c.compareOptions(vecs[0], vecs[1])
	}
	// An error or null operand takes precedence over an unhandled option.
	if vec, ok := CatchFirst(c.sctx, CatchBasic|CatchOption, vecs, c.op); ok {
		return vec
	}
	return c.evalCaught(vecs...)
}

func (c *Compare) evalAllowingOption(vecs ...vector.Any) vector.Any {
	if vec, ok := CatchFirst(c.sctx, CatchBasic, vecs, c.op); ok {
		return vec
	}
	return c.evalCaught(vecs...)
}

// evalCaught is evalAllowingOption for operands already checked for errors
// and nulls.
func (c *Compare) evalCaught(vecs ...vector.Any) vector.Any {
	if vecs[0].Kind() == vector.KindOption || vecs[1].Kind() == vector.KindOption {
		return c.compareOptions(vecs[0], vecs[1])
	}
	lhs, rhs := vector.Super(vecs[0]), vector.Super(vecs[1])
	ltyp, rtyp := lhs.Type(), rhs.Type()
	lhs, rhs = vector.Under(lhs), vector.Under(rhs)
	if vec, ok := c.compareNone(lhs, rhs); ok {
		return vec
	}
	if vec, ok := c.compareMixedInts(lhs, rhs); ok {
		return vec
	}
	lhs0, rhs0 := lhs, rhs
	typ, lhs, rhs, ok, defused := c.coerce(lhs, rhs)
	if defused {
		return vector.Apply(vector.ApplyRipUnions, c.evalAllowingOption, lhs, rhs)
	}
	if !ok {
		if c.lenient {
			return vector.NewConstBool(c.opCode == vector.CompNE, lhs.Len())
		}
		return vector.NewStringError(c.sctx, coerce.TypeMismatch(c.op, ltyp, rtyp), lhs.Len())
	}
	if typ.Kind() != super.PrimitiveKind {
		return c.compareComplex(typ, lhs0, rhs0, lhs, rhs)
	}
	kind := lhs.Kind()
	if kind != rhs.Kind() || kind == vector.KindError {
		// A value couldn't be cast to the comparison type, leaving an
		// error in its place, so it's unequal.
		if lhs.Kind() == vector.KindError || rhs.Kind() == vector.KindError {
			return vector.NewConstBool(c.opCode == vector.CompNE, lhs.Len())
		}
		_, lok := lhs.(*vector.Dynamic)
		_, rok := rhs.(*vector.Dynamic)
		if !lok && !rok {
			panic("vector kind mismatch after coerce")
		}
		// Some values couldn't be cast, so handle each part as above.
		return vector.Apply(vector.ApplyRipUnions, func(vecs ...vector.Any) vector.Any {
			if vecs[0].Kind() == vector.KindError || vecs[1].Kind() == vector.KindError || vecs[0].Type() != vecs[1].Type() {
				return vector.NewConstBool(c.opCode == vector.CompNE, vecs[0].Len())
			}
			return c.evalAllowingOption(vecs...)
		}, lhs, rhs)
	}
	switch kind {
	case vector.KindBool:
		return c.compareBool(lhs, rhs)
	case vector.KindIP:
		return c.compareIPs(lhs, rhs)
	case vector.KindUUID:
		return c.compareUUIDs(lhs, rhs)
	case vector.KindNet:
		return c.compareNets(lhs, rhs)
	case vector.KindType:
		return c.compareTypeVals(lhs, rhs)
	}
	return c.compareForms(kind, lhs, rhs)
}

func (c *Compare) compareForms(kind vector.Kind, lhs, rhs vector.Any) vector.Any {
	lform, ok := vector.FormOf(lhs)
	if !ok {
		return vector.NewStringError(c.sctx, coerce.ErrIncompatibleTypes.Error(), lhs.Len())
	}
	rform, ok := vector.FormOf(rhs)
	if !ok {
		return vector.NewStringError(c.sctx, coerce.ErrIncompatibleTypes.Error(), lhs.Len())
	}
	f, ok := compareFuncs[vector.FuncCode(c.opCode, kind, lform, rform)]
	if !ok {
		return vector.NewConstBool(false, lhs.Len())
	}
	return f(lhs, rhs)
}

// compareOptions compares the payloads of some values while an option none
// equals only another option none.  An option is never equal to a non-option.
func (c *Compare) compareOptions(lhs, rhs vector.Any) vector.Any {
	return vector.Apply(vector.ApplyRipOptions, func(vecs ...vector.Any) vector.Any {
		if vecs[0].Kind() != vector.KindOption || vecs[1].Kind() != vector.KindOption {
			return vector.NewConstBool(c.opCode == vector.CompNE, vecs[0].Len())
		}
		lhs := vector.UnderOption(vecs[0]).Any
		rhs := vector.UnderOption(vecs[1]).Any
		if vec, ok := c.compareNone(lhs, rhs); ok {
			return vec
		}
		return vector.Apply(vector.ApplyRipFusions|vector.ApplyRipUnions, c.evalAllowingOption, lhs, rhs)
	}, lhs, rhs)
}

// compareNone compares lhs and rhs if either is none, which equals only
// none and has no order.
func (c *Compare) compareNone(lhs, rhs vector.Any) (vector.Any, bool) {
	lhsNone, rhsNone := lhs.Kind() == vector.KindNone, rhs.Kind() == vector.KindNone
	if !lhsNone && !rhsNone {
		return nil, false
	}
	eq := lhsNone && rhsNone
	return vector.NewConstBool(c.opCode == vector.CompEQ && eq || c.opCode == vector.CompNE && !eq, lhs.Len()), true
}

// compareMixedInts compares an unsigned integer with a signed integer that
// is no wider, for which the signed type they would otherwise be cast to
// can't hold every unsigned value (e.g., a uint64 greater than the maximum
// int64).  It returns false for any other types.
func (c *Compare) compareMixedInts(lhs, rhs vector.Any) (vector.Any, bool) {
	isUnsigned := func(id int) bool { return id >= super.IDUint8 && id <= super.IDUint64 }
	isSigned := func(id int) bool { return id >= super.IDInt8 && id <= super.IDInt64 }
	lid, rid := lhs.Type().ID(), rhs.Type().ID()
	var flip bool
	switch {
	case isUnsigned(lid) && isSigned(rid):
	case isSigned(lid) && isUnsigned(rid):
		lhs, rhs, flip = rhs, lhs, true
	default:
		return nil, false
	}
	if coerce.UintBits(lhs.Type()) < coerce.IntBits(rhs.Type()) {
		return nil, false
	}
	n := lhs.Len()
	if _, ok := rhs.(*vector.Const); ok && n > 0 && vector.IntValue(rhs, 0) >= 0 {
		// The signed constant fits the unsigned type, so use the
		// vectorized comparison.
		rhs = cast.To(c.sctx, rhs, lhs.Type())
		if flip {
			lhs, rhs = rhs, lhs
		}
		return c.compareForms(vector.KindUint, lhs, rhs), true
	}
	out := vector.NewFalse(n)
	for i := range n {
		v := orderUintInt(vector.UintValue(lhs, i), vector.IntValue(rhs, i))
		if flip {
			v = -v
		}
		if isCompareOpSatisfied(c.opCode, v) {
			out.Set(i)
		}
	}
	return out, true
}

// coerce returns the comparison type of lhs and rhs and them cast as needed
// to it or false if their types can't be compared.  If their types contain
// fusion types that leave the comparison type unknown, it instead returns them
// defused, with defused true, to be compared again.
func (c *Compare) coerce(lhs, rhs vector.Any) (super.Type, vector.Any, vector.Any, bool, bool) {
	ltyp, rtyp := lhs.Type(), rhs.Type()
	if ltyp == rtyp {
		return ltyp, lhs, rhs, true, false
	}
	key := [2]super.Type{ltyp, rtyp}
	ct, cached := c.comparisonTypes[key]
	if !cached {
		ct.typ, ct.ok = coerce.ComparisonType(c.sctx, c.hasFusion.Check, ltyp, rtyp)
		if c.comparisonTypes == nil {
			c.comparisonTypes = make(map[[2]super.Type]comparisonType)
		}
		c.comparisonTypes[key] = ct
	}
	typ, ok := ct.typ, ct.ok
	if !ok {
		return nil, lhs, rhs, false, false
	}
	if coerce.IsUnknown(typ) {
		if c.hasFusion.Check(ltyp) || c.hasFusion.Check(rtyp) {
			lhs, rhs := c.defuse.Eval(lhs), c.defuse.Eval(rhs)
			// Compare again only if defusing removed the fusion types so
			// this can't recur forever.
			if c.defused(lhs) && c.defused(rhs) {
				return nil, lhs, rhs, true, true
			}
		}
		return nil, lhs, rhs, false, false
	}
	to := cast.To
	if typ.Kind() != super.PrimitiveKind {
		to = cast.Cast
	}
	if ltyp != typ {
		lhs = to(c.sctx, lhs, typ)
	}
	if rtyp != typ {
		rhs = to(c.sctx, rhs, typ)
	}
	return typ, vector.Under(lhs), vector.Under(rhs), true, false
}

// defused reports whether vec, the result of defusing a vector, has no fusion
// types (or is a Dynamic, whose parts are checked when compared).
func (c *Compare) defused(vec vector.Any) bool {
	return vector.IsDynamic(vec) || !c.hasFusion.Check(vec.Type())
}

// compareBool orders false before true.
func (c *Compare) compareBool(lhs, rhs vector.Any) vector.Any {
	out := vector.NewFalse(lhs.Len())
	if lhs, ok := lhs.(*vector.Bool); ok {
		if rhs, ok := rhs.(*vector.Bool); ok {
			lhsBits := lhs.GetBits()
			rhsBits := rhs.GetBits()
			outBits := out.GetBits()
			for i := range outBits {
				l, r := lhsBits[i], rhsBits[i]
				switch c.opCode {
				case vector.CompEQ:
					outBits[i] = ^(l ^ r)
				case vector.CompNE:
					outBits[i] = l ^ r
				case vector.CompLT:
					outBits[i] = ^l & r
				case vector.CompLE:
					outBits[i] = ^l | r
				case vector.CompGT:
					outBits[i] = l &^ r
				case vector.CompGE:
					outBits[i] = l | ^r
				}
			}
			return out
		}
	}
	for i := range lhs.Len() {
		var l, r int
		if vector.BoolValue(lhs, i) {
			l = 1
		}
		if vector.BoolValue(rhs, i) {
			r = 1
		}
		if isCompareOpSatisfied(c.opCode, l-r) {
			out.Set(i)
		}
	}
	return out
}

func (c *Compare) compareIPs(lhs, rhs vector.Any) vector.Any {
	out := vector.NewFalse(lhs.Len())
	for i := range lhs.Len() {
		l := vector.IPValue(lhs, i)
		r := vector.IPValue(rhs, i)
		if isCompareOpSatisfied(c.opCode, l.Compare(r)) {
			out.Set(i)
		}
	}
	return out
}

func (c *Compare) compareUUIDs(lhs, rhs vector.Any) vector.Any {
	out := vector.NewFalse(lhs.Len())
	for i := range lhs.Len() {
		if isCompareOpSatisfied(c.opCode, vector.UUIDValue(lhs, i).Compare(vector.UUIDValue(rhs, i))) {
			out.Set(i)
		}
	}
	return out
}

func (c *Compare) compareNets(lhs, rhs vector.Any) vector.Any {
	out := vector.NewFalse(lhs.Len())
	for i := range lhs.Len() {
		l := vector.NetValue(lhs, i)
		r := vector.NetValue(rhs, i)
		if isCompareOpSatisfied(c.opCode, l.Compare(r)) {
			out.Set(i)
		}
	}
	return out
}

func isCompareOpSatisfied(opCode, i int) bool {
	switch opCode {
	case vector.CompLT:
		return i < 0
	case vector.CompLE:
		return i <= 0
	case vector.CompGT:
		return i > 0
	case vector.CompGE:
		return i >= 0
	case vector.CompEQ:
		return i == 0
	case vector.CompNE:
		return i != 0
	}
	panic(opCode)
}

// compareTypeVals orders type values by super.CompareTypes.
func (c *Compare) compareTypeVals(lhs, rhs vector.Any) vector.Any {
	out := vector.NewFalse(lhs.Len())
	for i := range lhs.Len() {
		l := vector.TypeValueValue(lhs, i)
		r := vector.TypeValueValue(rhs, i)
		if isCompareOpSatisfied(c.opCode, super.CompareTypes(l, r)) {
			out.Set(i)
		}
	}
	return out
}

// compareComplex compares values of complex types, which have been cast to
// their comparison type typ as lhs and rhs.  A value that couldn't be cast
// (e.g., one with a uint64 too large for an int64 in typ) is compared as it
// was in lhs0 and rhs0 instead.
func (c *Compare) compareComplex(typ super.Type, lhs0, rhs0, lhs, rhs vector.Any) vector.Any {
	var lb, rb scode.Builder
	out := vector.NewFalse(lhs.Len())
	for i := range lhs.Len() {
		l := vector.ValueAt(&lb, lhs, i)
		r := vector.ValueAt(&rb, rhs, i)
		var v int
		var ok bool
		if l.Type() == typ && r.Type() == typ {
			v, ok = orderValues(c.sctx, typ, l.Bytes(), r.Bytes())
		} else {
			l, r = vector.ValueAt(&lb, lhs0, i), vector.ValueAt(&rb, rhs0, i)
			v, ok = orderMixed(c.sctx, l.Type(), l.Bytes(), r.Type(), r.Bytes())
		}
		// Values with no order are unequal.
		if ok && isCompareOpSatisfied(c.opCode, v) || !ok && c.opCode == vector.CompNE {
			out.Set(i)
		}
	}
	return out
}

// orderValues compares a and b, which have type typ, returning false if they
// are unequal but have no order (e.g., values of different types in a union).
// Null is ordered after any other value, and type values are ordered by
// super.CompareTypes.  Arrays are ordered by their first elements that aren't
// equal, with the shorter one first when it has no such element, sets likewise
// with their elements in ascending order, maps likewise with their entries in
// ascending order of their keys, and records by their first fields that aren't
// equal.
func orderValues(sctx *super.Context, typ super.Type, a, b scode.Bytes) (int, bool) {
	if a == nil || b == nil {
		return cmp.Compare(btoi(a == nil), btoi(b == nil)), true
	}
	if bytes.Equal(a, b) {
		return 0, true
	}
	switch typ := super.TypeUnder(typ).(type) {
	case *super.TypeUnion:
		atyp, a := typ.Untag(a)
		btyp, b := typ.Untag(b)
		anull, bnull := atyp == super.TypeNull, btyp == super.TypeNull
		if anull || bnull {
			return cmp.Compare(btoi(anull), btoi(bnull)), true
		}
		if atyp != btyp {
			return 0, false
		}
		return orderValues(sctx, atyp, a, b)
	case *super.TypeArray:
		return orderLists(sctx, typ.Type, a.Iter(), typ.Type, b.Iter())
	case *super.TypeSet:
		return orderSets(sctx, typ.Type, a, typ.Type, b)
	case *super.TypeRecord:
		return orderRecords(sctx, typ, a, typ, b)
	case *super.TypeMap:
		return orderMaps(sctx, typ, a, typ, b)
	case *super.TypeOption:
		atyp, a := typ.Decode(a)
		btyp, b := typ.Decode(b)
		if atyp == nil || btyp == nil {
			// None equals only none, which has the same bytes.
			return 0, false
		}
		return orderValues(sctx, atyp, a, b)
	}
	switch id := typ.ID(); {
	case super.IsSigned(id):
		return cmp.Compare(super.DecodeInt(a), super.DecodeInt(b)), true
	case super.IsUnsigned(id):
		return cmp.Compare(super.DecodeUint(a), super.DecodeUint(b)), true
	case super.IsFloat(id):
		return coerce.CompareFloats(super.DecodeFloat(a), super.DecodeFloat(b)), true
	case id == super.IDBool:
		return cmp.Compare(btoi(super.DecodeBool(a)), btoi(super.DecodeBool(b))), true
	case id == super.IDString, id == super.IDBytes:
		return bytes.Compare(a, b), true
	case id == super.IDIP:
		return super.DecodeIP(a).Compare(super.DecodeIP(b)), true
	case id == super.IDNet:
		return super.DecodeNet(a).Compare(super.DecodeNet(b)), true
	case id == super.IDUUID:
		return super.DecodeUUID(a).Compare(super.DecodeUUID(b)), true
	case id == super.IDType:
		atyp, aerr := sctx.LookupByValue(a)
		btyp, berr := sctx.LookupByValue(b)
		if aerr != nil || berr != nil {
			return 0, false
		}
		return super.CompareTypes(atyp, btyp), true
	}
	return 0, false
}

// orderMixed is like orderValues for a of type atyp and b of type btyp, which
// are the same type or have a comparison type (e.g., when one of them
// couldn't be cast to it).  Integers and floats of different types are
// ordered by value.
func orderMixed(sctx *super.Context, atyp super.Type, a scode.Bytes, btyp super.Type, b scode.Bytes) (int, bool) {
	if a == nil || b == nil {
		return cmp.Compare(btoi(a == nil), btoi(b == nil)), true
	}
	atyp, btyp = super.TypeUnder(atyp), super.TypeUnder(btyp)
	if atyp == btyp {
		return orderValues(sctx, atyp, a, b)
	}
	au, aUnion := atyp.(*super.TypeUnion)
	bu, bUnion := btyp.(*super.TypeUnion)
	if aUnion || bUnion {
		if aUnion {
			atyp, a = au.Untag(a)
		}
		if bUnion {
			btyp, b = bu.Untag(b)
		}
		anull, bnull := atyp == super.TypeNull, btyp == super.TypeNull
		if anull || bnull {
			return cmp.Compare(btoi(anull), btoi(bnull)), true
		}
		// A value keeps its type in a union.
		if atyp != btyp {
			return 0, false
		}
		return orderValues(sctx, atyp, a, b)
	}
	switch at := atyp.(type) {
	case *super.TypeArray:
		if bt, ok := btyp.(*super.TypeArray); ok {
			return orderLists(sctx, at.Type, a.Iter(), bt.Type, b.Iter())
		}
	case *super.TypeSet:
		if bt, ok := btyp.(*super.TypeSet); ok {
			return orderSets(sctx, at.Type, a, bt.Type, b)
		}
	case *super.TypeRecord:
		if bt, ok := btyp.(*super.TypeRecord); ok {
			return orderRecords(sctx, at, a, bt, b)
		}
	case *super.TypeMap:
		if bt, ok := btyp.(*super.TypeMap); ok {
			return orderMaps(sctx, at, a, bt, b)
		}
	case *super.TypeOption:
		if bt, ok := btyp.(*super.TypeOption); ok {
			atyp, a := at.Decode(a)
			btyp, b := bt.Decode(b)
			if atyp == nil || btyp == nil {
				// None equals only none.
				return 0, atyp == nil && btyp == nil
			}
			return orderMixed(sctx, atyp, a, btyp, b)
		}
	}
	if aid, bid := atyp.ID(), btyp.ID(); isNumeric(aid) && isNumeric(bid) {
		return orderNumbers(aid, a, bid, b)
	}
	return 0, false
}

// orderLists orders the elements of arrays or sets, which have types atyp and
// btyp, by their first elements that aren't equal.
func orderLists(sctx *super.Context, atyp super.Type, ait scode.Iter, btyp super.Type, bit scode.Iter) (int, bool) {
	for !ait.Done() && !bit.Done() {
		v, ok := orderMixed(sctx, atyp, ait.Next(), btyp, bit.Next())
		if !ok || v != 0 {
			return v, ok
		}
	}
	return cmp.Compare(btoi(!ait.Done()), btoi(!bit.Done())), true
}

// orderSets orders sets like arrays with their elements in ascending order
// rather than in the order in which they're stored (i.e., the order of their
// encodings), which differs for values that are equal but encoded
// differently (e.g., 0.0 and -0.0) or of different types.
func orderSets(sctx *super.Context, atyp super.Type, a scode.Bytes, btyp super.Type, b scode.Bytes) (int, bool) {
	as, bs := sortedElems(sctx, atyp, a), sortedElems(sctx, btyp, b)
	for i := range min(len(as), len(bs)) {
		v, ok := orderMixed(sctx, atyp, as[i], btyp, bs[i])
		if !ok || v != 0 {
			return v, ok
		}
	}
	return cmp.Compare(len(as), len(bs)), true
}

// sortedElems returns the elements of set body b, whose elements have type
// typ, in ascending order, with elements that have no order in the order of
// their encodings.
func sortedElems(sctx *super.Context, typ super.Type, b scode.Bytes) []scode.Bytes {
	var elems []scode.Bytes
	for it := b.Iter(); !it.Done(); {
		elems = append(elems, it.Next())
	}
	sortByValue(sctx, typ, elems, func(elem scode.Bytes) scode.Bytes { return elem })
	return elems
}

// sortByValue sorts s in ascending order of key(s[i]), which has type typ, with
// keys that have no order in the order of their encodings.
func sortByValue[T any](sctx *super.Context, typ super.Type, s []T, key func(T) scode.Bytes) {
	slices.SortStableFunc(s, func(x, y T) int {
		kx, ky := key(x), key(y)
		if v, ok := orderValues(sctx, typ, kx, ky); ok && v != 0 {
			return v
		}
		return bytes.Compare(kx, ky)
	})
}

// orderRecords orders records a and b, which have types atyp and btyp with
// the same field names in the same order, by their first fields from left to
// right that aren't equal.
func orderRecords(sctx *super.Context, atyp *super.TypeRecord, a scode.Bytes, btyp *super.TypeRecord, b scode.Bytes) (int, bool) {
	if len(atyp.Fields) != len(btyp.Fields) {
		return 0, false
	}
	ait, bit := a.Iter(), b.Iter()
	for i, f := range atyp.Fields {
		if v, ok := orderMixed(sctx, f.Type, ait.Next(), btyp.Fields[i].Type, bit.Next()); !ok || v != 0 {
			return v, ok
		}
	}
	return 0, true
}

// orderMaps orders maps a and b, which have types atyp and btyp, like sets of
// entries with their entries in ascending order of their keys, by their first
// entries whose keys or values aren't equal.
func orderMaps(sctx *super.Context, atyp *super.TypeMap, a scode.Bytes, btyp *super.TypeMap, b scode.Bytes) (int, bool) {
	as, bs := sortedEntries(sctx, atyp.KeyType, a), sortedEntries(sctx, btyp.KeyType, b)
	for i := range min(len(as), len(bs)) {
		if v, ok := orderMixed(sctx, atyp.KeyType, as[i].key, btyp.KeyType, bs[i].key); !ok || v != 0 {
			return v, ok
		}
		if v, ok := orderMixed(sctx, atyp.ValType, as[i].val, btyp.ValType, bs[i].val); !ok || v != 0 {
			return v, ok
		}
	}
	return cmp.Compare(len(as), len(bs)), true
}

type mapEntry struct {
	key, val scode.Bytes
}

// sortedEntries returns the entries of map body b, whose keys have type
// keyType, in ascending order of their keys.
func sortedEntries(sctx *super.Context, keyType super.Type, b scode.Bytes) []mapEntry {
	var entries []mapEntry
	for it := b.Iter(); !it.Done(); {
		entries = append(entries, mapEntry{it.Next(), it.Next()})
	}
	sortByValue(sctx, keyType, entries, func(e mapEntry) scode.Bytes { return e.key })
	return entries
}

// orderNumbers orders integers and floats of different types by value.
func orderNumbers(aid int, a scode.Bytes, bid int, b scode.Bytes) (int, bool) {
	switch {
	case super.IsFloat(aid) || super.IsFloat(bid):
		return coerce.CompareFloats(toFloat(aid, a), toFloat(bid, b)), true
	case super.IsUnsigned(aid) && super.IsUnsigned(bid):
		return cmp.Compare(super.DecodeUint(a), super.DecodeUint(b)), true
	case super.IsSigned(aid) && super.IsSigned(bid):
		return cmp.Compare(super.DecodeInt(a), super.DecodeInt(b)), true
	case super.IsUnsigned(aid):
		return orderUintInt(super.DecodeUint(a), super.DecodeInt(b)), true
	default:
		return -orderUintInt(super.DecodeUint(b), super.DecodeInt(a)), true
	}
}

func orderUintInt(u uint64, s int64) int {
	if s < 0 {
		return 1
	}
	return cmp.Compare(u, uint64(s))
}

func toFloat(id int, b scode.Bytes) float64 {
	switch {
	case super.IsFloat(id):
		return super.DecodeFloat(b)
	case super.IsUnsigned(id):
		return float64(super.DecodeUint(b))
	}
	return float64(super.DecodeInt(b))
}

func isNumeric(id int) bool {
	return super.IsInteger(id) || super.IsFloat(id)
}

func btoi(b bool) int {
	if b {
		return 1
	}
	return 0
}

type isNull struct {
	expr Evaluator
}

func NewIsNull(e Evaluator) Evaluator {
	return &isNull{e}
}

func (i *isNull) Eval(this vector.Any) vector.Any {
	return vector.Apply(vector.ApplyRipUnions|vector.ApplyRipFusions, i.eval, i.expr.Eval(this))
}

func (i *isNull) eval(vecs ...vector.Any) vector.Any {
	k := vecs[0].Kind()
	if k == vector.KindError {
		return vecs[0]
	}
	return vector.NewConstBool(k == vector.KindNull, vecs[0].Len())
}
