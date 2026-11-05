package bsup

import (
	"io"

	"github.com/superdb/super/pkg/field"
	"github.com/superdb/super/vector"
	"golang.org/x/sync/errgroup"
)

type AnyEncoder struct {
	values   Encoder
	subtypes *TypeValueEncoder
}

func NewAnyEncoder(typedefs *TypeDefs, vec *vector.Fusion) *AnyEncoder {
	return &AnyEncoder{
		values:   NewPrimitiveEncoder(typedefs, vec.Values, false),
		subtypes: NewTypeValueEncoder(typedefs, vec.Subtypes),
	}
}

func (a *AnyEncoder) Emit(w io.Writer) error {
	if err := a.values.Emit(w); err != nil {
		return err
	}
	return a.subtypes.Emit(w)
}

func (a *AnyEncoder) Encode(group *errgroup.Group) {
	a.values.Encode(group)
	a.subtypes.Encode(group)
}

// Locate encodes the locators of a fusion(all) vector, which are those of any
// fusion vector (see FusionEncoder.Locate).
func (a *AnyEncoder) Locate(w *LocatorWriter) {
	w.WriteKind(KindPlain)
	w.Locate(a.values)
	a.subtypes.locateIDs(w)
}

func (*AnyEncoder) Stats(field.Path, CollectStatsFunc) {}
