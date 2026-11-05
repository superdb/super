package emitter

import (
	"context"
	"fmt"
	"strconv"

	"github.com/superdb/super"
	"github.com/superdb/super/pkg/storage"
	"github.com/superdb/super/sio"
	"github.com/superdb/super/sio/anyio"
	"github.com/superdb/super/vector"
	"github.com/superdb/super/vector/vio"
)

type Split struct {
	ctx        context.Context
	sctx       *super.Context
	dir        *storage.URI
	prefix     string
	unbuffered bool
	ext        string
	opts       anyio.WriterOpts
	writers    map[super.Type]vio.PushCloser
	seen       map[string]struct{}
	engine     storage.Engine
}

var _ vio.Pusher = (*Split)(nil)

func NewSplit(ctx context.Context, sctx *super.Context, engine storage.Engine, dir *storage.URI, prefix string, unbuffered bool, opts anyio.WriterOpts) (*Split, error) {
	e := sio.Extension(opts.Format)
	if e == "" {
		return nil, fmt.Errorf("unknown format: %s", opts.Format)
	}
	if prefix != "" {
		prefix = prefix + "-"
	}
	return &Split{
		ctx:        ctx,
		sctx:       sctx,
		dir:        dir,
		prefix:     prefix,
		unbuffered: unbuffered,
		ext:        e,
		opts:       opts,
		writers:    make(map[super.Type]vio.PushCloser),
		seen:       make(map[string]struct{}),
		engine:     engine,
	}, nil
}

func (s *Split) Push(vec vector.Any) error {
	if vec, ok := vec.(*vector.Dynamic); ok {
		for _, v := range vec.Values {
			if err := s.Push(v); err != nil {
				return err
			}
		}
		return nil
	}
	out, err := s.lookupOutput(vec.Type())
	if err != nil {
		return err
	}
	return out.Push(vec)
}

func (s *Split) lookupOutput(typ super.Type) (vio.PushCloser, error) {
	w, ok := s.writers[typ]
	if ok {
		return w, nil
	}
	w, err := NewFileFromURI(s.ctx, s.sctx, s.engine, s.path(), s.unbuffered, s.opts)
	if err != nil {
		return nil, err
	}
	s.writers[typ] = w
	return w, nil
}

// path returns the storage URI given the prefix combined with a unique ID
// to make a unique path for each type.
func (s *Split) path() *storage.URI {
	uniq := strconv.Itoa(len(s.writers))
	return s.dir.JoinPath(s.prefix + uniq + s.ext)
}

func (s *Split) Close() error {
	var cerr error
	for _, w := range s.writers {
		if err := w.Close(); err != nil {
			cerr = err
		}
	}
	return cerr
}
