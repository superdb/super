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
	byteCap  int
}

func newStream(ctx context.Context, r io.Reader, n, inputCap, byteCap int) *stream {
	return &stream{
		r:        r,
		ch:       make(chan result, n),
		ctx:      ctx,
		done:     make(chan struct{}),
		inputCap: inputCap,
		byteCap:  byteCap,
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
	r := NewValReader(s.r)
	for {
		batch, startLineNum, err := readBatch(r, s.inputCap, s.byteCap)
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

// putBytesTable returns t to the pool unless its buffer grew well beyond
// byteCap (e.g., to hold a single oversized value), in which case it is
// dropped so the pool doesn't pin that memory.
func putBytesTable(t *vector.BytesTable, byteCap int) {
	if cap(t.RawBytes()) <= 2*byteCap {
		bytesTablePool.Put(t)
	}
}

// readBatch reads values from r until the batch holds inputCap values or
// at least byteCap bytes, whichever comes first.  A batch always holds at
// least one value so a value larger than byteCap is still read.
func readBatch(r *valReader, inputCap, byteCap int) (*vector.BytesTable, int, error) {
	t := newBytesTable(inputCap)
	start := r.lineNumber()
	var err error
	for range inputCap {
		var b []byte
		if b, err = r.Next(); err != nil {
			break
		}
		t.Append(b)
		if len(t.RawBytes()) >= byteCap {
			break
		}
	}
	if t.Len() == 0 {
		putBytesTable(t, byteCap)
		t = nil
	}
	if errors.Is(err, io.EOF) {
		err = nil
	}
	return t, start, err
}
