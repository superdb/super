package sup

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf16"
	"unicode/utf8"
)

var ErrBufferOverflow = errors.New("SUP scanner buffer size exceeded")

const primitiveRE = `^(true|false|null|none|NaN|nan|[[:xdigit:]]{8}-[[:xdigit:]]{4}-[[:xdigit:]]{4}-[[:xdigit:]]{4}-[[:xdigit:]]{12}|[-+][Ii]nf|[-+0-9Ee./]+|0x[[:xdigit:]]*|([[:xdigit:]]{0,4}(:[[:xdigit:]]{0,4}){2,}(/[0-9]+)?)|([-.0-9]+(ns|us|ms|s|m|h|d|w|y))+|([-.:T\d]+(Z|[-+]\d\d:\d\d)))`
const indentationRE = `\n\s*`
const ip6followRE = `^([[:space:],:}\]])`

type Lexer struct {
	reader      io.Reader
	buffer      []byte
	cursor      []byte
	primitive   *regexp.Regexp
	indentation *regexp.Regexp
	ip6follow   *regexp.Regexp
	line        int
}

const (
	ReadSize = 64 * 1024
	MaxSize  = 50 * 1024 * 1024
)

func NewLexer(r io.Reader) *Lexer {
	primitive := regexp.MustCompile(primitiveRE)
	primitive.Longest()
	indentation := regexp.MustCompile(indentationRE)
	indentation.Longest()
	return &Lexer{
		reader:      r,
		buffer:      make([]byte, ReadSize),
		primitive:   primitive,
		indentation: indentation,
		ip6follow:   regexp.MustCompile(ip6followRE),
		line:        1,
	}
}

func roundUp(n int) int {
	size := ReadSize
	for size < n {
		size *= 2
	}
	return size
}

func (l *Lexer) fill(n int) error {
	if n > MaxSize {
		return ErrBufferOverflow
	}
	remaining := len(l.cursor)
	if n >= cap(l.buffer) {
		n = roundUp(n)
		l.buffer = make([]byte, n)
		copy(l.buffer, l.cursor)
	} else if remaining > 0 {
		copy(l.buffer[0:remaining], l.cursor)
	}
	cc, err := io.ReadAtLeast(l.reader, l.buffer[remaining:cap(l.buffer)], n)
	l.cursor = l.buffer[0 : remaining+cc]
	if err == io.ErrUnexpectedEOF && cc > 0 {
		err = nil
	}
	return err
}

func (l *Lexer) check(n int) error {
	if len(l.cursor) < n {
		if err := l.fill(n); err != nil {
			return err
		}
		if len(l.cursor) < n {
			return io.ErrUnexpectedEOF
		}
	}
	return nil
}

func (l *Lexer) skip(n int) error {
	if err := l.check(n); err != nil {
		return err
	}
	l.line += bytes.Count(l.cursor[:n], []byte{'\n'})
	l.cursor = l.cursor[n:]
	return nil
}

func (l *Lexer) peekByte() (byte, error) {
	if err := l.check(1); err != nil {
		return 0, err
	}
	return l.cursor[0], nil
}

func (l *Lexer) peek(n int) (string, error) {
	if err := l.check(n); err != nil {
		return "", noEOF(err)
	}
	return string(l.cursor[0:n]), nil
}

func (l *Lexer) match(b byte) (bool, error) {
	if err := l.skipSpace(); err != nil {
		return false, err
	}
	return l.matchTight(b)
}

func (l *Lexer) matchTight(b byte) (bool, error) {
	if err := l.check(1); err != nil {
		return false, err
	}
	if b == l.cursor[0] {
		if err := l.skip(1); err != nil {
			return false, err
		}
		return true, nil
	}
	return false, nil
}

func (l *Lexer) matchBytesTight(b []byte) (bool, error) {
	n := len(b)
	if err := l.check(n); err != nil {
		return false, err
	}
	ok := bytes.Equal(b, l.cursor[:n])
	if ok {
		l.skip(n)
	}
	return ok, nil
}

func (l *Lexer) readByte() (byte, error) {
	if err := l.check(1); err != nil {
		return 0, err
	}
	b := l.cursor[0]
	l.cursor = l.cursor[1:]
	if b == '\n' {
		l.line++
	}
	return b, nil
}

func (l *Lexer) peekRune() (rune, int, error) {
	if !utf8.FullRune(l.cursor) {
		err := l.fill(utf8.UTFMax)
		if len(l.cursor) == 0 {
			if err == nil {
				err = io.EOF
			}
			return 0, 0, err
		}
	}
	r, n := utf8.DecodeRune(l.cursor)
	return r, n, nil
}

