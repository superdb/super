package function

import (
	"encoding/base64"
	"encoding/hex"

	"github.com/superdb/super"
	"github.com/superdb/super/runtime/expr"
	"github.com/superdb/super/vector"
)

type Base64 struct {
	sctx *super.Context
}

func (b *Base64) Call(args ...vector.Any) vector.Any {
	if vec, ok := expr.Catch(b.sctx, true, args, "base64"); ok {
		return vec
	}
	val := vector.Under(args[0])
	switch val.Type().ID() {
	case super.IDBytes:
		out := vector.NewStringEmpty(0)
		for i := uint32(0); i < val.Len(); i++ {
			bytes := vector.BytesValue(val, i)
			out.Append(base64.StdEncoding.EncodeToString(bytes))
		}
		return out
	case super.IDString:
		errvals := vector.NewStringEmpty(0)
		tags := make([]uint32, val.Len())
		out := vector.NewBytesEmpty(0)
		for i := uint32(0); i < val.Len(); i++ {
			s := vector.StringValue(val, i)
			bytes, err := base64.StdEncoding.DecodeString(s)
			if err != nil {
				errvals.Append(s)
				tags[i] = 1
				continue
			}
			out.Append(bytes)
		}
		err := vector.NewWrappedError(b.sctx, "base64: string argument is not base64", errvals)
		return vector.NewDynamic(tags, []vector.Any{out, err})
	default:
		return vector.NewWrappedError(b.sctx, "base64: argument must a bytes or string type", val)
	}
}

type Hex struct {
	sctx *super.Context
}

func (h *Hex) Call(args ...vector.Any) vector.Any {
	if vec, ok := expr.Catch(h.sctx, true, args, "hex"); ok {
		return vec
	}
	val := vector.Under(args[0])
	switch val.Type().ID() {
	case super.IDBytes:
		out := vector.NewStringEmpty(val.Len())
		for i := uint32(0); i < val.Len(); i++ {
			bytes := vector.BytesValue(val, i)
			out.Append(hex.EncodeToString(bytes))
		}
		return out
	case super.IDString:
		errvals := vector.NewStringEmpty(0)
		tags := make([]uint32, val.Len())
		out := vector.NewBytesEmpty(0)
		for i := uint32(0); i < val.Len(); i++ {
			s := vector.StringValue(val, i)
			bytes, err := hex.DecodeString(s)
			if err != nil {
				errvals.Append(s)
				tags[i] = 1
				continue
			}
			out.Append(bytes)
		}
		err := vector.NewWrappedError(h.sctx, "hex: string argument is not hexadecimal", errvals)
		return vector.NewDynamic(tags, []vector.Any{out, err})
	default:
		return vector.NewWrappedError(h.sctx, "hex: argument must a bytes or string type", val)
	}
}
