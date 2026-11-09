package srcfiles

import (
	"os"
	"sort"
	"strings"
	"unicode"
)

type Input interface {
	Load() (string, error)
}

// PlainInput is source text.  Name, if not empty, names its source (e.g.,
// a file that was read by the caller) in error messages.
type PlainInput struct {
	Name string
	Text string
}

func (p *PlainInput) Load() (string, error) {
	return p.Text, nil
}

type FileInput struct {
	Name string
}

func (f *FileInput) Load() (string, error) {
	b, err := os.ReadFile(f.Name)
	return string(b), err
}

type List struct {
	Text   string
	Files  []File
	errors ErrorList
}

func (l *List) AddError(msg string, pos, end int) {
	l.errors.Append(l, msg, pos, end)
}

func (l *List) Error() error {
	if len(l.errors) == 0 {
		return nil
	}
	return l.errors
}

func (l *List) FileOf(pos int) File {
	i := sort.Search(len(l.Files), func(i int) bool { return l.Files[i].start > pos }) - 1
	return l.Files[i]
}

// Concat reads in the indicated files and concatenates their content with
// newlines appending the final query text.
func Concat(inputs []Input) (*List, error) {
	var b strings.Builder
	var files []File
	var needSep bool
	for _, input := range inputs {
		// Empty string is the unnamed query text while
		// the included files all have names.
		var name string
		var bytes []byte
		switch input := input.(type) {
		case *PlainInput:
			name = input.Name
			bytes = []byte(input.Text)
		case *FileInput:
			name = input.Name
			var err error
			bytes, err = os.ReadFile(input.Name)
			if err != nil {
				return nil, err
			}
		}
		// Skip over any empties.
		if len(bytes) > 0 {
			files = append(files, newFile(name, b.Len(), bytes))
			// Separate file content with a newline but only when needed.
			if needSep {
				b.WriteByte('\n')
			}
			b.Write(bytes)
			needSep = !unicode.IsSpace(rune(bytes[len(bytes)-1]))
		}
	}
	return &List{Text: b.String(), Files: files}, nil
}

func Plain(src string) []Input {
	return []Input{&PlainInput{Text: src}}
}
