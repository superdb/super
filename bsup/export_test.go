package bsup

import (
	"bytes"

	"github.com/superdb/super/vector"
)

// EncodeFrame encodes vec as a column frame without the dynamic builder of
// a ColumnWriter, which rebuilds the vectors written to it, so tests can
// encode vectors of any shape (e.g., consts of complex types).
func EncodeFrame(vec *vector.Dynamic) ([]byte, error) {
	enc := NewDynamicEncoder(vec)
	vtypes, locators, dataSize, err := enc.Encode()
	if err != nil {
		return nil, err
	}
	typedefs := enc.typedefs.Encoder().Bytes()
	header := newColumnHeader(0, uint64(len(vtypes)), uint64(len(locators)), uint64(len(typedefs)), dataSize)
	var buf bytes.Buffer
	buf.Write(header.Serialize())
	buf.Write(vtypes)
	buf.Write(locators)
	buf.Write(typedefs)
	if err := enc.Emit(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
