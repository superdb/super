package bsup

import (
	"io"

	"github.com/superdb/super"
	"github.com/superdb/super/pkg/field"
	"golang.org/x/sync/errgroup"
)

type ConstEncoder struct {
	val super.Value
	len uint32
}

func (*ConstEncoder) Encode(group *errgroup.Group) {}

func (c *ConstEncoder) Metadata(bctx *Context, off uint64) (uint64, ID) {
	return off, bctx.enter(&Const{bctx.TypeDefs().LookupType(c.val.Type()), c.val.Bytes(), c.len})
}

func (c *ConstEncoder) Stats(path field.Path, fn CollectStatsFunc) {
	if c.len > 0 && super.IsPrimitiveType(c.val.Type()) {
		fn(path, c.val, c.val)
	}
}

func (*ConstEncoder) Emit(w io.Writer) error { return nil }
