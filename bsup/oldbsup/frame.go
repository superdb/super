package oldbsup

import (
	"fmt"

	"github.com/pierrec/lz4/v4"
)

const (
	TypesFrame   = 0
	ValuesFrame  = 1
	ControlFrame = 2
)

const (
	EOS                 = 0xff
	ControlFormatBSUP   = 0
	ControlFormatJSON   = 1
	ControlFormatSUP    = 2
	ControlFormatString = 3
	ControlFormatBinary = 4
)

type CompressionFormat int

const CompressionFormatLZ4 CompressionFormat = 0x00

type frame struct {
	fmt  CompressionFormat
	zbuf *buffer
	ubuf *buffer
}

func (f *frame) free() {
	f.zbuf.free()
	f.ubuf.free()
}

func (f *frame) decompress() error {
	if f.fmt != CompressionFormatLZ4 {
		return fmt.Errorf("bsup-rows: unknown compression format 0x%x", f.fmt)
	}
	n, err := lz4.UncompressBlock(f.zbuf.data, f.ubuf.data)
	if err != nil {
		return fmt.Errorf("bsup-rows: %w", err)
	}
	if n != len(f.ubuf.data) {
		return fmt.Errorf("bsup-rows: got %d uncompressed bytes, expected %d", n, len(f.ubuf.data))
	}
	return nil
}
