package loader_test

import (
	"encoding/binary"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/superdb/super"
	"github.com/superdb/super/bsup"
	"github.com/superdb/super/bsup/loader"
)

// locators returns a locator section of one root vector of type id whose
// locators are encoded by fn.
func locators(id uint32, fn func(*bsup.LocatorWriter)) []byte {
	var w bsup.LocatorWriter
	w.WriteCount(1)
	w.WriteUvarint(uint64(id))
	fn(&w)
	return w.Bytes()
}

func unmarshal(t *testing.T, b []byte, dataSize uint64) (loader.Shadow, error) {
	defs, ok := super.NewTypeDefsFromBytes(nil)
	require.True(t, ok)
	return loader.Unmarshal(defs, b, dataSize)
}

func TestUnmarshalValid(t *testing.T) {
	b := locators(super.IDInt64, func(w *bsup.LocatorWriter) {
		w.WriteKind(bsup.KindPlain)
		w.WriteCount(2)
		w.Segment(bsup.Segment{Offset: w.Offset(), Length: 16, MemLength: 16})
	})
	shadow, err := unmarshal(t, b, 16)
	require.NoError(t, err)
	assert.Equal(t, &loader.Node{TypeID: super.IDInt64, Length: 2}, &shadow.(*loader.Int).Node)
	// A const of a primitive type that has no plain encoding is valid.
	b = locators(super.IDDate, func(w *bsup.LocatorWriter) {
		w.WriteKind(bsup.KindConst)
		w.WriteCount(2)
		w.Value(nil)
	})
	shadow, err = unmarshal(t, b, 0)
	require.NoError(t, err)
	assert.Equal(t, uint32(2), shadow.Len())
}

func TestUnmarshalMalformed(t *testing.T) {
	cases := []struct {
		name     string
		id       uint32
		fn       func(*bsup.LocatorWriter)
		dataSize uint64
		err      string
	}{
		{
			name: "plain vector of bad type ID",
			id:   super.IDTypeComplex + 7,
			fn: func(w *bsup.LocatorWriter) {
				w.WriteKind(bsup.KindPlain)
				w.WriteCount(1)
			},
			err: "bad type ID",
		},
		{
			name: "none outside an option",
			id:   super.IDInt64,
			fn: func(w *bsup.LocatorWriter) {
				w.WriteKind(bsup.KindNone)
				w.WriteCount(1)
			},
			err: "kind 3 not allowed",
		},
		{
			name: "const values of a dict",
			id:   super.IDInt64,
			fn: func(w *bsup.LocatorWriter) {
				w.WriteKind(bsup.KindDict)
				w.WriteKind(bsup.KindConst)
				w.WriteCount(1)
				w.Value([]byte{2})
			},
			err: "kind 1 not allowed",
		},
		{
			name: "segment past the data section",
			id:   super.IDInt64,
			fn: func(w *bsup.LocatorWriter) {
				w.WriteKind(bsup.KindPlain)
				w.WriteCount(1 << 20)
				w.Segment(bsup.Segment{Offset: w.Offset(), Length: 8 << 20, MemLength: 8 << 20})
			},
			dataSize: 16,
			err:      "segment at offset 0 of length 8388608 is past the end of the data section",
		},
		{
			name: "segments short of the data section",
			id:   super.IDInt64,
			fn: func(w *bsup.LocatorWriter) {
				w.WriteKind(bsup.KindPlain)
				w.WriteCount(1)
				w.Segment(bsup.Segment{Offset: w.Offset(), Length: 8, MemLength: 8})
			},
			dataSize: 16,
			err:      "segments don't fill the data section",
		},
		{
			name: "extra bytes",
			id:   super.IDNull,
			fn: func(w *bsup.LocatorWriter) {
				w.WriteKind(bsup.KindPlain)
				w.WriteCount(1)
				w.WriteCount(1)
			},
			err: "extra bytes",
		},
		{
			name: "segment with unknown compression format",
			id:   super.IDInt64,
			fn: func(w *bsup.LocatorWriter) {
				w.WriteKind(bsup.KindPlain)
				w.WriteCount(2)
				w.Segment(bsup.Segment{Offset: w.Offset(), Length: 16, MemLength: 16, CompressionFormat: 9})
			},
			dataSize: 16,
			err:      "unknown compression format 9",
		},
		{
			name: "LZ4 segment with huge uncompressed length",
			id:   super.IDInt64,
			fn: func(w *bsup.LocatorWriter) {
				w.WriteKind(bsup.KindPlain)
				w.WriteCount(2)
				w.Segment(bsup.Segment{Offset: w.Offset(), Length: 16, MemLength: 1 << 40, CompressionFormat: bsup.CompressionFormatLZ4})
			},
			dataSize: 16,
			err:      "malformed BSUP locator section",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := unmarshal(t, locators(c.id, c.fn), c.dataSize)
			assert.ErrorContains(t, err, c.err)
		})
	}
}

