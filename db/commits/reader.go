package commits

import (
	"context"
	"errors"
	"io/fs"
	"uuid"

	"github.com/superdb/super"
	"github.com/superdb/super/sio"
)

type LogReader struct {
	ctx       context.Context
	marshaler *super.Marshaler
	store     *Store
	cursor    uuid.UUID
	stop      uuid.UUID
}

var _ sio.Reader = (*LogReader)(nil)

func newLogReader(ctx context.Context, sctx *super.Context, store *Store, leaf, stop uuid.UUID) *LogReader {
	m := super.NewMarshaler(sctx)
	m.Decorate(super.StyleSimple)
	return &LogReader{
		ctx:       ctx,
		marshaler: m,
		store:     store,
		cursor:    leaf,
		stop:      stop,
	}
}

func (r *LogReader) Read() (*super.Value, error) {
	if r.cursor == uuid.Nil() {
		return nil, nil
	}
	_, commitObject, err := r.store.GetBytes(r.ctx, r.cursor)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			r.cursor = uuid.Nil()
			err = nil
		}
		return nil, err
	}
	next := commitObject.Parent
	if next == r.stop {
		next = uuid.Nil()
	}
	r.cursor = next
	val, err := r.marshaler.Marshal(commitObject)
	return &val, err
}
