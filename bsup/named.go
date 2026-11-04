package bsup

import (
	"io"

	"github.com/superdb/super"
	"github.com/superdb/super/pkg/field"
	"github.com/superdb/super/vector"
	"golang.org/x/sync/errgroup"
)

type NamedEncoder struct {
	encoder Encoder
	typ     *super.TypeNamed
}

func NewNamedEncoder(bctx *Context, named *vector.Named) Encoder {
	e := &NamedEncoder{typ: named.Typ}
	if _, ok := named.Any.(*vector.Empty); !ok {
		e.encoder = NewEncoder(bctx, named.Any)
	}
	return e
}

func (n *NamedEncoder) Encode(group *errgroup.Group) {
	if n.encoder != nil {
		n.encoder.Encode(group)
	}
}

func (n *NamedEncoder) Metadata(bctx *Context, off uint64) (uint64, ID) {
	if n.encoder == nil {
		return off, bctx.enter(&Empty{bctx.TypeDefs().LookupType(n.typ)})
	}
	off, id := n.encoder.Metadata(bctx, off)
	return off, bctx.enter(&Named{n.typ.Name, id})
}

func (n *NamedEncoder) Stats(path field.Path, fn CollectStatsFunc) {
	if n.encoder != nil {
		n.encoder.Stats(path, fn)
	}
}

func (n *NamedEncoder) Emit(w io.Writer) error {
	if n.encoder != nil {
		return n.encoder.Emit(w)
	}
	return nil
}
