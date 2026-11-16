package anyio

import (
	"bytes"
	"io"
)

const utf8BOM = "\xef\xbb\xbf"

// SkipBOM returns a reader for the bytes of r that follow a leading UTF-8
// byte order mark or for all of r's bytes if r doesn't begin with one.  It
// doesn't seek r since that costs another request for a remote object.
func SkipBOM(r io.Reader) io.Reader {
	if b := readBOM(r); string(b) != utf8BOM {
		return io.MultiReader(bytes.NewReader(b), r)
	}
	return r
}

// readBOM reads from r until it has read a UTF-8 byte order mark or a byte
// that isn't part of one and returns the bytes it read.
func readBOM(r io.Reader) []byte {
	var b []byte
	for i := range len(utf8BOM) {
		var c [1]byte
		if _, err := io.ReadFull(r, c[:]); err != nil {
			break
		}
		b = append(b, c[0])
		if c[0] != utf8BOM[i] {
			break
		}
	}
	return b
}
