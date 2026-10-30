package expr

import (
	"fmt"
	"math"

	"github.com/superdb/super"
	"github.com/superdb/super/runtime/expr/cast"
	"github.com/superdb/super/scode"
	"github.com/superdb/super/sup"
	"github.com/superdb/super/vector"
)

// Index represents an index operator "container[index]" where container is
// either an array or set (with index type integer), or a record
// (with index type string), or a map (with any index type).
type Index struct {
	sctx      *super.Context
	container Evaluator
	index     Evaluator
	base1     bool
}

func NewIndexExpr(sctx *super.Context, container, index Evaluator, base1 bool) Evaluator {
	return &Index{sctx, container, index, base1}
}

func (i *Index) Eval(this vector.Any) vector.Any {
	return vector.Apply(vector.ApplyRipUnions|vector.ApplyRipFusions, i.eval, i.container.Eval(this), i.index.Eval(this))
}

func (i *Index) eval(args ...vector.Any) vector.Any {
	container := vector.Under(args[0])
	index := vector.Under(args[1])
	switch container.Kind() {
	case vector.KindArray, vector.KindSet:
		return indexArrayOrSet(i.sctx, container, index, i.base1)
	case vector.KindRecord:
		return indexRecord(i.sctx, container, index, i.base1)
	case vector.KindMap:
		return indexMap(i.sctx, container, index)
	case vector.KindType:
		return indexTypeValue(i.sctx, container, index, i.base1)
	case vector.KindOption:
		return vector.NewWrappedError(i.sctx, "option value cannot be indexed (consider ??)", container)
	default:
		return vector.NewWrappedError(i.sctx, "value cannot be indexed", container)
	}
}

func indexArrayOrSet(sctx *super.Context, vec, indexVec vector.Any, base1 bool) vector.Any {
	if _, ok := indexVec.(*vector.Error); ok {
		return indexVec
	}
	if id := indexVec.Type().ID(); super.IsUnsigned(id) {
		return vector.Apply(vector.ApplyRipUnions, func(args ...vector.Any) vector.Any {
			return indexArrayOrSet(sctx, args[0], args[1], base1)
		}, vec, cast.To(sctx, indexVec, super.TypeInt64))
	} else if !super.IsInteger(id) {
		return vector.NewWrappedError(sctx, "index is not an integer", indexVec)
	}
	var index []uint32
	if view, ok := vec.(*vector.View); ok {
		vec, index = view.Any, view.Index
	}
	offsets, vals := arrayOrSetContents(vec)
	var errs []uint32
	var viewIndexes []uint32
	for i := range indexVec.Len() {
		idx := i
		if index != nil {
			idx = index[i]
		}
		idxVal := vector.IntValue(indexVec, uint32(i))
		start := offsets[idx]
		len := int64(offsets[idx+1]) - int64(start)
		if idxVal < 0 {
			idxVal = len + idxVal
		} else if base1 {
			idxVal--
		}
		if idxVal >= 0 && idxVal < len {
			viewIndexes = append(viewIndexes, start+uint32(idxVal))
			continue
		}
		errs = append(errs, i)
	}
	out := vector.Pick(vector.Deunion(vals), viewIndexes)
	if len(errs) > 0 {
		return vector.NewCombinedError(sctx, "index out of range", out, indexVec, errs)
	}
	return out
}

func indexRecord(sctx *super.Context, vec, indexVec vector.Any, base1 bool) vector.Any {
	var isint bool
	switch id := indexVec.Type().ID(); {
	case super.IsUnsigned(id):
		return vector.Apply(vector.ApplyRipUnions, func(args ...vector.Any) vector.Any {
			return indexRecord(sctx, args[0], args[1], base1)
		}, vec, cast.To(sctx, indexVec, super.TypeInt64))
	case super.IsSigned(id):
		isint = true
	case id == super.IDString:
	default:
		if indexVec.Type().Kind() == super.ErrorKind {
			return indexVec
		}
		return vector.NewWrappedError(sctx, "invalid value for record index", indexVec)
	}
	var rec *vector.Record
	var index []uint32
	switch vec := vec.(type) {
	case *vector.Record:
		rec = vec
	case *vector.View:
		rec, index = vec.Any.(*vector.Record), vec.Index
	default:
		panic(vec)
	}
	var errs []uint32
	tags := make([]uint32, vec.Len())
	n := len(rec.Typ.Fields)
	viewIndexes := make([][]uint32, n)
	for i := range vec.Len() {
		var k int
		if isint {
			k = int(vector.IntValue(indexVec, i))
			if k < 0 {
				k = n + k
			} else if base1 {
				k--
			}
		} else {
			field := vector.StringValue(indexVec, i)
			var ok bool
			if k, ok = rec.Typ.IndexOfField(field); !ok {
				k = -1
			}
		}
		if k < 0 || k >= n {
			tags[i] = uint32(n)
			errs = append(errs, i)
			continue
		}
		idx := i
		if index != nil {
			idx = index[i]
		}
		tags[i] = uint32(k)
		viewIndexes[k] = append(viewIndexes[k], idx)
	}
	out := make([]vector.Any, n+1)
	out[n] = vector.NewWrappedError(sctx, "invalid record index", vector.NewView(indexVec, errs))
	for i, field := range rec.Fields {
		out[i] = vector.Pick(field, viewIndexes[i])
	}
	return vector.NewDynamic(tags, out)
}

