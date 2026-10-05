package oldbsup

import (
	"encoding/binary"
	"io"
	"slices"

	"github.com/pierrec/lz4/v4"
	"github.com/superdb/super"
	"github.com/superdb/super/sbuf"
	"github.com/superdb/super/scode"
	"github.com/superdb/super/vector"
)

// DefaultFrameThresh is a reasonable default for WriterOpts.FrameThresh.
const DefaultFrameThresh = 512 * 1024

type Writer struct {
	writer     io.WriteCloser
	position   int64
	flushed    int64
	compressor *compressor

	types  *Encoder
	values []byte
	header []byte
}

// NewWriterWithOpts returns a writer to w with opts.
func NewWriter(w io.WriteCloser) *Writer {
	return &Writer{
		writer:     w,
		compressor: &compressor{},
		types:      NewEncoder(),
	}
}

func (w *Writer) Push(vec vector.Any) error {
	return sbuf.WriteVec(w, vec)
}

func (w *Writer) DisableCompression() {
	w.compressor = nil
}

func (w *Writer) Close() error {
	err := w.EndStream()
	if closeErr := w.writer.Close(); err == nil {
		err = closeErr
	}
	return err
}

func (w *Writer) write(p []byte) error {
	n, err := w.writer.Write(p)
	if err != nil {
		return err
	}
	w.position += int64(n)
	return nil
}

// Position may be called after EndStream to get a seekable offset into the
// output for the next stream.  Calling Position at any other team returns
// unusable seek offsets.
func (w *Writer) Position() int64 {
	return w.position
}

func (w *Writer) EndStream() error {
	// Flush any compression state and write the EOS afterward the
	// compressed block since the buffer-filter may skip entire
	// compressed before and we would otherwise miss the EOS marker.
	if err := w.flush(); err != nil {
		return err
	}
	if w.flushed != w.position {
		if err := w.write([]byte{EOS}); err != nil {
			return err
		}
		w.flushed = w.position
	}
	w.types.Reset()
	return nil
}

func (w *Writer) Write(val super.Value) error {
	id := w.types.Encode(val.Type())
	w.values = binary.AppendUvarint(w.values, uint64(id))
	w.values = scode.Append(w.values, val.Bytes())
	if thresh := DefaultFrameThresh; len(w.values) >= thresh || w.types.Len() >= thresh {
		return w.flush()
	}
	return nil
}

func (w *Writer) WriteControl(b []byte, format uint8) error {
	// Flush the compressor since we need to preserve the interleaving
	// order of control messages and BSUPROW data.
	if err := w.flush(); err != nil {
		return err
	}
	// Yuck.
	bytes := make([]byte, len(b)+1)
	bytes[0] = format
	copy(bytes[1:], b)
	return w.writeBlock(ControlFrame, bytes)
}

func (w *Writer) flush() error {
	if err := w.writeBlock(TypesFrame, w.types.nextBuffer()); err != nil {
		return nil
	}
	if err := w.writeBlock(ValuesFrame, w.values); err != nil {
		return nil
	}
	w.values = w.values[:0]
	return nil
}

func (w *Writer) writeBlock(blockType int, b []byte) error {
	if len(b) == 0 {
		return nil
	}
	if w.compressor != nil {
		sbuf, err := w.compressor.compress(b)
		if err != nil {
			return err
		}
		if sbuf != nil {
			if err := w.writeCompHeader(blockType, len(b), len(sbuf)); err != nil {
				return err
			}
			return w.write(sbuf)
		}
	}
	if err := w.writeHeader(blockType, len(b)); err != nil {
		return err
	}
	return w.write(b)
}

func (w *Writer) writeHeader(blockType, size int) error {
	version := Version | 0x80
	code := blockType<<4 | (size & 0xf)
	w.header = append(w.header[:0], byte(version))
	w.header = append(w.header, byte(code))
	w.header = binary.AppendUvarint(w.header, uint64(size>>4))
	return w.write(w.header)
}

func (w *Writer) writeCompHeader(blockType, size, zlen int) error {
	zlen += 1 + scode.SizeOfUvarint(uint64(size))
	version := Version | 0x80
	code := (blockType << 4) | (zlen & 0xf) | 0x40
	w.header = append(w.header[:0], byte(version))
	w.header = append(w.header, byte(code))
	w.header = binary.AppendUvarint(w.header, uint64(zlen>>4))
	w.header = append(w.header, byte(CompressionFormatLZ4))
	w.header = binary.AppendUvarint(w.header, uint64(size))
	return w.write(w.header)
}

type compressor struct {
	compressor lz4.Compressor
	sbuf       []byte
}

func (c *compressor) compress(b []byte) ([]byte, error) {
	if c == nil || len(b) == 0 {
		return nil, nil
	}
	c.sbuf = slices.Grow(c.sbuf[:0], len(b))
	sbuf := c.sbuf[:len(b)]
	zlen, err := c.compressor.CompressBlock(b, sbuf)
	if err != nil && err != lz4.ErrInvalidSourceShortBuffer {
		return nil, err
	}
	if zlen > 0 {
		// Compression succeeded and the compressed value message block
		// is smaller than the buffered messages, so write the
		// compressed value message block.
		return sbuf[:zlen], nil
	}
	return nil, nil
}
