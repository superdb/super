package expr

import (
	"unicode/utf8"

	"github.com/superdb/super"
	"github.com/superdb/super/runtime/sam/expr"
	"github.com/superdb/super/vector"
)

type sliceExpr struct {
	sctx                            *super.Context
	containerEval, fromEval, toEval Evaluator
	base1                           bool
}

func NewSliceExpr(sctx *super.Context, container, from, to Evaluator, base1 bool) Evaluator {
	return &sliceExpr{
		sctx:          sctx,
		containerEval: container,
		fromEval:      from,
		toEval:        to,
		base1:         base1,
	}
}

func (s *sliceExpr) Eval(vec vector.Any) vector.Any {
	vecs := []vector.Any{s.containerEval.Eval(vec)}
	if s.fromEval != nil {
		vecs = append(vecs, s.fromEval.Eval(vec))
	}
	if s.toEval != nil {
		vecs = append(vecs, s.toEval.Eval(vec))
	}
	return vector.Apply(vector.ApplyRipUnions|vector.ApplyRipFusions, s.eval, vecs...)
}

func (s *sliceExpr) eval(vecs ...vector.Any) vector.Any {
	if vec, ok := Catch(s.sctx, true, vecs, "slice expression"); ok {
		return vec
	}
	container := vecs[0]
	vecs = vecs[1:]
	var from, to vector.Any
	if s.fromEval != nil {
		from = vecs[0]
		if !super.IsSigned(from.Type().ID()) {
			return vector.NewStringError(s.sctx, "slice index is not a number", from.Len())
		}
		vecs = vecs[1:]
	}
	if s.toEval != nil {
		to = vecs[0]
		if !super.IsSigned(to.Type().ID()) {
			return vector.NewStringError(s.sctx, "slice index is not a number", to.Len())
		}
	}
	switch container.Kind() {
	case vector.KindArray, vector.KindSet:
		return s.evalArrayOrSlice(container, from, to, s.base1)
	case vector.KindBytes, vector.KindString:
		return s.evalStringOrBytes(container, from, to, s.base1)
	default:
		return vector.NewWrappedError(s.sctx, "sliced value is not array, set, bytes, or string", container)
	}
}

func (s *sliceExpr) evalArrayOrSlice(vec, fromVec, toVec vector.Any, base1 bool) vector.Any {
	from, constFrom := sliceIsConstIndex(fromVec)
	to, constTo := sliceIsConstIndex(toVec)
	slowPath := !constFrom || !constTo
	var index []uint32
	n := vec.Len()
	if view, ok := vec.(*vector.View); ok {
		vec, index = view.Any, view.Index
	}
	offsets, inner := arrayOrSetContents(vec)
	newOffsets := []uint32{0}
	var innerIndex []uint32
	for i := range n {
		idx := i
		if index != nil {
			idx = index[i]
		}
		off := offsets[idx]
		size := int(offsets[idx+1] - off)
		start, end := 0, size
		if fromVec != nil {
			if slowPath {
				from = int(vector.IntValue(fromVec, i))
			}
			start = sliceIndex(from, size, base1)
		}
		if toVec != nil {
			if slowPath {
				to = int(vector.IntValue(toVec, i))
			}
			end = sliceIndex(to, size, base1)
		}
		start, end = expr.FixSliceBounds(start, end, size)
		newOffsets = append(newOffsets, newOffsets[len(newOffsets)-1]+uint32(end-start))
		for k := start; k < end; k++ {
			innerIndex = append(innerIndex, off+uint32(k))
		}
	}
	inner = vector.Pick(inner, innerIndex)
	if vec.Kind() == vector.KindArray {
		return vector.NewArray(vec.Type().(*super.TypeArray), newOffsets, inner)
	}
	return vector.NewSet(vec.Type().(*super.TypeSet), newOffsets, inner)
}

func (s *sliceExpr) evalStringOrBytes(vec, fromVec, toVec vector.Any, base1 bool) vector.Any {
	constFrom, isConstFrom := sliceIsConstIndex(fromVec)
	constTo, isConstTo := sliceIsConstIndex(toVec)
	if isConstFrom && isConstTo {
		if out, ok := s.evalStringOrBytesFast(vec, constFrom, constTo, base1); ok {
			return out
		}
	}
	newOffsets := []uint32{0}
	var newBytes []byte
	id := vec.Type().ID()
	for i := range vec.Len() {
		slice := s.bytesAt(vec, i)
		size := lengthOfBytesOrString(id, slice)
		start, end := 0, size
		if fromVec != nil {
			from := int(vector.IntValue(fromVec, i))
			start = sliceIndex(from, size, base1)
		}
		if toVec != nil {
			to := int(vector.IntValue(toVec, i))
			end = sliceIndex(to, size, base1)
		}
		start, end = expr.FixSliceBounds(start, end, size)
		slice = sliceBytesOrString(slice, id, start, end)
		newBytes = append(newBytes, slice...)
		newOffsets = append(newOffsets, newOffsets[len(newOffsets)-1]+uint32(len(slice)))

	}
	return s.bytesOrStringVec(vec.Type(), newOffsets, newBytes)
}

