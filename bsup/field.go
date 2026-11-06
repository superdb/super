package bsup

import (
	"io"

	"github.com/superdb/super/pkg/field"
	"golang.org/x/sync/errgroup"
)

type FieldEncoder struct {
	name   string
	values Encoder
}

func (f *FieldEncoder) Encode(group *errgroup.Group, count uint32) {
	f.values.Encode(group)
}

func (f *FieldEncoder) Stats(path field.Path, fn CollectStatsFunc) {
	f.values.Stats(append(path, f.name), fn)
}

func (f *FieldEncoder) Emit(w io.Writer) error {
	return f.values.Emit(w)
}
