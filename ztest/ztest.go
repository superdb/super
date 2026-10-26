// Package ztest runs formulaic tests ("ztests") that can be (1) run in-process
// with the compiled-in code base or (2) run as a bash script running a sequence
// of arbitrary shell commands invoking any of the build artifacts.  The
// first two cases comprise the "SPQ test style" and the last case
// comprises the "script test style".  Case (1) is easier to debug by
// simply running "go test" compared replicating the test using "go run".
// Script-style tests don't have this convenience.
//
// Ztests are specified in YAML files.  Multiple ztests can be specified in a
// single file by separating them with a line containing three dashes ("---").
//
// In the SPQ style, ztest runs a SuperSQL program on an input and checks
// for an expected output.
//
//	spq: count()
//
//	input: |
//	  #0:record[i:int64]
//	  0:[1;]
//	  0:[2;]
//
//	output: |
//	  #0:record[count:uint64]
//	  0:[2;]
//
// Input format is detected automatically and can be anything recognized by
// "super -i auto" (including optional gzip compression).  Output format defaults
// to SUP but can be set to anything accepted by "super -f".
//
//	spq: count()
//
//	flags: -f table
//
//	input: |
//	  #0:record[i:int64]
//	  0:[1;]
//	  0:[2;]
//
//	output: |
//	  count
//	  2
//
// Alternatively, tests can be configured to run as shell scripts.
// In this style of test, arbitrary bash scripts can run chaining together
// any of the tools in cmd/ in addition to super.  Scripts are executed by "bash -e
// -o pipefail", and a nonzero shell exit code causes a test failure, so any failed
// command generally results in a test failure.  Here, the yaml sets up a collection
// of input files and stdin, the script runs, and the test driver compares expected
// output files, stdout, and stderr with data in the yaml spec.  In this case,
// instead of specifying, "spq", "input", "output", you specify the yaml arrays
// "inputs" and "outputs" --- where each array element defines a file, stdin,
// stdout, or stderr --- and a "script" that specifies a multi-line yaml string
// defining the script, e.g.,
//
// inputs:
//   - name: in1.sup
//     data: |
//     #0:record[i:int64]
//     0:[1;]
//   - name: stdin
//     data: |
//     #0:record[i:int64]
//     0:[2;]
//
// script: |
//
//	super -o out.sup in1.sup -
//	super -o count.sup "count()" out.sup
//
// outputs:
//   - name: out.sup
//     data: |
//     #0:record[i:int64]
//     0:[1;]
//     0:[2;]
//   - name: count.sup
//     data: |
//     #0:record[count:uint64]
//     0:[2;]
//
// Each input and output has a name.  For inputs, a file (source)
// or inline data (data) may be specified.
// If no data is specified, then a file of the same name as the
// name field is looked for in the same directory as the yaml file.
// The source spec is a file path relative to the directory of the
// yaml file.  For outputs, expected output is defined in the same
// fashion as the inputs though you can also specify a "regexp" string
// instead of expected data.  If an output is named "stdout" or "stderr"
// then the actual output is taken from the stdout or stderr of the
// the shell script.
//
// Ztest YAML files for a package should reside in a subdirectory named
// testdata/ztest.
//
//	pkg/
//	  pkg.go
//	  pkg_test.go
//	  testdata/
//	    ztest/
//	      test-1.yaml
//	      test-2.yaml
//	      ...
//
// Name YAML files descriptively since each ztest runs as a subtest
// named for the file that defines it.
//
// pkg_test.go should contain a Go test named TestZTest that calls Run.
//
//	func TestZTest(t *testing.T) { ztest.Run(t, "testdata/ztest") }
//
// If the ZTEST_PATH environment variable is unset or empty and the test
// is not a script test, Run runs ztests in the current process and skips
// the script tests.  Otherwise, Run runs each ztest in a separate process
// using the super executable in the directories specified by ZTEST_PATH.
//
// Tests of either style can be skipped by setting the skip field to a non-empty
// string.  A message containing the string will be written to the test log.
package ztest

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/goccy/go-yaml"
	yamlparser "github.com/goccy/go-yaml/parser"
	"github.com/pmezard/go-difflib/difflib"
	"github.com/superdb/super"
	"github.com/superdb/super/cli/inputflags"
	"github.com/superdb/super/cli/outputflags"
	"github.com/superdb/super/compiler"
	"github.com/superdb/super/compiler/parser"
	"github.com/superdb/super/pkg/storage"
	"github.com/superdb/super/runtime"
	"github.com/superdb/super/runtime/exec"
	"github.com/superdb/super/sio"
	"github.com/superdb/super/sio/anyio"
	"github.com/superdb/super/vector/vio"
)

