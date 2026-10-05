package sbuf

import (
	"context"
	"fmt"
	"sync/atomic"

	"github.com/superdb/super"
	"github.com/superdb/super/pkg/field"
	"github.com/superdb/super/runtime/sam/expr"
	"github.com/superdb/super/sio"
	"github.com/superdb/super/vector/vio"
)

type Pushdown interface {
	Projection() field.Projection
	DataFilter() (expr.Evaluator, error)
	BSUPFilter() (*expr.BufferFilter, error)
	MetaFilter() (expr.Evaluator, field.Projection, error)
	// Undordered reports whether a reader may return values in arbirary order.
	Unordered() bool
}

// ScannerAble is implemented by Readers that provide an optimized
// implementation of the Scanner interface.
type ScannerAble interface {
	NewScanner(context.Context, Pushdown) (Scanner, error)
}

// A Scanner is a Batch source that also provides progress updates.
type Scanner interface {
	vio.Meter
	Puller
}

// NewScanner returns a Scanner for r that filters records by filterExpr and s.
// If r implements fmt.Stringer, the scanner reports errors using a prefix of the
// string returned by its String method.
func NewScanner(ctx context.Context, r sio.Reader, filterExpr Pushdown) (Scanner, error) {
	s, err := newScanner(ctx, r, filterExpr)
	if err != nil {
		return nil, err
	}
	if stringer, ok := r.(fmt.Stringer); ok {
		s = NamedScanner(s, stringer.String())
	}
	return s, nil
}

func newScanner(ctx context.Context, r sio.Reader, filterExpr Pushdown) (Scanner, error) {
	if sa, ok := r.(ScannerAble); ok {
		return sa.NewScanner(ctx, filterExpr)
	}
	var f expr.Evaluator
	if filterExpr != nil {
		var err error
		if f, err = filterExpr.DataFilter(); err != nil {
			return nil, err
		}
	}
	sc := &scanner{reader: r, filter: f, ctx: ctx}
	sc.Puller = NewPuller(sc)
	return sc, nil
}

type scanner struct {
	Puller
	reader   sio.Reader
	filter   expr.Evaluator
	ctx      context.Context
	progress vio.Progress
}

func (s *scanner) Progress() vio.Progress {
	return s.progress.Copy()
}

// Read implements Reader.Read.
func (s *scanner) Read() (*super.Value, error) {
	for {
		if err := s.ctx.Err(); err != nil {
			return nil, err
		}
		this, err := s.reader.Read()
		if err != nil || this == nil {
			return nil, err
		}
		atomic.AddInt64(&s.progress.BytesRead, int64(len(this.Bytes())))
		atomic.AddInt64(&s.progress.RecordsRead, 1)
		if s.filter != nil {
			if !expr.IsTrue(s.filter.Eval(*this)) {
				continue
			}
		}
		atomic.AddInt64(&s.progress.BytesMatched, int64(len(this.Bytes())))
		atomic.AddInt64(&s.progress.RecordsMatched, 1)
		return this, nil
	}
}

type MultiStats []Scanner

func (m MultiStats) Progress() vio.Progress {
	var ss vio.Progress
	for _, s := range m {
		ss.Add(s.Progress())
	}
	return ss
}

func NamedScanner(s Scanner, name string) *namedScanner {
	return &namedScanner{
		Scanner: s,
		name:    name,
	}
}

type namedScanner struct {
	Scanner
	name string
}

func (n *namedScanner) Pull(done bool) (Batch, error) {
	b, err := n.Scanner.Pull(done)
	if err != nil {
		err = fmt.Errorf("%s: %w", n.name, err)
	}
	return b, err
}
