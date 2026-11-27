package csvio

import (
	"bufio"
	"bytes"
	"io"
	"unicode"
)

// preprocess is a reader, meant to sit in front of the go csv reader, that
// looks for fields where quotes do not cover the entirety of a field. If such
// a case is found the quotes are stripped from the field.
//
// For instance, the line:
// field1,"field2" extra
// Would get converted into:
// field1,field2 extra
type preprocess struct {
	delimiter rune
	leftover  []byte
	scanner   *bufio.Reader
	scratch   []byte
}

func newPreprocess(r io.Reader, delim rune) *preprocess {
	if delim == 0 {
		delim = ','
	}
	return &preprocess{
		delimiter: delim,
		scanner:   bufio.NewReader(r),
	}
}

func (p *preprocess) Read(b []byte) (int, error) {
	n := len(p.leftover)
	if n > 0 {
		if cc := p.copy(b, p.leftover); cc < n {
			return cc, nil
		}
	}
	for {
		field, err := p.parseField()
		if len(field) > 0 {
			cc := p.copy(b[n:], field)
			n += cc
			if cc < len(field) {
				// If copied is less than field size it means there was not
				// enough space in b to copy the entirety of the field. The
				// copy function has copied the remaining data into leftover,
				// just return what we have.
				return n, err
			}
		}
		if err != nil {
			return n, err
		}
	}
}

func (p *preprocess) copy(dst []byte, src []byte) int {
	cc := copy(dst, src)
	p.leftover = append(p.leftover[:0], src[cc:]...)
	return cc
}

func (p *preprocess) parseField() ([]byte, error) {
	var hasstr bool
	p.scratch = p.scratch[:0]
	for {
		c, err := p.scanner.ReadByte()
		if err != nil {
			return p.scratch, err
		}
		if c == '"' {
			if !hasstr {
				// Space before the opening quote isn't part of the field.
				p.scratch = bytes.TrimLeftFunc(p.scratch, unicode.IsSpace)
			}
			hasstr = true
			var s []byte
			s, err = p.parseString()
			p.scratch = append(p.scratch, s...)
			if err == nil {
				continue
			}
			if err != io.EOF {
				return p.scratch, err
			}
		}
		if rune(c) == p.delimiter || c == '\n' || err == io.EOF {
			var ending []byte
			if err != io.EOF {
				ending = []byte{c}
			}
			if hasstr {
				// If field had quotes wrap entire field in quotes.
				if last := len(p.scratch) - 1; last >= 0 && p.scratch[last] == '\r' {
					ending = []byte("\r\n")
					p.scratch = p.scratch[:last]
				}
				p.scratch = append(p.scratch, '"')
				p.scratch = append([]byte{'"'}, p.scratch...)
			}
			p.scratch = append(p.scratch, ending...)
			return p.scratch, err
		}
		p.scratch = append(p.scratch, c)
	}
}

func (p *preprocess) parseString() ([]byte, error) {
	var str []byte
	for {
		c, err := p.scanner.ReadByte()
		if err != nil {
			return str, err
		}
		if c == '"' {
			next, err := p.scanner.ReadByte()
			if err != nil {
				return str, err
			}
			if next == '"' {
				// keep double quotes in a string.
				str = append(str, c, c)
				continue
			}
			return str, p.scanner.UnreadByte()
		}
		str = append(str, c)
	}
}
