package cast

import (
	"github.com/superdb/super/pkg/byteconv"
	"github.com/superdb/super/vector"
)

func castToBool(vec vector.Any, index []uint32) (vector.Any, []uint32, string, bool) {
	switch vec := vec.(type) {
	case *vector.Int:
		return numberToBool(vec.Values, index), nil, "", true
	case *vector.Uint:
		return numberToBool(vec.Values, index), nil, "", true
	case *vector.Float:
		return numberToBool(vec.Values, index), nil, "", true
	case *vector.Bool:
		out := vector.Any(vec)
		if index != nil {
			out = vector.Pick(out, index)
		}
		return out, nil, "", true
	case *vector.String:
		vvec, errs := stringToBool(vec, index)
		return vvec, errs, "", true
	default:
		return nil, nil, "", false
	}
}

func numberToBool[E numeric](s []E, index []uint32) *vector.Bool {
	n := uint32(len(s))
	if index != nil {
		n = uint32(len(index))
	}
	out := vector.NewFalse(n)
	for i := range n {
		idx := i
		if index != nil {
			idx = index[i]
		}
		if s[idx] != 0 {
			out.Set(i)
		}
	}
	return out
}

func stringToBool(vec *vector.String, index []uint32) (vector.Any, []uint32) {
	n := lengthOf(vec, index)
	bools := vector.NewFalse(n)
	var errs []uint32
	var boollen uint32
	for i := range n {
		idx := i
		if index != nil {
			idx = index[i]
		}
		b, err := byteconv.ParseBool(vec.Table().Bytes(idx))
		if err != nil {
			errs = append(errs, i)
			continue
		}
		if b {
			bools.Set(boollen)
		}
		boollen++
	}
	bools.Bits.Shorten(boollen)
	return bools, errs
}