func (s *sliceExpr) evalStringOrBytesFast(vec vector.Any, from, to int, base1 bool) (vector.Any, bool) {
	switch vec := vec.(type) {
	case *vector.Const:
		slice := s.bytesAt(vec, 0)
		id := vec.Type().ID()
		size := lengthOfBytesOrString(id, slice)
		start, end := 0, size
		if s.fromEval != nil {
			start = sliceIndex(from, size, base1)
		}
		if s.toEval != nil {
			end = sliceIndex(to, size, base1)
		}
		start, end = expr.FixSliceBounds(start, end, size)
		slice = sliceBytesOrString(slice, id, start, end)
		if id == super.IDBytes {
			return vector.NewConstBytes(slice, vec.Len()), true
		}
		return vector.NewConstString(string(slice), vec.Len()), true
	case *vector.View:
		out, ok := s.evalStringOrBytesFast(vec.Any, from, to, base1)
		if !ok {
			return nil, false
		}
		return vector.NewView(out, vec.Index), true
	case *vector.Dict:
		out, ok := s.evalStringOrBytesFast(vec.Any, from, to, base1)
		if !ok {
			return nil, false
		}
		return vector.NewDict(out, vec.Index, vec.Counts), true
	default:
		offsets, bytes := stringOrBytesContents(vec)
		newOffsets := []uint32{0}
		newBytes := []byte{}
		id := vec.Type().ID()
		for i := range vec.Len() {
			slice := bytes[offsets[i]:offsets[i+1]]
			size := lengthOfBytesOrString(id, slice)
			start, end := 0, size
			if s.fromEval != nil {
				start = sliceIndex(from, size, base1)
			}
			if s.toEval != nil {
				end = sliceIndex(to, size, base1)
			}
			start, end = expr.FixSliceBounds(start, end, size)
			slice = sliceBytesOrString(slice, id, start, end)
			newBytes = append(newBytes, slice...)
			newOffsets = append(newOffsets, newOffsets[len(newOffsets)-1]+uint32(len(slice)))
		}
		return s.bytesOrStringVec(vec.Type(), newOffsets, newBytes), true
	}
}

func (s *sliceExpr) bytesOrStringVec(typ super.Type, offsets []uint32, bytes []byte) vector.Any {
	switch typ.ID() {
	case super.IDBytes:
		return vector.NewBytes(vector.NewBytesTable(offsets, bytes))
	case super.IDString:
		return vector.NewString(vector.NewBytesTable(offsets, bytes))
	default:
		panic(typ)
	}
}

func (s *sliceExpr) bytesAt(val vector.Any, slot uint32) []byte {
	switch val.Kind() {
	case vector.KindBytes:
		return vector.BytesValue(val, slot)
	case vector.KindString:
		return []byte(vector.StringValue(val, slot))
	}
	panic(val)
}

func lengthOfBytesOrString(id int, slice []byte) int {
	if id == super.IDString {
		return utf8.RuneCount(slice)
	}
	return len(slice)
}

func sliceIsConstIndex(vec vector.Any) (int, bool) {
	if vec == nil {
		return 0, true
	}
	if _, ok := vec.(*vector.Const); ok {
		return int(vector.IntValue(vec, 0)), true
	}
	return 0, false
}

func sliceIndex(idx, size int, base1 bool) int {
	if base1 && idx > 0 {
		idx--
	}
	if idx < 0 {
		idx += size
	}
	return idx
}

func sliceBytesOrString(slice []byte, id int, start, end int) []byte {
	if id == super.IDString {
		slice = slice[expr.UTF8PrefixLen(slice, start):]
		return slice[:expr.UTF8PrefixLen(slice, end-start)]
	} else {
		return slice[start:end]
	}
}

func stringOrBytesContents(vec vector.Any) ([]uint32, []byte) {
	switch vec := vec.(type) {
	case *vector.String:
		offsets, bytes := vec.Table().Slices()
		return offsets, bytes
	case *vector.Bytes:
		offsets, bytes := vec.Table().Slices()
		return offsets, bytes
	default:
		panic(vec)
	}
}

func arrayOrSetContents(vec vector.Any) ([]uint32, vector.Any) {
	switch vec := vec.(type) {
	case *vector.Array:
		return vec.Offsets, vec.Values
	case *vector.Set:
		return vec.Offsets, vec.Values
	default:
		panic(vec)
	}
}