func indexMap(sctx *super.Context, vec, indexVec vector.Any) vector.Any {
	// XXX At some point we'll want to optimize having a const indexVec.
	if _, ok := indexVec.(*vector.Error); ok {
		return indexVec
	}
	m := vector.PushView(vec).(*vector.Map)
	var pick []uint32
	for i := range vec.Len() {
		for range m.Offsets[i+1] - m.Offsets[i] {
			pick = append(pick, i)
		}
	}
	var valIndexes, nones []uint32
	cmp := NewCompare(sctx, "==", nil, nil).evalAllowingOption
	hits := vector.Apply(vector.ApplyRipFusions|vector.ApplyRipUnions, cmp, vector.Pick(indexVec, pick), m.Keys)
	bits := FlattenBool(hits).Bits
	for i := range vec.Len() {
		selected := -1
		for off := m.Offsets[i]; off < m.Offsets[i+1]; off++ {
			if bits.IsSet(off) {
				selected = int(off)
				break
			}
		}
		if selected != -1 {
			valIndexes = append(valIndexes, uint32(selected))
		} else {
			nones = append(nones, i)
		}
	}
	vals := vector.Pick(vector.Deunion(m.Values), valIndexes)
	if len(nones) > 0 {
		return vector.Combine(vals, nones, vector.NewNone(uint32(len(nones))))
	}
	return vals
}

// indexTypeValue returns the type for each type value in vec indexed by the
// corresponding value in indexVec.  A string index is a field name and is the
// same as the dot operator (see derefType), so, e.g., <{x:int64}>["x"] is
// <int64>.  An integer index is the position of a field of a record type or
// selects the element type of an array or set type, so, e.g.,
// <{x:int64,y:string}>[1] and <[string]>[0] are <string>.
func indexTypeValue(sctx *super.Context, vec, indexVec vector.Any, base1 bool) vector.Any {
	if _, ok := indexVec.(*vector.Error); ok {
		return indexVec
	}
	id := indexVec.Type().ID()
	if id != super.IDString && !super.IsInteger(id) {
		return vector.NewWrappedError(sctx, "invalid value for type index", indexVec)
	}
	out := vector.NewDynamicValueBuilder()
	var b, ib scode.Builder
	for i := range vec.Len() {
		val := vector.ValueAt(&b, vec, i)
		if val.IsNull() {
			out.Write(val)
			continue
		}
		typ, err := sctx.LookupByValue(val.Bytes())
		if err != nil {
			panic(err)
		}
		idx := vector.ValueAt(&ib, indexVec, i)
		var msg string
		if id == super.IDString {
			name := super.DecodeString(idx.Bytes())
			if typ = derefType(sctx, typ, name); typ == nil {
				msg = fmt.Sprintf("no such field '%s'", sup.QuotedName(name))
			}
		} else {
			var k int64
			if super.IsSigned(id) {
				k = super.DecodeInt(idx.Bytes())
			} else {
				k = int64(min(super.DecodeUint(idx.Bytes()), math.MaxInt64))
			}
			if typ = indexType(sctx, typ, k, base1); typ == nil {
				msg = "invalid type index"
			}
		}
		if msg != "" {
			out.Write(sctx.WrapError(msg, val))
			continue
		}
		out.Write(sctx.LookupTypeValue(typ))
	}
	return out.Build(sctx)
}

// indexType returns the type at integer index k in typ or nil if there is no
// such type.  See indexTypeValue.
func indexType(sctx *super.Context, typ super.Type, k int64, base1 bool) super.Type {
	switch typ := typ.(type) {
	case *super.TypeNamed:
		return indexType(sctx, typ.Type, k, base1)
	case *super.TypeRecord:
		n := int64(len(typ.Fields))
		if k < 0 {
			k += n
		} else if base1 {
			k--
		}
		if k >= 0 && k < n {
			return typ.Fields[k].Type
		}
	case *super.TypeArray:
		return typ.Type
	case *super.TypeSet:
		return typ.Type
	case *super.TypeUnion:
		return unionOfTypes(sctx, typ, func(typ super.Type) super.Type {
			return indexType(sctx, typ, k, base1)
		})
	case *super.TypeFusion:
		return indexType(sctx, typ.Type, k, base1)
	case *super.TypeOption:
		return indexType(sctx, typ.Type, k, base1)
	case *super.TypeError:
		return indexType(sctx, typ.Type, k, base1)
	}
	return nil
}
