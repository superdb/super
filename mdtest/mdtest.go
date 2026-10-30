// Package mdtest finds example shell commands in Markdown files and runs them,
// checking for expected output and exit status.
//
// Example inputs, commands, and outputs are specified in fenced code blocks
// whose info string (https://spec.commonmark.org/0.29/#info-string) has
// mdtest-input, mdtest-command, or mdtest-output as the first word.  The
// mdtest-command and mdtest-output blocks must be paired.
//
//	```mdtest-input file.txt
//	hello
//	```
//	```mdtest-command [dir=...] [fails]
//	cat file.txt
//	```
//	```mdtest-output [head]
//	hello
//	```
//
// The content of each mdtest-command block is fed to "bash -e -o pipefail" on
// standard input.
//
// The shell's working directory is a temporary directory populated with files
// described by any mdtest-input blocks in the same Markdown file and shared by
// other tests in the same file.  Alternatively, if the mdtest-command block's
// info string contains a word prefixed with "dir=", the rest of that word
// specifies the shell's working directory as a path relative to the repository
// root, and files desribed by mdtest-input blocks are not available.
//
// The shell's exit status must indicate success (i.e., be zero) unless the
// mdtest-command block's info string contains the word "fails", in which case
// the exit status must indicate failure (i.e. be nonzero).
//
// The shell's combined standard output and standard error must exactly match
// the content of the following mdtest-output block unless that block's info
// string contains the word "head", in which case any "...\n" suffix of the
// block content is ignored, and what remains must be a prefix of the shell
// output.
//
//	```mdtest-command
//	echo hello
//	echo goodbye
//	```
//	```mdtest-output head
//	hello
//	...
//	```
//
// # SPQ tests
//
// A fenced code block with mdtest-spq as the first word of its info string
// contains an SPQ test.  The content of the block must comprise three sections,
// each preceeded by one or more "#"-prefixed lines.  The first section contains
// an SPQ program, the second contains input provided to the program when the
// test runs, and the third contains the program's expected output.  If the
// second section contains only whitespace, no input is provided when the test
// runs.
//
// SPQ tests are run via the super command.  The command's exit status must
// indicate success (i.e., be zero) unless the mdtest-spq block's info string
// contains the word "fails", in which case the exit status must indicate
// failure (i.e. be nonzero).
//
//	```mdtest-spq [fails]
//	# spq
//	values a
//	# input
//	{a:1}
//	# expected output
//	1
//	```
package mdtest

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
)

// File represents a Markdown file and the tests it contains.
type File struct {
	Path   string
	Inputs map[string]string
	Tests  []*Test
}

// Load walks the file tree rooted at the current working directory, looking for
// Markdown files containing tests.  Any file whose name ends with ".md" is
// considered a Markdown file.  Files containing no tests are ignored.
func Load() ([]*File, error) {
	var files []*File
	err := filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".md") {
			return err
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		inputs, tests, err := parseMarkdown(b)
		if err != nil {
			if le, ok := errors.AsType[lineError](err); ok {
				return fmt.Errorf("%s:%d: %s", path, le.line, le.msg)
			}
			return fmt.Errorf("%s: %w", path, err)
		}
		if len(tests) > 0 {
			files = append(files, &File{
				Path:   path,
				Inputs: inputs,
				Tests:  tests,
			})
		}
		return nil
	})
	return files, err
}

