package oldbsup

import (
	"errors"
	"fmt"
	"io"
	"slices"

	"github.com/superdb/super/pkg/peeker"
	"github.com/superdb/super/sbuf"
	"github.com/superdb/super/scode"
)

var errBadFormat = errors.New("malformed bsup-rows value")

// parser decodes the framing protocol for BSUPROWS updating and resetting its
// super context in conformance with BSUPROWS framing.
type parser struct {
	peeker  *peeker.Reader
	types   *Decoder
	maxSize int
}

const Version = 6

func CheckVersion(code byte) error {
	var version int
	if (code & 0x80) != 0 {
		version = int(code & 0x7f)
	}
	if version != Version {
		return fmt.Errorf("bsup-rows version mismatch: expected %d, found %d", Version, version)
	}
	return nil
}

func (p *parser) read() (frame, error) {
	for {
		version, err := p.peeker.ReadByte()
		if err != nil {
			return frame{}, err
		}
		if version == EOS {
			// At EOS, we create a new Decoder which clears out the types slice
			// mapping the local type IDs to the shared-context types.  Any data
			// batches concurrently being decoded by a worker will still point
			// to the old types slice so all continues on just fine as
			// everything gets properly mappped to the shared context
			// under concurrent locking in the target super.Context.
			p.types = NewDecoder(p.types.sctx)
			continue
		}
		if err := CheckVersion(version); err != nil {
			return frame{}, err
		}
		code, err := p.peeker.ReadByte()
		if err != nil {
			return frame{}, err
		}
		switch typ := (code >> 4) & 3; typ {
		case TypesFrame:
			if err := p.decodeTypes(code); err != nil {
				return frame{}, err
			}
		case ValuesFrame:
			return p.decodeValues(code)
		case ControlFrame:
			return frame{}, p.decodeControl(code)
		default:
			return frame{}, fmt.Errorf("unknown bsup-rows message frame type: %d", typ)
		}
	}
}

func (p *parser) decodeTypes(code byte) error {
	if (code & 0x40) != 0 {
		// Compressed
		f, err := p.readCompressedFrame(code)
		if err != nil {
			return err
		}
		if err := f.decompress(); err != nil {
			return err
		}
		if err := p.types.decode(f.ubuf); err != nil {
			return err
		}
		f.free()
		return nil
	} else {
		// Uncompressed.
		// b points into the peaker buffer, but not a problem
		// as we decode everything before the next read.
		f, err := p.readFrame(code)
		if err != nil {
			return err
		}
		tmpBuf := buffer{data: f}
		if err := p.types.decode(&tmpBuf); err != nil {
			return err
		}
		return nil
	}
}

func (p *parser) decodeValues(code byte) (frame, error) {
	if (code & 0x40) != 0 {
		// Compressed
		return p.readCompressedFrame(code)
	}
	bytes, err := p.readFrame(code)
	if err != nil {
		return frame{}, err
	}
	return frame{ubuf: newBufferFromBytes(bytes)}, nil
}

// decodeControl reads the next message frame as a control message and
// returns it as *sbuf.Control, which implements error.  Errors are also
// return as error so reflection must be used to distringuish the cases.
func (p *parser) decodeControl(code byte) error {
	var bytes []byte
	if (code & 0x40) == 0 {
		// b points into the peaker buffer so we copy it.
		b, err := p.readFrame(code)
		if err != nil {
			return err
		}
		bytes = slices.Clone(b)
	} else {
		// The frame is compressed.
		blk, err := p.readCompressedFrame(code)
		if err != nil {
			return err
		}
		if err := blk.decompress(); err != nil {
			return err
		}
		bytes = slices.Clone(blk.ubuf.data)
		blk.free()
	}
	if len(bytes) == 0 {
		return errBadFormat
	}
	// Insert this control message into the result queue to preserve
	// order between values frames and messages.  Note that a back-to-back
	// sequence of control messages will be processed here by the scanner
	// go-routine as the workers go idle.  However, this is not a critical
	// performance path so we're not worried about parallelism here.
	return &sbuf.Control{
		Message: &Control{
			Format: int(bytes[0]),
			Bytes:  bytes[1:],
		},
	}
}

func (p *parser) readFrame(code byte) ([]byte, error) {
	size, err := p.decodeLength(code)
	if err != nil {
		return nil, err
	}
	if size < 1 {
		return nil, fmt.Errorf("bsup-rows: frame length (%d) too small", size)
	}
	if size > p.maxSize {
		return nil, fmt.Errorf("bsup-rows: frame length (%d) exceeds maximum allowed (%d)", size, p.maxSize)
	}
	b, err := p.peeker.Read(size)
	if err == peeker.ErrBufferOverflow {
		return nil, fmt.Errorf("large value of %d bytes exceeds maximum read buffer", size)
	}
	return b, err
}

// readCompressedFrame parses the compression header and reads the compressed
// payload from the peaker into a buffer.  This allows the peaker to move on
// and the worker to decompress the buffer concurrently.  (A more sophisticated
// implementation could sync the peeker movement to the decode pipeline to
// avoid this copy.  In this approach, compressed buffers would point into the
// peeker buffer and be released after decompression.  A reference-counted double
// buffer would work nicely for this.)
func (p *parser) readCompressedFrame(code byte) (frame, error) {
	n, err := p.decodeLength(code)
	if err != nil {
		return frame{}, err
	}
	format, err := p.peeker.ReadByte()
	if err != nil {
		return frame{}, err
	}
	size, err := readUvarintAsInt(p.peeker)
	if err != nil {
		return frame{}, err
	}
	if size < 1 {
		return frame{}, fmt.Errorf("bsup-rows: frame length (%d) too small", size)
	}
	if size > p.maxSize {
		return frame{}, fmt.Errorf("bsup-rows: frame length (%d) exceeds maximum allowed (%d)", size, p.maxSize)
	}
	// The size of the compressed buffer needs to be adjusted by the
	// byte for the format and the variable-length bytes to encode
	// the original size.
	n -= 1 + scode.SizeOfUvarint(uint64(size))
	b, err := p.peeker.Read(n)
	if err != nil && err != io.EOF {
		if err == peeker.ErrBufferOverflow {
			return frame{}, fmt.Errorf("large value of %d bytes exceeds maximum read buffer", n)
		}
		return frame{}, errBadFormat
	}
	return frame{
		fmt:  CompressionFormat(format),
		zbuf: newBufferFromBytes(b),
		ubuf: newBuffer(size),
	}, nil
}

func (p *parser) decodeLength(code byte) (int, error) {
	v, err := readUvarintAsInt(p.peeker)
	if err != nil {
		return 0, err
	}
	return (v << 4) | (int(code) & 0xf), nil
}
