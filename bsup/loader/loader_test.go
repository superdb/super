package loader_test

import (
	"bytes"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/superdb/super"
	"github.com/superdb/super/bsup"
	"github.com/superdb/super/bsup/loader"
	"github.com/superdb/super/pkg/field"
	"github.com/superdb/super/scode"
	"github.com/superdb/super/sio"
	"github.com/superdb/super/sio/supio"
	"github.com/superdb/super/sup"
	"github.com/superdb/super/vector"
)

// The values cover each vector encoding: plain, dict, and const vectors of
// primitive types, nested records, arrays, sets, maps, unions, enums, errors,
// named types, and options (with all none values and with some).
const input = `
type port=uint16
{a:1,s:"x",k:"c",r:{x:1.5,y:true,z:{w:80::port}},arr:[1,2],st:set["a"],m:map{"a":1},u:1::(int64|string),e:"e1"::enum(e1,e2),err:error("bad"),o?:1,n?:none::int64,ip:10.0.0.1,net:10.0.0.0/8}
{a:2,s:"y",k:"c",r:{x:2.5,y:false,z:{w:443::port}},arr:[3],st:set["b","c"],m:map{"b":2},u:"two"::(int64|string),e:"e2"::enum(e1,e2),err:error("worse"),o?:none::int64,n?:none::int64,ip:10.0.0.2,net:192.168.0.0/16}
{a:3,s:"x",k:"c",r:{x:3.5,y:true,z:{w:80::port}},arr:[],st:set[],m:map{},u:3::(int64|string),e:"e1"::enum(e1,e2),err:error("bad"),o?:3,n?:none::int64,ip:10.0.0.3,net:10.0.0.0/8}
{a:4,s:"x",k:"c",r:{x:4.5,y:false,z:{w:80::port}},arr:[4,5,6],st:set["a"],m:map{"c":3},u:"four"::(int64|string),e:"e2"::enum(e1,e2),err:error("bad"),o?:none::int64,n?:none::int64,ip:10.0.0.4,net:10.0.0.0/8}
`

func newFrameLoader(t *testing.T, sctx *super.Context, s string) *loader.FrameLoader {
	var buf bytes.Buffer
	w := bsup.NewColumnWriter(sio.NopCloser(&buf))
	r := supio.NewReader(sctx, strings.NewReader(s))
	b := vector.NewDynamicValueBuilder()
	for {
		val, err := r.Read()
		require.NoError(t, err)
		if val == nil {
			break
		}
		b.Write(*val)
	}
	require.NoError(t, w.Push(b.Build(sctx)))
	require.NoError(t, w.Close())
	frame, err := bsup.NewSeekable(sctx, bytes.NewReader(buf.Bytes())).Next()
	require.NoError(t, err)
	l, err := loader.NewFrameLoader(sctx, frame.(*bsup.ColFrame))
	require.NoError(t, err)
	return l
}

func load(l *loader.FrameLoader, projection field.Projection) string {
	vec := l.Load(projection)
	var sb strings.Builder
	var b scode.Builder
	for slot := range vec.Len() {
		sb.WriteString(sup.String(vector.ValueAt(&b, vec, slot)))
		sb.WriteByte('\n')
	}
	return sb.String()
}

func projection(paths ...string) field.Projection {
	var out []field.Path
	for _, p := range paths {
		out = append(out, field.Dotted(p))
	}
	return field.NewProjection(out)
}

var projections = []field.Projection{
	projection("a"),
	projection("s", "r.z.w"),
	projection("r.x", "o", "u"),
	projection("k", "e", "err"),
	projection("arr", "st", "m", "n", "ip", "net"),
	projection("nosuchfield", "r.nosuchfield"),
	nil,
}

func TestLoaderProjections(t *testing.T) {
	sctx := super.NewContext()
	// Each projection from a fresh loader.
	var want []string
	for _, p := range projections {
		want = append(want, load(newFrameLoader(t, sctx, input), p))
	}
	// Loading everything gives back the input.
	var in strings.Builder
	r := supio.NewReader(sctx, strings.NewReader(input))
	for {
		val, err := r.Read()
		require.NoError(t, err)
		if val == nil {
			break
		}
		in.WriteString(sup.String(*val))
		in.WriteByte('\n')
	}
	assert.Equal(t, in.String(), want[len(want)-1])
	// A sequence of projections on one loader, which loads more data into
	// the same shadows, gives the same results, in either order.
	l := newFrameLoader(t, sctx, input)
	for k, p := range projections {
		assert.Equal(t, want[k], load(l, p), "projection %d", k)
	}
	l = newFrameLoader(t, sctx, input)
	for k := len(projections) - 1; k >= 0; k-- {
		assert.Equal(t, want[k], load(l, projections[k]), "projection %d", k)
	}
	// Concurrent projections on one loader.
	l = newFrameLoader(t, sctx, input)
	var wg sync.WaitGroup
	for range 4 {
		for k, p := range projections {
			wg.Go(func() {
				assert.Equal(t, want[k], load(l, p), "projection %d", k)
			})
		}
	}
	wg.Wait()
}

