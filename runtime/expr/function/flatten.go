package function

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/superdb/super"
	"github.com/superdb/super/scode"
	"github.com/superdb/super/sup"
	"github.com/superdb/super/vector"
)

// A path is an array whose elements navigate into a value: a string selects
// a record field, an integer selects an array (or set) element, and any other
// value selects a map entry by key.  A path-value collection is an array of
// {path,value} records, each giving the path to a leaf of a value and the leaf
// value.  A leaf is a value that isn't a record, array, set, or map, or is an
// empty record, array, set, or map.  flatten turns a value into its
// path-value collection and unflatten turns a path-value collection back into
// a value.

// maxUnflattenArrayLen limits the length of an array created by unflatten
// (e.g., from a path with a large index).
const maxUnflattenArrayLen = 1 << 20

type flatten struct {
	sctx *super.Context
	b    scode.Builder
	pvs  []pathValue
}

type pathValue struct {
	path []super.Value
	val  super.Value
}

func newFlatten(sctx *super.Context) *flatten {
	return &flatten{sctx: sctx}
}

func (f *flatten) Call(args ...vector.Any) vector.Any {
	vec := args[0]
	out := vector.NewDynamicValueBuilder()
	for i := range vec.Len() {
		out.Write(f.flatten(vector.ValueAt(&f.b, vec, i)))
	}
	return out.Build(f.sctx)
}

// flatten returns the path-value collection for val or val itself if val
// isn't a record, array, set, or map.
func (f *flatten) flatten(val super.Value) super.Value {
	if !isContainer(untag(val)) {
		return val
	}
	f.pvs = f.pvs[:0]
	f.walk(nil, val)
	var vals []super.Value
	for _, pv := range f.pvs {
		path := super.NewValue(f.sctx.LookupTypeArray(super.TypeString), scode.Bytes{})
		if len(pv.path) > 0 {
			path = buildArray(f.sctx, pv.path)
		}
		recType := f.sctx.MustLookupTypeRecord([]super.Field{
			super.NewField("path", path.Type()),
			super.NewField("value", pv.val.Type()),
		})
		b := scode.Append(nil, path.Bytes())
		b = scode.Append(b, pv.val.Bytes())
		vals = append(vals, super.NewValue(recType, b))
	}
	return buildArray(f.sctx, vals)
}

func (f *flatten) walk(path []super.Value, val super.Value) {
	if !isContainer(untag(val)) {
		// A leaf keeps its union or option type, e.g., so that an optional
		// field is optional after unflatten.
		f.leaf(path, val)
		return
	}
	val = untag(val)
	n := len(f.pvs)
	switch typ := super.TypeUnder(val.Type()).(type) {
	case *super.TypeRecord:
		it := val.Bytes().Iter()
		for _, field := range typ.Fields {
			fval := super.NewValue(field.Type, it.Next())
			if fval.IsNone() {
				// An absent optional field has no leaf.
				continue
			}
			f.walk(extend(path, super.NewString(field.Name)), fval)
		}
	case *super.TypeArray, *super.TypeSet:
		inner := super.InnerType(typ)
		var k int64
		for it := val.Bytes().Iter(); !it.Done(); k++ {
			// The union type of the elements of an array with mixed types
			// isn't part of the type of the element, unlike a union type
			// of a record field, so it's dropped.
			f.walk(extend(path, super.NewInt64(k)), untagUnion(super.NewValue(inner, it.Next())))
		}
	case *super.TypeMap:
		for it := val.Bytes().Iter(); !it.Done(); {
			key := untag(super.NewValue(typ.KeyType, it.Next()))
			f.walk(extend(path, key), untagUnion(super.NewValue(typ.ValType, it.Next())))
		}
	}
	if len(f.pvs) == n {
		// An empty container is a leaf.
		f.leaf(path, val)
	}
}

func (f *flatten) leaf(path []super.Value, val super.Value) {
	f.pvs = append(f.pvs, pathValue{path, val.Copy()})
}

// extend returns a copy of path with elem appended.
func extend(path []super.Value, elem super.Value) []super.Value {
	out := make([]super.Value, len(path), len(path)+1)
	copy(out, path)
	return append(out, elem.Copy())
}

// untag returns val without any union types (and options), i.e., the
// underlying value of a union or some value.  A none value is returned as is.
func untag(val super.Value) super.Value {
	for {
		switch typ := super.TypeUnder(val.Type()).(type) {
		case *super.TypeUnion:
			if val.Bytes() == nil {
				return val
			}
			val = super.NewValue(typ.Untag(val.Bytes()))
		case *super.TypeOption:
			inner, bytes := typ.Decode(val.Bytes())
			if inner == nil {
				return val
			}
			val = super.NewValue(inner, bytes)
		default:
			return val
		}
	}
}

