package vector

import (
	"github.com/superdb/super/vector/bitvec"
)

// Pick takes any vector vec and an index and returns a new vector consisting of the
// elements in the index.
func Pick(val Any, index []uint32) Any {
	switch val := val.(type) {
	case *Bool:
		return NewBool(val.Bits.Pick(index))
	case *Const:
		return NewConst(val.Any, uint32(len(index)))
	case *Dict:
		index2 := make([]byte, len(index))
		counts := make([]uint32, val.Any.Len())
		for k, idx := range index {
			v := val.Index[idx]
			index2[k] = v
			counts[v]++
		}
		return NewDict(val.Any, index2, counts)
	case *Error:
		return NewError(val.Typ, Pick(val.Vals, index))
	case *None:
		return NewNone(uint32(len(index)))
	case *NoRip:
		return &NoRip{Pick(val.Any, index)}
	case *Null:
		return NewNull(uint32(len(index)))
	case *Union:
		tags, values := viewForUnionOrDynamic(index, val.Tags(), val.ForwardTagMap(), val.Values())
		return NewUnion(val.Typ, tags, values)
	case *Dynamic:
		return NewDynamic(viewForUnionOrDynamic(index, val.Tags, val.ForwardTagMap(), val.Values))
	case *View:
		index2 := make([]uint32, len(index))
		for k, idx := range index {
			index2[k] = uint32(val.Index[idx])
		}
		return NewView(val.Any, index2)
	case *Named:
		// Wrapped View under Named so vector.Under still works.
		return &Named{val.Typ, Pick(val.Any, index)}
	case *Option:
		return NewOption(val.Typ, Pick(val.Any, index))
	case nil:
		return nil
	}
	return &View{val, index}
}

func viewForUnionOrDynamic(index, tags, forward []uint32, values []Any) ([]uint32, []Any) {
	indexes := make([][]uint32, len(values))
	resultTags := make([]uint32, len(index))
	for k, index := range index {
		tag := tags[index]
		indexes[tag] = append(indexes[tag], forward[index])
		resultTags[k] = tag
	}
	results := make([]Any, len(values))
	for k := range results {
		results[k] = Pick(values[k], indexes[k])
	}
	return resultTags, results
}

// ReversePick is like Pick but it builds the vector from the elements
// that are not in the index maintaining the element order of vec.
func ReversePick(vec Any, index []uint32) Any {
	return Pick(vec, bitvec.ReverseIndex(index, vec.Len()))
}