func ShellPath() string {
	return os.Getenv("ZTEST_PATH")
}

type Bundle struct {
	FileName string
	Test     *ZTest
	Error    error
}

func Load(dirname string) ([]Bundle, error) {
	var bundles []Bundle
	fileinfos, err := os.ReadDir(dirname)
	if err != nil {
		return nil, err
	}
	for _, fi := range fileinfos {
		filename := fi.Name()
		if !strings.HasSuffix(filename, ".yaml") {
			continue
		}
		filename = filepath.Join(dirname, filename)
		zts, err := FromYAMLFile(filename)
		if err != nil {
			bundles = append(bundles, Bundle{filename, nil, err})
			continue
		}
		for _, zt := range zts {
			bundles = append(bundles, Bundle{filename, zt, nil})
		}
	}
	return bundles, nil
}

// Run runs the ztests in the directory named dirname.  For each file f.yaml in
// the directory, Run calls FromYAMLFile to load a ztest and then runs it in
// subtest named f.  path is a command search path like the
// PATH environment variable.
func Run(t *testing.T, dirname string) {
	shellPath := ShellPath()
	bundles, err := Load(dirname)
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range bundles {
		name := filepath.Base(b.FileName)
		if z := b.Test; z != nil {
			name = fmt.Sprintf("%s/%d", name, z.Line)
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if b.Error != nil {
				t.Fatalf("%s: %s", b.FileName, b.Error)
			}
			b.Test.Run(t, shellPath, b.FileName)
		})
	}
}

type File struct {
	// Name is the name of the file with respect to the directoy in which
	// the test script runs.  For inputs, if no data source is specified,
	// then name is also the name of a data file in the diectory containing
	// the yaml test file, which is copied to the test script directory.
	// Name can also be stdio (for inputs) or stdout or stderr (for outputs).
	Name string `yaml:"name"`
	// Data and Source represent the different ways file data can
	// be defined for this file.  Data is a string turned into the contents
	// of the file. Source is a string representing
	// the pathname of a file the repo that is read to comprise the data.
	Data   *string `yaml:"data,omitempty"`
	Source string  `yaml:"source,omitempty"`
	// Re is a regular expression describing the contents of the file,
	// which is only applicable to output files.
	Re string `yaml:"regexp,omitempty"`
}

func (f *File) check() error {
	cnt := 0
	if f.Data != nil {
		cnt++
	}
	if f.Source != "" {
		cnt++
	}
	if cnt > 1 {
		return fmt.Errorf("%s: must specify at most one of data or source", f.Name)
	}
	return nil
}

func (f *File) load(dir string) ([]byte, *regexp.Regexp, error) {
	if f.Data != nil {
		return []byte(*f.Data), nil, nil
	}
	if f.Source != "" {
		b, err := os.ReadFile(filepath.Join(dir, f.Source))
		return b, nil, err
	}
	if f.Re != "" {
		re, err := regexp.Compile(f.Re)
		return nil, re, err
	}
	b, err := os.ReadFile(filepath.Join(dir, f.Name))
	if err == nil {
		return b, nil, nil
	}
	if os.IsNotExist(err) {
		err = fmt.Errorf("%s: no data source", f.Name)
	}
	return nil, nil, err
}

