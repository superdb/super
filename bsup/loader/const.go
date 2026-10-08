package loader

import (
	"github.com/superdb/super"
	"github.com/superdb/super/bsup"
	"github.com/superdb/super/pkg/field"
	"github.com/superdb/super/vector"
)

type const_ struct {
	meta *bsup.Const
	len  uint32
}

func newConst(cctx *bsup.Context, meta *bsup.Const) *const_ {
	return &const_{meta: meta, len: meta.Len(cctx)}
}

func (c *const_) length() uint32 {
	return c.len
}

func (*const_) unmarshal(*bsup.Context, field.Projection) {}

func (c *const_) project(loader *loader, projection field.Projection) vector.Any {
	// Map the const super.Value in the bsup's type context to
	// a new one in the query type context.
	val := c.meta.Value
	if val.IsNull() {
		return vector.NewNull(c.length())
	}
	typ, err := loader.sctx.TranslateType(val.Type())
	if err != nil {
		panic(err)
	}
	vec := vector.NewConstFromValue(loader.sctx, super.NewValue(typ, val.Bytes()), c.length())
	if len(projection) > 0 {
		return vector.NewWrappedError(loader.sctx, "'.': applied to non-record", vec)
	}
	return vec
}
