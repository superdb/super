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

func newConst(bctx *bsup.Context, meta *bsup.Const) *const_ {
	return &const_{meta: meta, len: meta.Len(bctx)}
}

func (c *const_) length() uint32 {
	return c.len
}

func (*const_) unmarshal(*bsup.Context, field.Projection) {}

func (c *const_) project(loader *FrameLoader, projection field.Projection) vector.Any {
	// Map the type from the BSUP context to the query context.
	typ := loader.lookupType(c.meta.TypeID)
	if c.length() == 0 {
		return vector.NewEmpty(typ)
	}
	if typ == super.TypeNull {
		return vector.NewNull(c.length())
	}
	return vector.NewConstFromValue(loader.sctx, super.NewValue(typ, c.meta.Bytes), c.length())
}

func (k *const_) strings(c *collector) {
	if id := c.loader.lookupType(k.meta.TypeID).ID(); id < super.IDTypeComplex && id != super.IDString {
		return
	}
	// Complex consts: every embedded string appears verbatim in the
	// encoding, so the raw bytes are a safe superset to scan.
	c.append(k.meta.Bytes)
}
