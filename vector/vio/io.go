package vio

import (
	"io"
	"slices"
	"sync/atomic"

	"github.com/superdb/super/vector"
)

type Puller interface {
	Pull(done bool) (vector.Any, error)
}

type PullCloser interface {
	Puller
	io.Closer
}

// A Scanner is a Puller that also provides progress updates.
type Scanner interface {
	Meter
	Puller
}

type ScanCloser interface {
	Scanner
	io.Closer
}

type Pusher interface {
	Push(vector.Any) error
}

type PushCloser interface {
	Pusher
	io.Closer
}

func NewPuller(vecs ...vector.Any) Puller {
	return &puller{vecs}
}

type puller struct {
	vecs []vector.Any
}

func (p *puller) Pull(done bool) (vector.Any, error) {
	if len(p.vecs) == 0 {
		return nil, nil
	}
	vec := p.vecs[0]
	p.vecs = p.vecs[1:]
	return vec, nil
}

// A Meter provides Progress statistics.
type Meter interface {
	Progress() Progress
}

// Progress represents progress statistics from a Scanner.
type Progress struct {
	BytesRead      int64 `super:"bytes_read" json:"bytes_read"`
	BytesMatched   int64 `super:"bytes_matched" json:"bytes_matched"`
	RecordsRead    int64 `super:"records_read" json:"records_read"`
	RecordsMatched int64 `super:"records_matched" json:"records_matched"`
}

var _ Meter = (*Progress)(nil)

// Add updates its receiver by adding to it the values in ss.
func (p *Progress) Add(in Progress) {
	if p != nil {
		atomic.AddInt64(&p.BytesRead, in.BytesRead)
		atomic.AddInt64(&p.BytesMatched, in.BytesMatched)
		atomic.AddInt64(&p.RecordsRead, in.RecordsRead)
		atomic.AddInt64(&p.RecordsMatched, in.RecordsMatched)
	}
}

func (p *Progress) Copy() Progress {
	if p == nil {
		return Progress{}
	}
	return Progress{
		BytesRead:      atomic.LoadInt64(&p.BytesRead),
		BytesMatched:   atomic.LoadInt64(&p.BytesMatched),
		RecordsRead:    atomic.LoadInt64(&p.RecordsRead),
		RecordsMatched: atomic.LoadInt64(&p.RecordsMatched),
	}
}

func (p *Progress) Progress() Progress {
	return p.Copy()
}

func Copy(dst Pusher, src Puller) error {
	for {
		vec, err := src.Pull(false)
		if err != nil || vec == nil {
			return err
		}
		if _, ok := vec.(*vector.Control); ok {
			continue
		}
		vec, _ = vector.Unlabel(vec)
		if vec == nil {
			continue
		}
		if err := dst.Push(vec); err != nil {
			return err
		}
	}
}

func CopyMux(outputs map[string]Pusher, parent Puller) error {
	for {
		vec, err := parent.Pull(false)
		if vec == nil || err != nil {
			return err
		}
		if _, ok := vec.(*vector.Control); ok {
			continue
		}
		var label string
		vec, label = vector.Unlabel(vec)
		if vec == nil {
			continue
		}
		if w, ok := outputs[label]; ok {
			if err := w.Push(vec); err != nil {
				return err
			}
		}
	}
}

func ConcatPuller(readers ...Puller) Puller {
	if len(readers) == 1 {
		return readers[0]
	}
	return &concatReader{slices.Clone(readers)}
}

type concatReader struct {
	pullers []Puller
}

func (c *concatReader) Pull(done bool) (vector.Any, error) {
	if done {
		var err error
		for _, p := range c.pullers {
			if _, perr := p.Pull(done); err == nil {
				err = perr
			}
		}
		c.pullers = nil
		return nil, err
	}
	for len(c.pullers) > 0 {
		vec, err := c.pullers[0].Pull(done)
		if vec != nil || err != nil {
			return vec, err
		}
		c.pullers = c.pullers[1:]
	}
	return nil, nil
}
