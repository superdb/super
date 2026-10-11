package sbuf

import (
	"context"
	"fmt"
	"sync/atomic"

	"github.com/superdb/super"
	"github.com/superdb/super/sio"
	"github.com/superdb/super/vector/vio"
)

// A Scanner is a Batch source that also provides progress updates.
type Scanner interface {
	vio.Meter
	Puller
}

// NewScanner returns a Scanner for r that filters records by filterExpr and s.
// If r implements fmt.Stringer, the scanner reports errors using a prefix of the
// string returned by its String method.
func NewScanner(ctx context.Context, r sio.Reader) (Scanner, error) {
	s, err := newScanner(ctx, r)
	if err != nil {
		return nil, err
	}
	if stringer, ok := r.(fmt.Stringer); ok {
		s = NamedScanner(s, stringer.String())
	}
	return s, nil
}

func newScanner(ctx context.Context, r sio.Reader) (Scanner, error) {
	sc := &scanner{reader: r, ctx: ctx}
	sc.Puller = NewPuller(sc)
	return sc, nil
}

type scanner struct {
	Puller
	reader   sio.Reader
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
		atomic.AddInt64(&s.progress.BytesScanned, int64(len(this.Bytes())))
		atomic.AddInt64(&s.progress.ValuesScanned, 1)
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

func MultiScanner(scanners ...Scanner) Scanner {
	return &multiScanner{scanners: scanners}
}

type multiScanner struct {
	scanners []Scanner
	progress vio.Progress
}

func (m *multiScanner) Pull(done bool) (Batch, error) {
	for len(m.scanners) > 0 {
		batch, err := m.scanners[0].Pull(done)
		if batch != nil || err != nil {
			return batch, err
		}
		m.progress.Add(m.scanners[0].Progress())
		m.scanners = m.scanners[1:]
	}
	return nil, nil
}

func (m *multiScanner) Progress() vio.Progress {
	return m.progress.Copy()
}