func isContainer(val super.Value) bool {
	if val.Bytes() == nil {
		return false
	}
	switch super.TypeUnder(val.Type()).(type) {
	case *super.TypeRecord, *super.TypeArray, *super.TypeSet, *super.TypeMap:
		return true
	}
	return false
}

// buildArray returns an array of vals, whose element type is the type of the
// vals or the union of their types.  The vals must not have union types.
func buildArray(sctx *super.Context, vals []super.Value) super.Value {
	inner := typeOf(sctx, vals)
	b := scode.Bytes{}
	for _, val := range vals {
		b = appendAs(b, inner, val)
	}
	return super.NewValue(sctx.LookupTypeArray(inner), b)
}

// typeOf returns the type of vals or the union of their types.  An empty vals
// has type null.  If vals have different types and any has a union type, then
// typeOf replaces those vals with their untagged values since a union can't
// contain a union.
func typeOf(sctx *super.Context, vals []super.Value) super.Type {
	types := uniqueTypes(vals)
	if len(types) > 1 && slices.ContainsFunc(types, isUnion) {
		for k, val := range vals {
			if isUnion(val.Type()) {
				vals[k] = untagUnion(val)
			}
		}
		types = uniqueTypes(vals)
	}
	switch len(types) {
	case 0:
		return super.TypeNull
	case 1:
		return types[0]
	}
	return sctx.MustLookupTypeUnion(types)
}

func uniqueTypes(vals []super.Value) []super.Type {
	var types []super.Type
	for _, val := range vals {
		types = append(types, val.Type())
	}
	return super.UniqueTypes(types)
}

func isUnion(typ super.Type) bool {
	_, ok := super.TypeUnder(typ).(*super.TypeUnion)
	return ok
}

// untagUnion returns val without any union types.
func untagUnion(val super.Value) super.Value {
	for {
		union, ok := super.TypeUnder(val.Type()).(*super.TypeUnion)
		if !ok {
			return val
		}
		val = super.NewValue(union.Untag(val.Bytes()))
	}
}

// appendAs appends val to b as a value of typ, which is either the type of val
// or a union containing it, and returns the extended b.
func appendAs(b scode.Bytes, typ super.Type, val super.Value) scode.Bytes {
	return scode.Append(b, bodyAs(typ, val))
}

// bodyAs returns the body of val as a value of typ, which is either the type
// of val or a union containing it.
func bodyAs(typ super.Type, val super.Value) scode.Bytes {
	if union, ok := typ.(*super.TypeUnion); ok && val.Type() != typ {
		var b scode.Builder
		super.BuildUnion(&b, union.TagOf(val.Type()), val.Bytes())
		return b.Bytes().Body()
	}
	return val.Bytes()
}

type unflatten struct {
	sctx *super.Context
	b    scode.Builder
}

func newUnflatten(sctx *super.Context) *unflatten {
	return &unflatten{sctx: sctx}
}

func (u *unflatten) Call(args ...vector.Any) vector.Any {
	vec := args[0]
	out := vector.NewDynamicValueBuilder()
	for i := range vec.Len() {
		val := vector.ValueAt(&u.b, vec, i)
		result, err := u.unflatten(val)
		if err != nil {
			result = u.sctx.WrapError("unflatten: "+err.Error(), val)
		}
		out.Write(result)
	}
	return out.Build(u.sctx)
}

// unflatten returns the value described by the path-value collection val or
// val itself if val isn't an array or set.
func (u *unflatten) unflatten(in super.Value) (super.Value, error) {
	val := untag(in)
	if val.Bytes() == nil {
		return in, nil
	}
	var inner super.Type
	switch typ := super.TypeUnder(val.Type()).(type) {
	case *super.TypeArray:
		inner = typ.Type
	case *super.TypeSet:
		inner = typ.Type
	default:
		return in, nil
	}
	root := &node{}
	for it := val.Bytes().Iter(); !it.Done(); {
		path, leaf, err := parsePathValue(untag(super.NewValue(inner, it.Next())))
		if err != nil {
			return super.Value{}, err
		}
		if err := root.set(path, leaf); err != nil {
			return super.Value{}, err
		}
	}
	if root.kind == nodeEmpty {
		return super.NewValue(u.sctx.MustLookupTypeRecord(nil), scode.Bytes{}), nil
	}
	return root.build(u.sctx)
}