func TestUnmarshalMalformedRoots(t *testing.T) {
	cases := map[string]func(*bsup.LocatorWriter){
		"no root vectors": func(w *bsup.LocatorWriter) {
			w.WriteCount(0)
		},
		"missing dynamic tags": func(w *bsup.LocatorWriter) {
			w.WriteCount(2)
			w.WriteUvarint(super.IDInt64)
			w.WriteUvarint(super.IDString)
		},
		"missing root vector": func(w *bsup.LocatorWriter) {
			w.WriteCount(2)
			w.WriteUvarint(super.IDNull)
			w.WriteUvarint(super.IDNull)
			w.Segment(bsup.Segment{})
			w.WriteKind(bsup.KindPlain)
			w.WriteCount(1)
		},
	}
	for name, fn := range cases {
		t.Run(name, func(t *testing.T) {
			var w bsup.LocatorWriter
			fn(&w)
			_, err := unmarshal(t, w.Bytes(), 0)
			assert.ErrorContains(t, err, "malformed BSUP locator")
		})
	}
}

func TestUnmarshalMalformedTypeDefs(t *testing.T) {
	// An array type whose element type doesn't precede it (here, the
	// array itself) is malformed.
	typedef := binary.AppendUvarint([]byte{super.TypeDefArray}, super.IDTypeComplex)
	// The typedefs table: one typedef at offset 0.
	table := append([]byte{1, 0}, typedef...)
	defs, ok := super.NewTypeDefsFromBytes(table)
	require.True(t, ok)
	locs := locators(super.IDTypeComplex, func(w *bsup.LocatorWriter) {
		w.WriteKind(bsup.KindPlain)
		w.WriteCount(1)
		w.Segment(bsup.Segment{})
	})
	_, err := loader.Unmarshal(defs, locs, 0)
	assert.ErrorContains(t, err, "bad typedef")
}

func TestUnmarshalMaxDepth(t *testing.T) {
	// Arrays nested more deeply than bsup.MaxLocatorDepth are malformed.
	sctx := super.NewContext()
	typ := super.Type(super.TypeInt64)
	for range bsup.MaxLocatorDepth {
		typ = sctx.LookupTypeArray(typ)
	}
	enc := super.NewTypeDefsEncoder()
	id := enc.LookupType(typ)
	defs, ok := super.NewTypeDefsFromBytes(enc.Bytes())
	require.True(t, ok)
	locs := locators(id, func(w *bsup.LocatorWriter) {
		for range bsup.MaxLocatorDepth {
			w.WriteKind(bsup.KindPlain)
			w.WriteCount(1)
			w.Segment(bsup.Segment{})
		}
		w.WriteKind(bsup.KindConst)
		w.WriteCount(1)
		w.Value([]byte{2})
	})
	_, err := loader.Unmarshal(defs, locs, 0)
	assert.ErrorContains(t, err, "vectors nested too deeply")
}
