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

func NewNamedEncoder(typedefs *TypeDefs, named *vector.Named) Encoder {
	e := &NamedEncoder{typ: named.Typ}
	if _, ok := named.Any.(*vector.Empty); !ok {
		e.encoder = NewEncoder(typedefs, named.Any)
	}
	return e
}

func (n *NamedEncoder) Encode(group *errgroup.Group) {
	if n.encoder != nil {
		n.encoder.Encode(group)
	}
}

func (n *NamedEncoder) Locate(w *LocatorWriter) {
	if n.encoder == nil {
		// An empty named vector is an empty const.
		w.WriteKind(KindConst)
		w.WriteCount(0)
		w.Value(nil)
		return
	}
	w.WriteKind(KindPlain)
	w.Locate(n.encoder)
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
