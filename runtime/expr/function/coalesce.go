package function

import (
	"math"
	"slices"

	"github.com/superdb/super/vector"
)

type Coalesce struct{}

func (*Coalesce) ApplyOpt() vector.ApplyOpt { return vector.ApplyNone }

func (c *Coalesce) Call(vecs ...vector.Any) vector.Any {
	for i, vec := range vecs {
		if !containsNullOrError(vec) {
			return vec
		}
		if k := vec.Kind(); k == vector.KindUnion || k == vector.KindFusion {
			return c.slow(vecs[i:])
		}
	}
	return vector.NewNull(vecs[0].Len())
}

func (c *Coalesce) slow(vecs []vector.Any) vector.Any {
	var indexes [][]uint32
	var numNulls uint32
	vecsIndexToTag := map[int]uint32{}
	tags := make([]uint32, vecs[0].Len())
Loop:
	for i := range tags {
		i := uint32(i)
		for j, vec := range vecs {
			if !slotIsNullOrError(vec, i) {
				tag, ok := vecsIndexToTag[j]
				if !ok {
					tag = uint32(len(vecsIndexToTag))
					indexes = append(indexes, nil)
					vecsIndexToTag[j] = tag
				}
				indexes[tag] = append(indexes[tag], i)
				tags[i] = tag
				continue Loop
			}
		}
		numNulls++
		tags[i] = math.MaxUint32
	}
	outvecs := make([]vector.Any, len(vecsIndexToTag))
	for from, tag := range vecsIndexToTag {
		outvecs[tag] = vector.Pick(vecs[from], indexes[tag])
	}
	if numNulls > 0 {
		outvecs = append(outvecs, vector.NewNull(numNulls))
		nullTag := uint32(len(outvecs) - 1)
		for i, tag := range tags {
			if tag == math.MaxUint32 {
				tags[i] = nullTag
			}
		}
	}
	return vector.NewDynamic(tags, outvecs)
}

func containsNullOrError(vec vector.Any) bool {
	switch vec := vector.Under(vec).(type) {
	case *vector.Null, *vector.Missing:
		return true
	case *vector.Union:
		return slices.ContainsFunc(vec.Values(), containsNullOrError)
	case *vector.Error:
		return true
	case *vector.Named:
		return containsNullOrError(vec.Any)
	case *vector.Fusion:
		return containsNullOrError(vec.Values)
	case *vector.Const:
		return containsNullOrError(vec.Any)
	case *vector.Dict:
		return containsNullOrError(vec.Any)
	case *vector.Dynamic:
		return slices.ContainsFunc(vec.Values, containsNullOrError)
	case *vector.View:
		return containsNullOrError(vec.Any)
	}
	return false
}

func slotIsNullOrError(vec vector.Any, slot uint32) bool {
	switch vec := vec.(type) {
	case *vector.Null:
		return true
	case *vector.Missing:
		return true
	case *vector.Union:
		return slotIsNullOrError(vec.Dynamic(), slot)
	case *vector.Error:
		return true
	case *vector.Named:
		return slotIsNullOrError(vec.Any, slot)
	case *vector.Const:
		return slotIsNullOrError(vec.Any, 0)
	case *vector.Dict:
		return slotIsNullOrError(vec.Any, uint32(vec.Index[slot]))
	case *vector.Dynamic:
		tag := vec.Tags[slot]
		return slotIsNullOrError(vec.Values[tag], vec.ForwardTagMap()[slot])
	case *vector.View:
		return slotIsNullOrError(vec.Any, vec.Index[slot])
	case *vector.Fusion:
		return slotIsNullOrError(vec.Values, slot)
	}
	return false
}