// ZTest defines a ztest.
type ZTest struct {
	Line int    `yaml:"-"`
	Skip string `yaml:"skip,omitempty"`
	Tag  string `yaml:"tag,omitempty"`

	// For SPQ-style tests.
	SPQ    string  `yaml:"spq,omitempty"`
	Flags  string  `yaml:"flags,omitempty"`
	Input  *string `yaml:"input,omitempty"`
	Output string  `yaml:"output,omitempty"`
	Error  string  `yaml:"error,omitempty"`

	// For script-style tests.
	Script  string   `yaml:"script,omitempty"`
	Inputs  []File   `yaml:"inputs,omitempty"`
	Outputs []File   `yaml:"outputs,omitempty"`
	Env     []string `yaml:"env,omitempty"`
}

func (z *ZTest) check() error {
	if z.Script != "" {
		if z.Outputs == nil {
			return errors.New("outputs field missing in a sh test")
		}
		for _, f := range z.Inputs {
			if err := f.check(); err != nil {
				return err
			}
			if f.Re != "" {
				return fmt.Errorf("%s: cannot use regexp in an input", f.Name)
			}
		}
		for _, f := range z.Outputs {
			if err := f.check(); err != nil {
				return err
			}
		}
	} else if z.SPQ == "" {
		return errors.New("either a spq field or script field must be present")
	}
	return nil
}

// FromYAMLFile loads ztests from the named YAML file.
func FromYAMLFile(name string) ([]*ZTest, error) {
	f, err := yamlparser.ParseFile(name, 0)
	if err != nil {
		return nil, err
	}
	var zs []*ZTest
	for _, d := range f.Docs {
		var z ZTest
		if err := yaml.NodeToValue(d.Body, &z, yaml.DisallowUnknownField()); err != nil {
			return nil, err
		}
		if s := d.Start; s != nil {
			// s is a YAML directives end marker ("---").
			z.Line = s.Position.Line + 1
		} else {
			z.Line = 1
		}
		zs = append(zs, &z)
	}
	return zs, nil
}

func (z *ZTest) ShouldSkip(path string) string {
	switch {
	case z.Script != "" && path == "":
		return "script test on in-process run"
	case z.SPQ != "" && path != "":
		return "in-process test on script run"
	case z.Skip != "":
		return z.Skip
	case z.Tag != "" && z.Tag != os.Getenv("ZTEST_TAG"):
		return fmt.Sprintf("tag %q does not match ZTEST_TAG=%q", z.Tag, os.Getenv("ZTEST_TAG"))
	}
	return ""
}

func (z *ZTest) diffInternal(out string, err error) error {
	var outDiffErr, errDiffErr error
	if z.Output != out {
		outDiffErr = diffErr("output", z.Output, out)
	}
	var errStr string
	if err != nil {
		// Append newline if err doesn't end with one.
		errStr = strings.TrimSuffix(err.Error(), "\n") + "\n"
	}
	if z.Error != errStr {
		errDiffErr = diffErr("error", z.Error, errStr)
	}
	return errors.Join(outDiffErr, errDiffErr)
}

func (z *ZTest) Run(t *testing.T, path, filename string) {
	if msg := z.ShouldSkip(path); msg != "" {
		t.Skip("skipping test:", msg)
	}
	var err error
	if z.Script != "" {
		err = z.RunScript(t.Context(), path, filepath.Dir(filename), t.TempDir())
	} else {
		err = z.RunInternal(t.Context())
	}
	if err != nil {
		// Lead with newline so line-numbered errors are
		// navigable in editors.
		t.Fatalf("\n%s:%d: %s", filename, z.Line, err)
	}
}

