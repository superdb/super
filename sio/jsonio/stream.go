package jsonio

import (
	"context"
	"errors"
	"io"
	"sync"

	"github.com/superdb/super/vector"
)

type stream struct {
	r        io.Reader
	ch       chan result
	done     chan struct{}
	once     sync.Once
	ctx      context.Context
	inputCap int
}

func newStream(ctx context.Context, r io.Reader, n int, inputCap int) *stream {
	return &stream{
		r:        r,
		ch:       make(chan result, n),
		ctx:      ctx,
		done:     make(chan struct{}),
		inputCap: inputCap,
	}
}

type result struct {
	bytes        *vector.BytesTable
	startLineNum int
	err          error
}

func (s *stream) next() (*vector.BytesTable, int, error) {
	s.once.Do(func() { go s.run() })
	select {
	case r := <-s.ch:
		return r.bytes, r.startLineNum, r.err
	case <-s.ctx.Done():
		return nil, -1, s.ctx.Err()
	}
}

func (s *stream) run() {
	defer close(s.ch)
	r := NewValReader(s.r, s.inputCap)
	for {
		batch, startLineNum, err := readBatch(r)
		select {
		case s.ch <- result{batch, startLineNum, err}:
			if batch == nil || err != nil {
				return
			}
		case <-s.done:
			return
		case <-s.ctx.Done():
			return
		}
	}
}

func (s *stream) close() {
	close(s.done)
}

var bytesTablePool sync.Pool

func newBytesTable(inputCap int) *vector.BytesTable {
	b, ok := bytesTablePool.Get().(*vector.BytesTable)
	if !ok {
		b = new(vector.NewBytesTableEmpty(uint32(inputCap)))
	}
	b.Reset()
	return b
}

func readBatch(r *valReader) (*vector.BytesTable, int, error) {
	t := newBytesTable(r.inputCap)
	start := r.lineNumber()
	var err error
	for range r.inputCap {
		var b []byte
		if b, err = r.Next(); err != nil {
			break
		}
		t.Append(b)
	}
	if t.Len() == 0 {
		bytesTablePool.Put(t)
		t = nil
	}
	if errors.Is(err, io.EOF) {
		err = nil
	}
	return t, start, err
}
