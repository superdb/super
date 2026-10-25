package anyio

import (
	"context"
	"os"
	"sync"

	"github.com/superdb/super"
	"github.com/superdb/super/pkg/storage"
	"github.com/superdb/super/sbuf"
	"github.com/superdb/super/sio"
	"github.com/superdb/super/vector"
)

// Sources opens the inputs of a query.  Lookup returns the same Source for a
// given path so the compiler, which asks for an input's type, and the
// runtime, which reads its values, share whatever was read to determine the
// type (e.g., an input that can be read only once).
type Sources struct {
	engine  storage.Engine
	mu      sync.Mutex
	sources map[string]*Source
}

func NewSources(engine storage.Engine) *Sources {
	return &Sources{
		engine:  engine,
		sources: make(map[string]*Source),
	}
}

func (s *Sources) Lookup(path string) (*Source, error) {
	if path == "-" {
		path = "stdio:stdin"
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if src, ok := s.sources[path]; ok {
		return src, nil
	}
	uri, err := storage.ParseURI(path)
	if err != nil {
		return nil, err
	}
	src := &Source{
		engine:   s.engine,
		path:     path,
		uri:      uri,
		seekable: isSeekable(uri),
	}
	s.sources[path] = src
	return src, nil
}

// A Source is an input to a query, which is a local file, standard input,
// or a remote object.
type Source struct {
	engine   storage.Engine
	path     string
	uri      *storage.URI
	seekable bool

	mu  sync.Mutex
	buf *storage.Buffer
}

// isSeekable reports whether uri is a regular local file, which can be read
// at compile time to determine its type and read again at run time.  Other
// inputs (standard input, even when redirected from a file, pipes, and remote
// objects) are read into memory first when they must be read at compile time.
func isSeekable(uri *storage.URI) bool {
	if !uri.HasScheme(storage.FileScheme) {
		return false
	}
	switch uri.String() {
	case "/dev/stdin", "/dev/fd/0":
		return false
	}
	info, err := os.Stat(uri.Filepath())
	// If the file doesn't exist, call it seekable so the error is reported
	// when it's opened.
	return err != nil || info.Mode().IsRegular()
}

// Type returns the fused type of the source's values or nil if the type
// isn't known, in which case the source is dynamically typed.
//
// Without static, the type is known only for a seekable source in a format
// that knows its type (BSUP, SUP, Parquet, and Arrow), so other formats and
// non-seekable sources are dynamically typed.  With static, the type is
// always computed, if necessary by reading all of the source's values, and
// a non-seekable source is first read into memory so the runtime can then
// read the source from memory.
func (s *Source) Type(ctx context.Context, sctx *super.Context, opts ReaderOpts, static bool) (super.Type, error) {
	if !s.seekable {
		if !static {
			return nil, nil
		}
		if err := s.buffer(ctx); err != nil {
			return nil, err
		}
	}
	f, err := s.Open(ctx, sctx, opts)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if typed, ok := f.Puller.(sio.Typer); ok {
		typ, err := typed.Type()
		if err != nil || typ != nil || !static {
			return typ, err
		}
		// The reader couldn't determine the type (e.g., a compressed
		// BSUP file) so compute it from the values.
	}
	if !static {
		return nil, nil
	}
	fuser := super.NewFuser(sctx, true)
	for {
		vec, err := f.Pull(false)
		if vec == nil || err != nil {
			return fuser.Type(), err
		}
		vector.Apply(vector.ApplyNone, func(vecs ...vector.Any) vector.Any {
			fuser.Fuse(vecs[0].Type())
			return vecs[0]
		}, vec)
	}
}

// buffer reads the source into memory if it hasn't been already.
func (s *Source) buffer(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.buf != nil {
		return nil
	}
	r, err := s.engine.Get(ctx, s.uri)
	if err != nil {
		return err
	}
	defer r.Close()
	s.buf, err = storage.ReadBuffer(r)
	return err
}

// Open returns a new reader for the source's values, which reads from memory
// if the source was read into memory by Type.
func (s *Source) Open(ctx context.Context, sctx *super.Context, opts ReaderOpts) (*sbuf.File, error) {
	s.mu.Lock()
	buf := s.buf
	s.mu.Unlock()
	if buf != nil {
		return NewFile(ctx, sctx, buf.Reader(), s.path, opts)
	}
	ch := make(chan struct{})
	var f *sbuf.File
	var err error
	go func() {
		defer close(ch)
		var sr storage.Reader
		// Opening a fifo might block.
		sr, err = s.engine.Get(ctx, s.uri)
		if err != nil {
			return
		}
		// NewFile reads from sr, which might block.
		f, err = NewFile(ctx, sctx, sr, s.path, opts)
		if err != nil {
			sr.Close()
		}
	}()
	select {
	case <-ch:
		return f, err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