func TestLoaderDynamic(t *testing.T) {
	// Values of different types make a dynamic root, whose values are
	// loaded together or separately.
	const s = "1\n\"a\"\n{x:1}\n2\n{x:2}\n"
	sctx := super.NewContext()
	l := newFrameLoader(t, sctx, s)
	assert.Equal(t, uint32(5), l.Len())
	assert.Equal(t, s, load(l, nil))
	var lens []uint32
	var out []string
	for ul := range l.UnorderedLoaders() {
		lens = append(lens, ul.Len())
		vec := ul.Load(nil)
		var b scode.Builder
		for slot := range vec.Len() {
			out = append(out, sup.String(vector.ValueAt(&b, vec, slot)))
		}
	}
	assert.Equal(t, []uint32{2, 1, 2}, lens)
	assert.ElementsMatch(t, []string{"1", "2", `"a"`, "{x:1}", "{x:2}"}, out)
}

func TestLocatorsMalformed(t *testing.T) {
	// Malformed locators aren't detected when the frame is read but when
	// its loader decodes them.
	sctx := super.NewContext()
	var buf bytes.Buffer
	w := bsup.NewColumnWriter(sio.NopCloser(&buf))
	val, err := sup.ParseValue(sctx, `{a:1,b:"x"}`)
	require.NoError(t, err)
	b := vector.NewDynamicValueBuilder()
	b.Write(val)
	require.NoError(t, w.Push(b.Build(sctx)))
	require.NoError(t, w.Close())
	file := buf.Bytes()
	raw, err := bsup.NewRawReader(bytes.NewReader(file)).Next()
	require.NoError(t, err)
	header := raw.(*bsup.RawColFrame).Header()
	off := bsup.ColumnHeaderSize + int(header.VectorTypedefsSize)
	locators := file[off : off+int(header.LocatorsSize)]
	// Corrupt the kind of the root vector.
	locators[2] = 0x7f
	frame, err := bsup.NewSeekable(sctx, bytes.NewReader(file)).Next()
	require.NoError(t, err)
	_, err = loader.NewFrameLoader(sctx, frame.(*bsup.ColFrame))
	assert.EqualError(t, err, "malformed BSUP locators: BSUP loader: unknown vector kind 127")
}

func TestLoaderLen(t *testing.T) {
	// Len is valid before and after a load, for one root vector or a
	// dynamic.
	sctx := super.NewContext()
	for s, n := range map[string]uint32{input: 4, "1\n\"a\"\n{x:1}\n2\n{x:2}\n": 5} {
		l := newFrameLoader(t, sctx, s)
		assert.Equal(t, n, l.Len())
		load(l, projection("a"))
		assert.Equal(t, n, l.Len())
		l = newFrameLoader(t, sctx, s)
		load(l, nil)
		assert.Equal(t, n, l.Len())
	}
}

func TestLoaderRoundtrip(t *testing.T) {
	// Each input, written to a frame and loaded in its entirety, gives back
	// the input.
	cases := map[string]string{
		"fusion": `
{f:fusion(1::(int64|string),<int64>)}
{f:fusion("a"::(int64|string),<string>)}
{f:fusion(2::(int64|string),<int64>)}
`,
		"fusion(all)": `
{x:1::any}
{x:"a"::any}
{x:{y:1}::any}
`,
		"primitives": `
{u:8f6a5f8b-1d3a-4b7e-9e3b-2a1c5d6e7f80,h:1.5::float16,f:2.5::float32,d:1s,t:2026-01-01T00:00:00Z,ty:<int64>,b:0x0102,n:null}
{u:9f6a5f8b-1d3a-4b7e-9e3b-2a1c5d6e7f80,h:2.5::float16,f:3.5::float32,d:2m,t:2026-01-02T00:00:00Z,ty:<string>,b:0x03,n:null}
{u:af6a5f8b-1d3a-4b7e-9e3b-2a1c5d6e7f80,h:3.5::float16,f:4.5::float32,d:3h,t:2026-01-03T00:00:00Z,ty:<{a:int64}>,b:0x,n:null}
`,
		"dict type values": `
{ty:<int64>}
{ty:<string>}
{ty:<int64>}
{ty:<int64>}
`,
		"recursive named": `
type foo={l:foo|null}
{l:{l:null}::foo}::foo
{l:null}::foo
{l:{l:{l:null}::foo}::foo}::foo
`,
		"repeated complex values": `
{r:{a:1,b:"x"},s:[1,2],m:map{"a":[1]}}
{r:{a:1,b:"x"},s:[1,2],m:map{"a":[1]}}
{r:{a:1,b:"x"},s:[1,2],m:map{"a":[1]}}
`,
		"empty records": `
{e:{},a:1}
{e:{},a:2}
`,
		"none under named option": `
type o=int64
{x?:none::o,y:1}
{x?:none::o,y:2}
`,
		"named option": `
type o=int64
{x?:1::o,y:1}
{x?:none::o,y:2}
`,
		"empty and const in a dynamic": `
{a:1}
"x"
{a:1}
[1,2]
`,
	}
	for name, input := range cases {
		t.Run(name, func(t *testing.T) {
			sctx := super.NewContext()
			var want strings.Builder
			r := supio.NewReader(sctx, strings.NewReader(input))
			for {
				val, err := r.Read()
				require.NoError(t, err)
				if val == nil {
					break
				}
				want.WriteString(sup.String(*val))
				want.WriteByte('\n')
			}
			assert.Equal(t, want.String(), load(newFrameLoader(t, sctx, input), nil))
		})
	}
}
