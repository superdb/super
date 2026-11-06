package bsup

import (
	"io"

	"github.com/superdb/super/pkg/field"
	"golang.org/x/sync/errgroup"
)

type NoneEncoder struct {
	len uint32
}

func NewNoneEncoder(len uint32) *NoneEncoder {
	return &NoneEncoder{len}
}

func (n *NoneEncoder) Locate(w *LocatorWriter) {
	w.WriteKind(KindPlain)
	w.WriteCount(n.len)
}

func (*NoneEncoder) Encode(group *errgroup.Group)       {}
func (*NoneEncoder) Stats(field.Path, CollectStatsFunc) {}
func (*NoneEncoder) Emit(io.Writer) error               { return nil }
