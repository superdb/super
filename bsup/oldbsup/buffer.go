package oldbsup

import (
	"io"
	"sync"
)

type buffer struct {
	data []byte
	off  int
}

var bigBuffers sync.Pool
var smallBuffers sync.Pool

func newBuffer(length int) *buffer {
	if length <= DefaultFrameThresh {
		b, ok := smallBuffers.Get().(*buffer)
		if !ok {
			b = &buffer{data: make([]byte, DefaultFrameThresh)}
		}
		b.data = b.data[:length]
		b.off = 0
		return b
	}
	b, ok := bigBuffers.Get().(*buffer)
	// The capacity check will send the smaller buffers to GC and tend
	// to keep buffers big enough in the pool
	if !ok || cap(b.data) < length {
		// Add 25% cushion
		b = &buffer{data: make([]byte, length+(length>>2))}
	}
	b.data = b.data[:length]
	b.off = 0
	return b
}

func newBufferFromBytes(b []byte) *buffer {
	buf := newBuffer(len(b))
	copy(buf.data, b)
	return buf
}

func (b *buffer) free() {
	if b == nil {
		return
	}
	if cap(b.data) == DefaultFrameThresh {
		smallBuffers.Put(b)
	} else if cap(b.data) > DefaultFrameThresh {
		bigBuffers.Put(b)
	}
}

// Bytes is so named to avoid collision with the bytes package.
func (b *buffer) Bytes() []byte {
	return b.data[b.off:]
}

func (b *buffer) length() int {
	return len(b.data) - b.off
}

func (b *buffer) read(n int) ([]byte, error) {
	var err error
	if avail := b.length(); n > avail {
		if avail == 0 {
			return nil, io.EOF
		}
		err = io.ErrUnexpectedEOF
		n = avail
	}
	off := b.off
	b.off += n
	return b.data[off:b.off], err
}

func (b *buffer) ReadByte() (byte, error) {
	if b.length() < 1 {
		return 0, io.EOF
	}
	off := b.off
	b.off++
	return b.data[off], nil
}
