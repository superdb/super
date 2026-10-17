package vector

import (
	"github.com/superdb/super"
	"github.com/superdb/super/scode"
)

type View struct {
	Any
	Index []uint32
}

var _ Any = (*View)(nil)

func NewView(vec Any, index []uint32) *View {
	return &View{vec, index}
}

func (v *View) Len() uint32 {
	return uint32(len(v.Index))
}

func (v *View) Serialize(b *scode.Builder, slot uint32) {
	v.Any.Serialize(b, v.Index[slot])
}

func PushView(val Any) Any {
	view, ok := val.(*View)
	if !ok {
		return val
	}
	if view.Len() == 0 {
		return NewEmpty(view.Type())
	}
	switch val := view.Any.(type) {
	case *Record:
		var fields []Any
		for _, field := range val.Fields {
			fields = append(fields, Pick(field, view.Index))
		}
		return NewRecord(val.Typ, fields, view.Len())
	case *Array:
		inner, offsets := pickList(val.Values, view.Index, val.Offsets)
		return NewArray(val.Typ, offsets, inner)
	case *Set:
		inner, offsets := pickList(val.Values, view.Index, val.Offsets)
		return NewSet(val.Typ, offsets, inner)
	case *Map:
		keys, offsets := pickList(val.Keys, view.Index, val.Offsets)
		values, _ := pickList(val.Values, view.Index, val.Offsets)
		return NewMap(val.Typ, offsets, keys, values)
	case *Fusion:
		types := val.Subtypes.Types()
		outTypes := make([]super.Type, len(view.Index))
		for i, slot := range view.Index {
			outTypes[i] = types[slot]
		}
		return NewFusion(val.Typ, Pick(val.Values, view.Index), outTypes)
	case *Option:
		return Pick(val, view.Index)
	default:
		return view
	}
}

func pickList(inner Any, index, offsets []uint32) (Any, []uint32) {
	newOffsets := []uint32{0}
	var innerIndex []uint32
	for _, idx := range index {
		start, end := offsets[idx], offsets[idx+1]
		for ; start < end; start++ {
			innerIndex = append(innerIndex, start)
		}
		newOffsets = append(newOffsets, uint32(len(innerIndex)))
	}
	return Pick(inner, innerIndex), newOffsets
}