func diffErr(name, expected, actual string) error {
	if !utf8.ValidString(expected) {
		expected = hex.Dump([]byte(expected))
		actual = hex.Dump([]byte(actual))
	}
	diff, err := difflib.GetUnifiedDiffString(difflib.UnifiedDiff{
		A:        difflib.SplitLines(expected),
		FromFile: "expected",
		B:        difflib.SplitLines(actual),
		ToFile:   "actual",
		Context:  5,
	})
	if err != nil {
		panic("ztest: " + err.Error())
	}
	return fmt.Errorf("expected and actual %s differ:\n%s", name, diff)
}

func (z *ZTest) RunScript(ctx context.Context, path, testDir, tempDir string) error {
	if err := z.check(); err != nil {
		return fmt.Errorf("bad yaml format: %w", err)
	}
	var stdin io.Reader
	for _, f := range z.Inputs {
		b, _, err := f.load(testDir)
		if err != nil {
			return err
		}
		if f.Name == "stdin" {
			stdin = bytes.NewReader(b)
			continue
		}
		if err := os.WriteFile(filepath.Join(tempDir, f.Name), b, 0644); err != nil {
			return err
		}
	}
	stdout, stderr, err := RunShell(ctx, tempDir, path, z.Script, stdin, z.Env, nil)
	if err != nil {
		return fmt.Errorf("script failed: %w\n=== stdout ===\n%s=== stderr ===\n%s",
			err, stdout, stderr)
	}
	for _, f := range z.Outputs {
		var actual string
		switch f.Name {
		case "stdout":
			actual = stdout
		case "stderr":
			actual = stderr
		default:
			b, err := os.ReadFile(filepath.Join(tempDir, f.Name))
			if err != nil {
				return fmt.Errorf("%s: %w", f.Name, err)
			}
			actual = string(b)
		}
		expected, expectedRE, err := f.load(testDir)
		if err != nil {
			return err
		}
		if expected != nil && string(expected) != actual {
			return diffErr(f.Name, string(expected), actual)
		}
		if expectedRE != nil && !expectedRE.MatchString(actual) {
			return fmt.Errorf("%s: regexp %q does not match %q", f.Name, expectedRE, actual)
		}
	}
	return nil
}

func (z *ZTest) RunInternal(ctx context.Context) error {
	if err := z.check(); err != nil {
		return fmt.Errorf("bad yaml format: %w", err)
	}
	return z.diffInternal(z.runInternal(ctx))
}

func (z *ZTest) runInternal(ctx context.Context) (string, error) {
	ast, err := parser.ParseText(z.SPQ)
	if err != nil {
		return "", err
	}
	args := append([]string{"-f=sup", "-pretty=0"}, strings.Fields(z.Flags)...)
	var fs flag.FlagSet
	var inflags inputflags.Flags
	var outflags outputflags.Flags
	inflags.SetFlags(&fs)
	outflags.SetFlags(&fs)
	if err := fs.Parse(args); err != nil {
		return "", err
	}
	if err := inflags.Init(); err != nil {
		return "", err
	}
	if err := outflags.Init(); err != nil {
		return "", err
	}
	eng := storage.NewInternalEngine()
	if i := z.Input; i != nil {
		ast.PrependFileScan([]string{"stdio:stdin"})
		eng.AddBytes("stdio:stdin", []byte(*i))
	}
	env := exec.NewEnvironment(eng, nil)
	env.Dynamic = inflags.Dynamic
	env.ReaderOpts = inflags.ReaderOpts
	// Do static type checking for all tests except those with -dynamic.
	env.Static = !inflags.Dynamic
	q, err := runtime.CompileQuery(ctx, super.NewContext(), compiler.NewCompilerWithEnv(env), ast)
	if err != nil {
		return "", err
	}
	defer q.Pull(true)
	var outbuf bytes.Buffer
	zw, err := anyio.NewWriter(sio.NopCloser(&outbuf), outflags.Options())
	if err != nil {
		return "", err
	}
	err = vio.Copy(zw, q)
	if err2 := zw.Close(); err == nil {
		err = err2
	}
	return outbuf.String(), err
}
