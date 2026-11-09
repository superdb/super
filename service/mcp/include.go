package mcp

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/superdb/super/compiler/srcfiles"
)

// maxListedIncludes bounds the number of include files listed in the
// server's instructions and the include argument's description.
const maxListedIncludes = 50

// includeArg is the include argument of the query tool: an array of file
// names or, for clients whose forms make arrays awkward (e.g., n8n), a
// string of file names separated by commas.
type includeArg []string

func (a *includeArg) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err == nil {
		*a = nil
		for name := range strings.SplitSeq(s, ",") {
			if name = strings.TrimSpace(name); name != "" {
				*a = append(*a, name)
			}
		}
		return nil
	}
	var names []string
	if err := json.Unmarshal(b, &names); err != nil {
		return errors.New("include must be an array of strings or a string")
	}
	*a = names
	return nil
}

func (s *Server) includeSchema() map[string]any {
	desc := `SuperSQL source files on the server whose text precedes the query in the
order given, e.g., ["common.spq"], so the query may use the constants, functions,
and operators they declare.  The names are relative to the server's include
directory.  A string of names separated by commas is also accepted.`
	items := map[string]any{"type": "string"}
	if files := s.includeFiles(); len(files) > 0 {
		desc += "  The include files are " + strings.Join(files, ", ") + "."
		// Listing the files as the items' enum lets a client offer them
		// as choices (e.g., n8n prefills the argument with the first).
		// The list is a snapshot, so a client sees a file added later
		// when it lists the tools again.
		items["enum"] = files
	}
	return map[string]any{
		"type":        "array",
		"items":       items,
		"description": desc,
	}
}

// includeFiles returns the names of the .spq files in the include
// directory and its subdirectories, sorted, or nil if there are none or
// there are more than maxListedIncludes.  Files with other names may
// still be included.
func (s *Server) includeFiles() []string {
	var files []string
	dir := s.opts.IncludeDir
	filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.Type().IsRegular() && strings.HasSuffix(d.Name(), ".spq") {
			if rel, err := filepath.Rel(dir, path); err == nil {
				files = append(files, filepath.ToSlash(rel))
			}
		}
		if len(files) > maxListedIncludes {
			return filepath.SkipAll
		}
		return nil
	})
	if len(files) > maxListedIncludes {
		return nil
	}
	slices.Sort(files)
	return files
}

// includeInputs returns the source inputs of the include files named by
// names.  A file must be in the include directory: a name that is
// absolute, contains "..", or resolves (e.g., via a symbolic link) outside
// the directory is rejected so a client can't read other files on the
// server (e.g., by way of a parse error's message).
func (s *Server) includeInputs(names []string) ([]srcfiles.Input, error) {
	if len(names) == 0 {
		return nil, nil
	}
	if s.opts.IncludeDir == "" {
		return nil, invalidArgs("include is not available since the server has no include directory")
	}
	var inputs []srcfiles.Input
	for _, name := range names {
		if !filepath.IsLocal(filepath.FromSlash(name)) {
			return nil, invalidArgs("include %q must be a relative path within the include directory", name)
		}
		text, err := readInRoot(s.opts.IncludeDir, filepath.FromSlash(name))
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return nil, invalidArgs("include %q: no such file in the include directory", name)
			}
			// Don't reveal the server's file system beyond name.
			return nil, invalidArgs("include %q: file can't be read from the include directory", name)
		}
		inputs = append(inputs, &srcfiles.PlainInput{Name: name, Text: text})
	}
	return inputs, nil
}

func readInRoot(dir, name string) (string, error) {
	f, err := os.OpenInRoot(dir, name)
	if err != nil {
		return "", err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", errors.New("not a regular file")
	}
	b, err := io.ReadAll(f)
	return string(b), err
}

// CheckIncludeDir returns an error if dir, an include directory (see
// Options.IncludeDir), is not empty and isn't a directory.
func CheckIncludeDir(dir string) error {
	if dir == "" {
		return nil
	}
	info, err := os.Stat(dir)
	if err != nil {
		return fmt.Errorf("include directory: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("include directory %s is not a directory", dir)
	}
	return nil
}
