package function

import (
	"uuid"

	"github.com/superdb/super"
	"github.com/superdb/super/vector"
)

type UUID struct {
	sctx *super.Context
}

func (*UUID) needsInput() {}

func (k *UUID) Call(args ...vector.Any) vector.Any {
	if len(args) == 1 {
		n := args[0].Len()
		out := vector.NewBytesEmpty(n)
		for range n {
			id := uuid.NewV7()
			out.Append(id[:])
		}
		return out
	}
	vec := vector.Under(args[1])
	switch vec.Type().ID() {
	case super.IDBytes:
		var errs []uint32
		out := vector.NewStringEmpty(vec.Len())
		for i := range vec.Len() {
			bytes := vector.BytesValue(vec, i)
			if len(bytes) != 16 {
				errs = append(errs, i)
				continue
			}
			id := uuid.UUID(bytes[:])
			out.Append(id.String())
		}
		errVec := vector.NewWrappedError(k.sctx, "uuid: invalid uuid value", vector.Pick(vec, errs))
		return vector.Combine(out, errs, errVec)
	case super.IDString:
		var errs []uint32
		out := vector.NewBytesEmpty(vec.Len())
		for i := uint32(0); i < vec.Len(); i++ {
			s := vector.StringValue(vec, i)
			id, err := uuid.Parse(s)
			if err != nil {
				errs = append(errs, i)
				continue
			}
			out.Append(id[:])
		}
		errVec := vector.NewWrappedError(k.sctx, "uuid: invalid uuid value", vector.Pick(vec, errs))
		return vector.Combine(out, errs, errVec)
	case super.IDNull:
		return vec
	default:
		return vector.NewWrappedError(k.sctx, "uuid: argument must a bytes or string type", vec)
	}
}