var slashCommentStart = []byte("//")
var starCommentStart = []byte("/*")

func (l *Lexer) skipSpace() error {
	for {
		r, n, err := l.peekRune()
		if err != nil {
			return err
		}
		if unicode.IsSpace(r) {
			l.skip(n)
			continue
		}
		if r == '/' {
			ok, err := l.matchBytesTight(slashCommentStart)
			if err != nil {
				return err
			}
			if ok {
				if err := l.skipLine(); err != nil {
					return err
				}
				continue
			}
			ok, err = l.matchBytesTight(starCommentStart)
			if err != nil {
				return err
			}
			if ok {
				if err := l.skipMultiLine(); err != nil {
					return err
				}
				continue
			}
		}
		return nil
	}
}

func isNewline(r rune) bool {
	// See http://www.unicode.org/versions/Unicode13.0.0/ch05.pdf#G10213
	switch r {
	case 0x000A, 0x000B, 0x000C, 0x000D, 0x0085, 0x2028, 0x2029:
		return true
	}
	return false
}

func (l *Lexer) skipLine() error {
	for {
		r, n, err := l.peekRune()
		if err != nil {
			return err
		}
		l.skip(n)
		if isNewline(r) {
			return nil
		}
	}
}

func (l *Lexer) skipMultiLine() error {
	for {
		b, err := l.readByte()
		if err != nil {
			return err
		}
		for b == '*' {
			b, err = l.readByte()
			if err != nil {
				return err
			}
			if b == '/' {
				return nil
			}
		}
	}
}

// scanString scans a quoted string presuming the first double quote has
// already been matched and consumed from the input.  scanString does not
// consume the terminating double quote.
func (l *Lexer) scanString() (string, error) {
	var s strings.Builder
	// We optimistically try to scan the string as a basic ascii string
	// with standard escapes, \t, \n etc.  If we hit \u or any non-ascii UTF
	// we read the rest of the string into a bytes buffer and call scanStringBytes()
	// to finish the job.
	for {
		c, err := l.peekByte()
		if err != nil {
			return "", err
		}
		if c == '"' {
			return s.String(), nil
		}
		if c >= utf8.RuneSelf {
			bytes, err := l.scanToCloseQuote(nil)
			if err != nil {
				return "", err
			}
			return parseStringBytes(&s, bytes)
		}
		l.skip(1)
		if c == '\n' {
			return "", errors.New("unescaped line break")
		}
		if c == '\\' {
			c, err = l.readByte()
			if err != nil {
				if err == io.EOF {
					err = errors.New("no end quote")
				}
				return "", err
			}
			switch c {
			case 'u':
				bytes, err := l.scanToCloseQuote([]byte{'\\', 'u'})
				if err != nil {
					return "", err
				}
				return parseStringBytes(&s, bytes)
			case '"', '\\', '/':
				// Write this byte below as is.
			case 'b':
				c = '\b'
			case 'f':
				c = '\f'
			case 'n':
				c = '\n'
			case 'r':
				c = '\r'
			case 't':
				c = '\t'
			default:
				return "", fmt.Errorf("illegal escape (\\%c)", c)
			}
		}
		s.WriteByte(c)
	}
}

// scanToCloseQuote scans the input into the slice appending to the b.
// It peeks at but does not consume the final end quote character.
func (l *Lexer) scanToCloseQuote(b []byte) ([]byte, error) {
	for {
		c, err := l.peekByte()
		if err != nil {
			return nil, err
		}
		if c == '"' {
			return b, nil
		}
		l.skip(1)
		if c == '\\' {
			b = append(b, '\\')
			c, err = l.readByte()
			if err != nil {
				return nil, err
			}
		}
		b = append(b, c)
	}
}