// parsePathValue returns the path and value of a {path,value} record.
func parsePathValue(val super.Value) ([]super.Value, super.Value, error) {
	rec, ok := super.TypeUnder(val.Type()).(*super.TypeRecord)
	if !ok || val.Bytes() == nil {
		return nil, super.Value{}, fmt.Errorf("element is not a {path,value} record: %s", sup.FormatValue(val))
	}
	pathVal := val.Deref("path")
	leaf := val.Deref("value")
	if pathVal == nil || leaf == nil {
		return nil, super.Value{}, fmt.Errorf("element is not a {path,value} record: %s", sup.FormatType(rec))
	}
	path, err := ParsePath(*pathVal)
	if err != nil {
		return nil, super.Value{}, err
	}
	return path, leaf.Copy(), nil
}

// ParsePath returns the elements of path, which is either an array (or set)
// of path elements or a string abbreviation of a path, e.g., "x.1.y" for
// ["x",1,"y"], where an element of the abbreviation that is an unsigned
// decimal number is an integer.  An abbreviation can't express a field name
// containing a period.
func ParsePath(path super.Value) ([]super.Value, error) {
	path = untag(path)
	switch typ := super.TypeUnder(path.Type()).(type) {
	case *super.TypeOfString:
		return parsePathString(super.DecodeString(path.Bytes())), nil
	case *super.TypeArray, *super.TypeSet:
		var elems []super.Value
		inner := super.InnerType(typ)
		for it := path.Bytes().Iter(); !it.Done(); {
			elems = append(elems, untag(super.NewValue(inner, it.Next())).Copy())
		}
		return elems, nil
	}
	return nil, fmt.Errorf("path must be an array or string: %s", sup.FormatValue(path))
}

// ParsePaths returns the paths of a path collection, which is either an array
// (or set) of paths, each of which is an array (not a string abbreviation), or
// a string abbreviation of a collection, e.g., "x.1.y,a.b" for
// [["x",1,"y"],["a","b"]].  An abbreviation can't express a field name
// containing a period or comma.
func ParsePaths(paths super.Value) ([][]super.Value, error) {
	paths = untag(paths)
	switch typ := super.TypeUnder(paths.Type()).(type) {
	case *super.TypeOfString:
		var out [][]super.Value
		for s := range strings.SplitSeq(super.DecodeString(paths.Bytes()), ",") {
			out = append(out, parsePathString(s))
		}
		return out, nil
	case *super.TypeArray, *super.TypeSet:
		var out [][]super.Value
		inner := super.InnerType(typ)
		for it := paths.Bytes().Iter(); !it.Done(); {
			path := untag(super.NewValue(inner, it.Next()))
			switch super.TypeUnder(path.Type()).(type) {
			case *super.TypeArray, *super.TypeSet:
			default:
				return nil, fmt.Errorf("path in a path collection must be an array: %s", sup.FormatValue(path))
			}
			elems, err := ParsePath(path)
			if err != nil {
				return nil, err
			}
			out = append(out, elems)
		}
		return out, nil
	}
	return nil, fmt.Errorf("path collection must be an array or string: %s", sup.FormatValue(paths))
}

func parsePathString(s string) []super.Value {
	var elems []super.Value
	for elem := range strings.SplitSeq(s, ".") {
		if isDecimal(elem) {
			if k, err := strconv.ParseInt(elem, 10, 64); err == nil {
				elems = append(elems, super.NewInt64(k))
				continue
			}
		}
		elems = append(elems, super.NewString(elem))
	}
	return elems
}

