package super_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime/debug"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/superdb/super"
	"github.com/superdb/super/compiler"
	"github.com/superdb/super/compiler/parser"
	"github.com/superdb/super/runtime"
	"github.com/superdb/super/sbuf"
	"github.com/superdb/super/sio"
	"github.com/superdb/super/sio/anyio"
	"github.com/superdb/super/sio/arrowio"
	"github.com/superdb/super/sio/supio"
	"github.com/superdb/super/vector"
	"github.com/superdb/super/vector/vio"
	"github.com/superdb/super/ztest"
)

func TestSPQ(t *testing.T) {
	dirs, err := findZTests()
	require.NoError(t, err)
	runZtests(t, dirs)
	t.Run("fuseinput", func(t *testing.T) {
		if os.Getenv("ZTEST_PATH") != "" {
			t.Skip("skipping on script run")
		}
		t.Setenv("SUPER_FUSEINPUT", "1")
		runZtests(t, dirs)
	})
	t.Run("boomerang", func(t *testing.T) {
		if testing.Short() {
			t.Skip("skipping boomerang in short mode")
		}
		t.Parallel()
		data, err := loadZTestInputsAndOutputs(t, dirs)
		require.NoError(t, err)
		// disabling arrow until we get nullable working without unions in unions
		//runAllBoomerangs(t, "arrows", data)
		runAllBoomerangs(t, "bsup -rows", data)
		runAllBoomerangs(t, "bsup", data)
		runAllBoomerangs(t, "parquet", data)
		runAllBoomerangs(t, "sup", data)
		runAllFusionBoomerangs(t, data)
	})
}

func findZTests() (map[string]struct{}, error) {
	dirs := map[string]struct{}{}
	pattern := fmt.Sprintf(`.*ztests\%c.*\.yaml$`, filepath.Separator)
	re := regexp.MustCompile(pattern)
	err := filepath.Walk(".", func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() && strings.HasSuffix(path, ".yaml") && re.MatchString(path) {
			dirs[filepath.Dir(path)] = struct{}{}
		}
		return nil
	})
	return dirs, err
}

func runZtests(t *testing.T, dirs map[string]struct{}) {
	for d := range dirs {
		t.Run(filepath.ToSlash(d), func(t *testing.T) {
			t.Parallel()
			ztest.Run(t, d)
		})
	}
}

func loadZTestInputsAndOutputs(t *testing.T, ztestDirs map[string]struct{}) (map[string]string, error) {
	out := map[string]string{}
	for dir := range ztestDirs {
		bundles, err := ztest.Load(dir)
		if err != nil {
			return nil, err
		}
		for _, b := range bundles {
			if b.Test == nil {
				continue
			}
			namePrefix := b.FileName + "/" + strconv.Itoa(b.Test.Line)
			name := namePrefix + "/input"
			if i := b.Test.Input; i != nil && isValid(t, name, *i) {
				out[name] = *i
			}
			name = namePrefix + "/output"
			if o := b.Test.Output; isValid(t, name, o) {
				out[name] = o
			}
			for _, i := range b.Test.Inputs {
				name = namePrefix + "/inputs/" + i.Name
				if i.Data != nil && isValid(t, name, *i.Data) {
					out[name] = *i.Data
				}
			}
			for _, o := range b.Test.Outputs {
				name = namePrefix + "/outputs/" + o.Name
				if o.Data != nil && isValid(t, name, *o.Data) {
					out[name] = *o.Data
				}
			}
		}
	}
	return out, nil
}

// isValid returns true if and only if s can be read fully without error by
// anyio and contains at least one value.
func isValid(t *testing.T, name, s string) bool {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("boomerang panic reading test input: %s: %+v\n%s\n", name, r, debug.Stack())
		}
	}()
	p, err := anyio.NewReader(t.Context(), super.NewContext(), strings.NewReader(s), anyio.ReaderOpts{})
	if err != nil {
		return false
	}
	defer p.Pull(true)
	r := sbuf.PullerReader(sbuf.NewMaterializer(p))
	var foundValue bool
	for {
		val, err := r.Read()
		if err != nil {
			return false
		}
		if val == nil {
			return foundValue
		}
		foundValue = true
	}
}

func runAllBoomerangs(t *testing.T, format string, data map[string]string) {
	t.Run(format, func(t *testing.T) {
		t.Parallel()
		for name, data := range data {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				runOneBoomerang(t, format, data)
			})
		}
	})
}

func runOneBoomerang(t *testing.T, format, data string) {
	// Create an auto-detecting puller for data.
	sctx := super.NewContext()
	dataPuller, err := anyio.NewReader(t.Context(), sctx, strings.NewReader(data), anyio.ReaderOpts{})
	require.NoError(t, err)
	defer dataPuller.Pull(true)

	if format == "parquet" {
		// Fuse data for formats that require uniform values.
		q, err := newQuery(t.Context(), sctx, "fuse", dataPuller)
		require.NoError(t, err)
		defer q.Pull(true)
		dataPuller = q
	}

	baseline, err := serialize(dataPuller, format)
	if err != nil {
		if errors.Is(err, arrowio.ErrMultipleTypes) ||
			errors.Is(err, arrowio.ErrNotRecord) ||
			errors.Is(err, arrowio.ErrUnsupportedType) {
			t.Skipf("skipping due to expected error: %s", err)
		}
		t.Fatalf("unexpected error writing %s baseline: %s", format, err)
	}
	readerFormat := format
	if readerFormat == "bsup -rows" {
		readerFormat = "bsup"
	}
	baselinePuller, err := anyio.NewReader(t.Context(), super.NewContext(), strings.NewReader(baseline), anyio.ReaderOpts{
		Format: readerFormat,
	})
	require.NoError(t, err)
	defer baselinePuller.Pull(true)

	boomerang, err := serialize(baselinePuller, format)
	require.NoError(t, err)

	require.Equal(t, baseline, boomerang, "baseline and boomerang differ")
}