// parseStringBytes parse unicode escapes and converts utf-16 surrogage pairs
// into utf-8 sequences.  It was copied and modified [with attribution](https://github.com/superdb/super/blob/main/acknowledgments.txt)
// from the encoding/json package in the Go source code.
func parseStringBytes(b *strings.Builder, bytes []byte) (string, error) {
	k := 0
	for k < len(bytes) {
		switch c := bytes[k]; {
		case c == '\\':
			k++
			if k >= len(bytes) {
				panic("can't happen because string scanner would look for next char")
			}
			switch c := bytes[k]; c {
			default:
				return "", fmt.Errorf("illegal escape (\\%c) in string", c)
			case '"', '\\', '/', '\'':
				b.WriteByte(bytes[k])
				k++
			case 'b':
				b.WriteByte('\b')
				k++
			case 'f':
				b.WriteByte('\f')
				k++
			case 'n':
				b.WriteByte('\n')
				k++
			case 'r':
				b.WriteByte('\r')
				k++
			case 't':
				b.WriteByte('\t')
				k++
			case 'u':
				k++
				r, err := unhexRune(bytes[k:])
				if err != nil {
					return "", err
				}
				k += 4
				if utf16.IsSurrogate(r) {
					if len(bytes) < 6 || bytes[0] != '\\' || bytes[1] != 'u' {
						return "", errors.New("illegal surrogate utf-16 rune pair")
					}
					r2, err := unhexRune(bytes[k+2:])
					if err != nil {
						return "", err
					}
					k += 6
					if dec := utf16.DecodeRune(r, r2); dec != unicode.ReplacementChar {
						// A valid pair; consume.
						if _, err := b.WriteRune(dec); err != nil {
							return "", err
						}
					}
				} else if _, err := b.WriteRune(r); err != nil {
					return "", err
				}
			}
		case c == '"':
			// This would be a bug as the string scanner should not
			// allow this to happen.
			panic("unescaped quote encountered in string")
		case c < ' ':
			return "", errors.New("illegal control code")
		// ASCII
		case c < utf8.RuneSelf:
			b.WriteByte(c)
			k++
		// Coerce to well-formed UTF-8.
		default:
			r, size := utf8.DecodeRune(bytes[k:])
			b.WriteRune(r)
			k += size
		}
	}
	return b.String(), nil
}

func unhexRune(b []byte) (rune, error) {
	if len(b) < 4 {
		return 0, errors.New("short \\u escape")
	}
	r0 := rune(Unhex(b[0]))
	r1 := rune(Unhex(b[1]))
	r2 := rune(Unhex(b[2]))
	r3 := rune(Unhex(b[3]))
	if r0 > 0xf || r1 > 0xf || r2 > 0xf || r3 > 0xf {
		return 0, errors.New("invalid hex digits in \\u escape")
	}
	return r0<<12 | r1<<8 | r2<<4 | r3, nil
}

func (l *Lexer) scanTypeName() (string, error) {
	ok, err := l.match('"')
	if err != nil {
		return "", noEOF(err)
	}
	if ok {
		s, err := l.scanString()
		if err != nil {
			return "", noEOF(err)
		}
		l.skip(1)
		return s, nil
	}
	var s strings.Builder
	for {
		r, n, err := l.peekRune()
		if err != nil {
			if err == io.EOF {
				return s.String(), nil
			}
			return "", err
		}
		if !typeChar(r) {
			return s.String(), nil
		}
		s.WriteRune(r)
		l.skip(n)
	}
}

func (l *Lexer) scanIdentifier() (string, error) {
	s, err := l.scanTypeName()
	if err != nil {
		return "", err
	}
	if !IsIdentifier(s) {
		return "", errors.New("malformed identifier")
	}
	return s, nil
}

// peekPrimitive returns a string that may be a candidate for a primitive token
// exlusive of string literals.  This works by scanning forward until we see
// whitespace or EOF while keeping everything buffered.  This could be made more
// efficient by keeping track of the whitespace boundary and only looking for
// it when we're past it (or by implementing a proper DFA for literal matching).
func (l *Lexer) peekPrimitive() (string, error) {
	var err error
	var off int
	for {
		err = l.check(off + utf8.UTFMax)
		if err != nil {
			break
		}
		r, n := utf8.DecodeRune(l.cursor[off:])
		if unicode.IsSpace(r) || r == ',' {
			break
		}
		off += n
	}
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		return "", err
	}
	if len(l.cursor) == 0 {
		return "", io.EOF
	}
	return string(l.ip6trim(l.primitive.Find(l.cursor), l.cursor)), nil
}

// IP6 addresses can overmatch into the decorator colons and into the
// the type name after a cast.  Detect this condition and return
// the match prior adjusted up to the cast.  If not detected,
// return the match.
func (l *Lexer) ip6trim(match, cursor []byte) []byte {
	if match == nil {
		return nil
	}
	colonsAt := bytes.LastIndex(match, []byte("::"))
	if colonsAt < 0 || bytes.IndexByte(match, '/') >= 0 {
		// No trim if not IP6 or IP6 but appended with slash network.
		return match
	}
	follows := cursor[len(match):]
	if len(follows) == 0 || len(l.cursor) <= colonsAt+2 || l.ip6follow.Find(follows) != nil {
		return match
	}
	return match[:colonsAt]
}