func isDecimal(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

type nodeKind int

const (
	nodeEmpty nodeKind = iota
	nodeLeaf
	nodeRecord
	nodeArray
	nodeMap
)

// node is a value under construction by unflatten.
type node struct {
	kind nodeKind
	leaf super.Value
	// keys and children are the field names (as string values), array
	// indexes (as int64 values), or map keys of the children in the order
	// of their first appearance.
	keys     []super.Value
	children []*node
	index    map[string]int
}

func kindOfElem(elem super.Value) nodeKind {
	id := super.TypeUnder(elem.Type()).ID()
	switch {
	case id == super.IDString:
		return nodeRecord
	case super.IsInteger(id):
		return nodeArray
	}
	return nodeMap
}

// set sets the value at path, replacing any value already there.
func (n *node) set(path []super.Value, leaf super.Value) error {
	if len(path) == 0 {
		*n = node{kind: nodeLeaf, leaf: leaf}
		return nil
	}
	elem := path[0]
	kind := kindOfElem(elem)
	if n.kind == nodeEmpty || n.kind == nodeLeaf {
		*n = node{kind: kind, index: make(map[string]int)}
	} else if n.kind != kind {
		return fmt.Errorf("path element %s conflicts with %s of earlier path", sup.FormatValue(elem), kindName(n.kind))
	}
	key, err := n.key(elem)
	if err != nil {
		return err
	}
	id := fmt.Sprintf("%d:%s", key.Type().ID(), key.Bytes())
	k, ok := n.index[id]
	if !ok {
		k = len(n.children)
		n.index[id] = k
		n.keys = append(n.keys, key)
		n.children = append(n.children, &node{})
	}
	return n.children[k].set(path[1:], leaf)
}

// key returns elem as a field name, array index (as an int64), or map key.
func (n *node) key(elem super.Value) (super.Value, error) {
	switch n.kind {
	case nodeRecord:
		return super.NewString(super.DecodeString(elem.Bytes())), nil
	case nodeArray:
		var k int64
		if super.IsSigned(super.TypeUnder(elem.Type()).ID()) {
			k = super.DecodeInt(elem.Bytes())
		} else {
			u := super.DecodeUint(elem.Bytes())
			if u >= maxUnflattenArrayLen {
				k = maxUnflattenArrayLen
			} else {
				k = int64(u)
			}
		}
		if k < 0 {
			return super.Value{}, fmt.Errorf("negative array index %d", k)
		}
		if k >= maxUnflattenArrayLen {
			return super.Value{}, fmt.Errorf("array index %s exceeds limit of %d", sup.FormatValue(elem), maxUnflattenArrayLen-1)
		}
		return super.NewInt64(k), nil
	}
	return elem, nil
}

func kindName(kind nodeKind) string {
	switch kind {
	case nodeRecord:
		return "record field"
	case nodeArray:
		return "array index"
	}
	return "map key"
}

func (n *node) build(sctx *super.Context) (super.Value, error) {
	switch n.kind {
	case nodeLeaf:
		return n.leaf, nil
	case nodeRecord:
		var fields []super.Field
		b := scode.Bytes{}
		for k, child := range n.children {
			val, err := child.build(sctx)
			if err != nil {
				return super.Value{}, err
			}
			fields = append(fields, super.NewField(super.DecodeString(n.keys[k].Bytes()), val.Type()))
			b = scode.Append(b, val.Bytes())
		}
		typ, err := sctx.LookupTypeRecord(fields)
		if err != nil {
			return super.Value{}, err
		}
		return super.NewValue(typ, b), nil
	case nodeArray:
		return n.buildArray(sctx)
	case nodeMap:
		var keys, vals []super.Value
		for k, child := range n.children {
			val, err := child.build(sctx)
			if err != nil {
				return super.Value{}, err
			}
			keys = append(keys, n.keys[k])
			vals = append(vals, val)
		}
		keyType := typeOf(sctx, keys)
		valType := typeOf(sctx, vals)
		b := scode.Bytes{}
		for k := range keys {
			b = appendAs(b, keyType, keys[k])
			b = appendAs(b, valType, vals[k])
		}
		return super.NewValue(sctx.LookupTypeMap(keyType, valType), super.NormalizeMap(b)), nil
	}
	return super.Value{}, errors.New("empty path")
}

// buildArray builds an array whose elements are the children of n at their
// indexes.  If there are missing indexes, the elements are options and the
// missing elements are none.
func (n *node) buildArray(sctx *super.Context) (super.Value, error) {
	var length int64
	for _, key := range n.keys {
		length = max(length, super.DecodeInt(key.Bytes())+1)
	}
	// slots maps an index to its position in vals or -1 if it's missing.
	slots := make([]int, length)
	for k := range slots {
		slots[k] = -1
	}
	var vals []super.Value
	for k, child := range n.children {
		val, err := child.build(sctx)
		if err != nil {
			return super.Value{}, err
		}
		slots[super.DecodeInt(n.keys[k].Bytes())] = len(vals)
		vals = append(vals, val)
	}
	inner := typeOf(sctx, vals)
	b := scode.Bytes{}
	if int64(len(vals)) == length {
		for _, slot := range slots {
			b = appendAs(b, inner, vals[slot])
		}
		return super.NewValue(sctx.LookupTypeArray(inner), b), nil
	}
	for _, slot := range slots {
		var o scode.Builder
		if slot < 0 {
			super.BuildNone(&o)
		} else {
			super.BuildSome(&o, bodyAs(inner, vals[slot]))
		}
		b = append(b, o.Bytes()...)
	}
	return super.NewValue(sctx.LookupTypeArray(sctx.LookupTypeOption(inner)), b), nil
}