func runAllFusionBoomerangs(t *testing.T, data map[string]string) {
	t.Run("fusion", func(t *testing.T) {
		t.Parallel()
		for name, data := range data {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				runOneFusionBoomerang(t, data)
			})
		}
	})
}

func runOneFusionBoomerang(t *testing.T, data string) {
	// Create an auto-detecting reader for data.
	dataSctx := super.NewContext()
	dataPuller, err := anyio.NewReader(t.Context(), dataSctx, strings.NewReader(data), anyio.ReaderOpts{})
	require.NoError(t, err)
	defer dataPuller.Pull(true)

	// Serialize non-fusion values from dataReader to baseline as SUP.
	r := &fusionRemovingReader{sbuf.PullerReader(sbuf.NewMaterializer(dataPuller)), hasFusion{}}
	puller := sbuf.NewDematerializer(dataSctx, sbuf.NewPuller(r))
	baseline, err := serialize(puller, "sup")
	require.NoError(t, err)
	if baseline == "" {
		t.Skip("skipping because data contains no non-fusion values")
	}

	boomerang, err := fuseDefuse(t.Context(), baseline)
	if assert.NoError(t, err) {
		assert.Equal(t, baseline, boomerang, "baseline and boomerang differ")
	}
}

func fuseDefuse(ctx context.Context, s string) (string, error) {
	sctx := super.NewContext()
	r := supio.NewReader(sctx, strings.NewReader(s))
	p := sbuf.NewDematerializer(sctx, sbuf.NewPuller(r))
	q, err := newQuery(ctx, sctx, "fuse | defuse(this)", p)
	if err != nil {
		return "", err
	}
	defer q.Pull(true)
	return serialize(q, "sup")
}

func newQuery(ctx context.Context, sctx *super.Context, spq string, p vio.Puller) (vio.Puller, error) {
	ast, err := parser.ParseText(spq)
	if err != nil {
		return nil, err
	}
	rctx := runtime.NewContext(ctx, sctx)
	q, err := compiler.NewCompiler(nil).NewQuery(rctx, ast, []vio.Puller{p}, 0)
	if err != nil {
		return nil, err
	}
	return &unlabeler{q}, nil
}

type unlabeler struct {
	vio.Puller
}

func (u *unlabeler) Pull(done bool) (vector.Any, error) {
	vec, err := u.Puller.Pull(done)
	vec, _ = vector.Unlabel(vec)
	return vec, err
}

func serialize(p vio.Puller, outputFormat string) (string, error) {
	var b strings.Builder
	opts := anyio.WriterOpts{Format: outputFormat}
	if outputFormat == "bsup -rows" {
		opts.Format = "bsup"
		opts.BSUP.Rows = true
	}
	w, err := anyio.NewWriter(sio.NopCloser(&b), opts)
	if err != nil {
		return "", err
	}
	err = vio.Copy(w, p)
	err2 := w.Close()
	return b.String(), errors.Join(err, err2)
}

type fusionRemovingReader struct {
	r sio.Reader
	h hasFusion
}

func (f *fusionRemovingReader) Read() (*super.Value, error) {
	for {
		val, err := f.r.Read()
		if val != nil && err == nil {
			if f.h.hasFusion(val.Type()) {
				continue
			}
		}
		return val, err
	}
}

type hasFusion map[super.Type]bool

func (h hasFusion) hasFusion(typ super.Type) bool {
	if has, ok := h[typ]; ok {
		return has
	}
	var has bool
	switch typ := typ.(type) {
	case *super.TypeRecord:
		has = slices.ContainsFunc(typ.Fields, func(f super.Field) bool { return h.hasFusion(f.Type) })
	case *super.TypeArray:
		has = h.hasFusion(typ.Type)
	case *super.TypeSet:
		has = h.hasFusion(typ.Type)
	case *super.TypeMap:
		has = h.hasFusion(typ.KeyType) || h.hasFusion(typ.ValType)
	case *super.TypeUnion:
		has = slices.ContainsFunc(typ.Types, h.hasFusion)
	case *super.TypeError:
		has = h.hasFusion(typ.Type)
	case *super.TypeFusion:
		has = true
	case *super.TypeNamed:
		// Store false to prevent infinite recursion.
		h[typ] = false
		has = h.hasFusion(typ.Type)
	}
	h[typ] = has
	return has
}

// If there's a problem with panics in the boomerangs, the Skip() can be commented
// out and this test run to have each input loaded in a test to see where the problem lies.
func TestLoad(t *testing.T) {
	t.Skip("this test used for debugging only")
	dirs, err := findZTests()
	require.NoError(t, err)
	for dir := range dirs {
		bundles, err := ztest.Load(dir)
		require.NoError(t, err)
		for _, b := range bundles {
			if b.Test == nil {
				continue
			}
			testName := b.FileName + "/" + strconv.Itoa(b.Test.Line)
			if i := b.Test.Input; i != nil {
				t.Run(testName+"/input", func(t *testing.T) {
					isValid(t, testName+"/input", *i)
				})
			}
			t.Run(testName+"/output", func(t *testing.T) {
				isValid(t, testName+"/output", b.Test.Output)
			})
			for _, i := range b.Test.Inputs {
				if i.Data != nil {
					t.Run(testName+"/inputs", func(t *testing.T) {
						isValid(t, testName+"/inputs", *i.Data)
					})
				}
			}
			for _, o := range b.Test.Outputs {
				if o.Data != nil {
					t.Run(testName+"/outputs", func(t *testing.T) {
						isValid(t, testName, *o.Data)
					})
				}
			}
		}
	}
}
