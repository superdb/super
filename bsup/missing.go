package bsup

import (
	"io"

	"github.com/superdb/super/pkg/field"
	"golang.org/x/sync/errgroup"
)

type MissingEncoder struct {
	len uint32
}

func NewMissingEncoder(len uint32) *MissingEncoder {
	return &MissingEncoder{len}
}

func (n *MissingEncoder) Locate(w *LocatorWriter) {
	w.WriteKind(KindPlain)
	w.WriteCount(n.len)
}

func (*MissingEncoder) Encode(group *errgroup.Group)       {}
func (*MissingEncoder) Emit(io.Writer) error               { return nil }
func (*MissingEncoder) Stats(field.Path, CollectStatsFunc) {}