// Run runs the file's tests.  It runs tests sequentially if Test.Command != ""
// and in parallel otherwise.  It runs relative-directory-style tests (Test.Dir
// != "") with the working directory set to Test.Dir, and it runs
// temporary-directory-style tests (Test.Dir == ""), with the working directory
// set to a shared temporary directory.
func (f *File) Run(t *testing.T) {
	tempDir := t.TempDir()
	for filename, content := range f.Inputs {
		if err := os.WriteFile(filepath.Join(tempDir, filename), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	for _, tt := range f.Tests {
		t.Run(strconv.Itoa(tt.Line), func(t *testing.T) {
			if err := tt.Run(tempDir); err != nil {
				// Lead with newline so line-numbered errors are
				// navigable in editors.
				t.Fatalf("\n%s:%d: %s", f.Path, tt.Line, err)
			}
		})
	}
}

// Matches one or more "#"-prefixed lines.
var spqSeparatorRE = regexp.MustCompile(`(?m:^#.*\n)+`)

func parseMarkdown(source []byte) (map[string]string, []*Test, error) {
	var commandFCB *ast.FencedCodeBlock
	var inputs map[string]string
	var tests []*Test
	doc := goldmark.DefaultParser().Parse(text.NewReader(source))
	err := ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		fcb, ok := n.(*ast.FencedCodeBlock)
		if !ok || !entering {
			return ast.WalkContinue, nil
		}
		switch string(fcb.Language(source)) {
		case "mdtest-input":
			words := fcbInfoWords(fcb, source)
			if len(words) < 2 {
				return ast.WalkStop, errors.New("mdtest-input without file name")
			}
			filename := words[1]
			if inputs == nil {
				inputs = map[string]string{}
			}
			if _, ok := inputs[filename]; ok {
				return ast.WalkStop, errors.New("mdtest-input with duplicate file name")
			}
			inputs[filename] = fcbLines(fcb, source)
		case "mdtest-command":
			if commandFCB != nil {
				return ast.WalkStop, fcbError(commandFCB, source, "unpaired mdtest-command")
			}
			commandFCB = fcb
		case "mdtest-output":
			if commandFCB == nil {
				return ast.WalkStop, fcbError(fcb, source, "unpaired mdtest-output")
			}
			var commandDir string
			var commandFails bool
			for _, s := range fcbInfoWords(commandFCB, source)[1:] {
				switch {
				case strings.HasPrefix(s, "dir="):
					commandDir = strings.TrimPrefix(s, "dir=")
				case s == "fails":
					commandFails = true
				default:
					msg := fmt.Sprintf("unknown word in mdtest-command info string: %q", s)
					return ast.WalkStop, fcbError(commandFCB, source, msg)
				}
			}
			expected := fcbLines(fcb, source)
			var head bool
			if words := fcbInfoWords(fcb, source); len(words) > 1 && words[1] == "head" {
				expected = strings.TrimSuffix(expected, "...\n")
				head = true
			}
			tests = append(tests, &Test{
				Command:  fcbLines(commandFCB, source),
				Dir:      commandDir,
				Expected: expected,
				Fails:    commandFails,
				Head:     head,
				Line:     fcbLineNumber(commandFCB, source),
			})
			commandFCB = nil
		case "mdtest-go-example":
			tests = append(tests, &Test{
				GoExample: fcbLines(fcb, source),
				Line:      fcbLineNumber(fcb, source),
			})
		case "mdtest-spq":
			var fails bool
			var fusion bool
			for _, word := range fcbInfoWords(fcb, source)[1:] {
				switch word {
				case "fails":
					fails = true
				case "fusion":
					fusion = true
				}
			}
			lines := fcbLines(fcb, source)
			if !strings.HasPrefix(lines, "#") {
				return ast.WalkStop, fcbError(fcb, source, "mdtest-spq content must begin with '#'")
			}
			sections := spqSeparatorRE.Split(lines, -1)
			// Ignore sections[0].
			if n := len(sections); n != 4 {
				msg := fmt.Sprintf("mdtest-spq content has %d '#'-prefixed sections (expected 3)", n-1)
				return ast.WalkStop, fcbError(fcb, source, msg)
			}
			tests = append(tests, &Test{
				Expected: sections[3],
				Fails:    fails,
				Line:     fcbLineNumber(fcb, source),
				Input:    sections[2],
				SPQ:      sections[1],
				Fusion:   fusion,
			})
		}
		return ast.WalkContinue, nil
	})
	if err != nil {
		return nil, nil, err
	}
	if commandFCB != nil {
		return nil, nil, fcbError(commandFCB, source, "unpaired mdtest-command")
	}
	return inputs, tests, nil
}

func fcbError(fcb *ast.FencedCodeBlock, source []byte, msg string) error {
	return lineError{line: fcbLineNumber(fcb, source), msg: msg}
}

func fcbInfoWords(fcb *ast.FencedCodeBlock, source []byte) []string {
	return strings.Fields(string(fcb.Info.Segment.Value(source)))
}

func fcbLineNumber(fcb *ast.FencedCodeBlock, source []byte) int {
	return bytes.Count(source[:fcb.Info.Segment.Start], []byte("\n")) + 1
}

func fcbLines(fcb *ast.FencedCodeBlock, source []byte) string {
	var b strings.Builder
	segments := fcb.Lines()
	for _, s := range segments.Sliced(0, segments.Len()) {
		b.Write(s.Value(source))
	}
	return b.String()
}

type lineError struct {
	line int
	msg  string
}

func (l lineError) Error() string {
	return fmt.Sprintf("line %d: %s", l.line, l.msg)
}
