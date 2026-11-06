package bsup

import (
	"fmt"
	"io"
	"math"
	"net/netip"

	"github.com/superdb/super"
	"github.com/superdb/super/pkg/field"
	"github.com/superdb/super/scode"
	"github.com/superdb/super/vector"
	"golang.org/x/sync/errgroup"
)

type Encoder interface {
	// Encode encodes all in-memory vector data into its storage-ready serialized format.
	// Vectors may be encoded concurrently and errgroup.Group is used to sync
	// and return errors.
	Encode(*errgroup.Group)
	// Locate encodes the locators of the vector in the frame's locator
	// section (see LocatorWriter) in DFS order by traversing the vector's
	// type.  This is called after the vector is encoded by Encode and before
	// the vector data is written by Emit, which must write the segments in
	// the order they are located.  It is called sequentially (no
	// parallelism).
	Locate(*LocatorWriter)
	Stats(field.Path, CollectStatsFunc)
	Emit(w io.Writer) error
}

func NewEncoder(typedefs *TypeDefs, vec vector.Any) Encoder {
	switch vec := vec.(type) {
	case *vector.Record:
		return NewRecordEncoder(typedefs, vec)
	case *vector.Array:
		return NewArrayEncoder(typedefs, vec)
	case *vector.Set:
		return NewSetEncoder(typedefs, vec)
	case *vector.Map:
		return NewMapEncoder(typedefs, vec)
	case *vector.Union:
		return NewUnionEncoder(typedefs, vec)
	case *vector.Enum:
		return NewEnumEncoder(vec)
	case *vector.Error:
		return &ErrorEncoder{NewEncoder(typedefs, vec.Vals)}
	case *vector.Named:
		return NewNamedEncoder(typedefs, vec)
	case *vector.Fusion:
		if super.IsFusionAll(vec.Type()) {
			return NewAnyEncoder(typedefs, vec)
		}
		return NewFusionEncoder(typedefs, vec)
	case *vector.Option:
		return NewOptionEncoder(typedefs, vec)
	default:
		return NewPrimitiveEncoder(typedefs, vec, true)
	}
}

func NewPrimitiveEncoder(typedefs *TypeDefs, vec vector.Any, root bool) Encoder {
	switch vec := vec.(type) {
	case *vector.Dict:
		return &DictEncoder{
			counts: NewUint32Encoder(vec.Counts),
			values: NewPrimitiveEncoder(typedefs, vec.Any, false),
			index:  vec.Index,
		}
	case *vector.Empty:
		// An empty vector is encoded as a zero-length const so that
		// its type is preserved.
		return &ConstEncoder{
			val: super.NewValue(vec.Type(), nil),
			len: 0,
		}
	case *vector.Const:
		return &ConstEncoder{
			val: vector.ValueAt(new(scode.Builder), vec.Any, 0),
			len: vec.Len(),
		}
	case *vector.Uint:
		if root {
			// XXX This is a potential computationally intensive operation and
			// should probable be move to the Encode pass where it can be
			// parallelized.
			out := maybeConvertToDictOrConst(vec.Values, func(vals []uint64) vector.Any {
				return vector.NewUint(vec.Typ, vals)
			})
			return NewPrimitiveEncoder(typedefs, out, false)
		}
		return &UintEncoder{typ: vec.Typ, vals: vec.Values}
	case *vector.Int:
		if root {
			out := maybeConvertToDictOrConst(vec.Values, func(vals []int64) vector.Any {
				return vector.NewInt(vec.Typ, vals)
			})
			return NewPrimitiveEncoder(typedefs, out, false)
		}
		return &IntEncoder{typ: vec.Typ, vals: vec.Values}
	case *vector.Float:
		if root {
			out := maybeConvertToDictOrConst(vec.Values, func(vals []float64) vector.Any {
				return vector.NewFloat(vec.Typ, vals)
			})
			return NewPrimitiveEncoder(typedefs, out, false)
		}
		return NewFloatEncoder(vec.Typ, vec.Values)
	case *vector.Bool:
		// XXX can convert all trues and all falses to consts.
		return NewBoolEncoder(vec)
	case *vector.String:
		if root {
			out := maybeStringBytesDict(super.TypeString, vec.Table())
			return NewPrimitiveEncoder(typedefs, out, false)
		}
		return NewBytesEncoder(super.TypeString, vec.Table())
	case *vector.Bytes:
		if root {
			out := maybeStringBytesDict(super.TypeBytes, vec.Table())
			return NewPrimitiveEncoder(typedefs, out, false)
		}
		return NewBytesEncoder(super.TypeBytes, vec.Table())
	case *vector.UUID:
		// UUIDs are encoded as a bytes table of 16-byte values.
		bytes := make([]byte, 0, 16*len(vec.Values))
		offsets := make([]uint32, 0, len(vec.Values)+1)
		offsets = append(offsets, 0)
		for _, u := range vec.Values {
			bytes = append(bytes, u[:]...)
			offsets = append(offsets, uint32(len(bytes)))
		}
		return NewBytesEncoder(super.TypeUUID, vector.NewBytesTable(offsets, bytes))
	case *vector.IP:
		if root {
			out := maybeConvertToDictOrConst(vec.Values, func(vals []netip.Addr) vector.Any {
				return vector.NewIP(vals)
			})
			return NewPrimitiveEncoder(typedefs, out, false)
		}
		return NewIPEncoder(vec.Values)
	case *vector.Net:
		if root {
			out := maybeConvertToDictOrConst(vec.Values, func(vals []netip.Prefix) vector.Any {
				return vector.NewNet(vals)
			})
			return NewPrimitiveEncoder(typedefs, out, false)
		}
		return NewNetEncoder(vec.Values)
	case *vector.TypeValue:
		if root {
			// Use the IDs in vec's table so foreign type values aren't
			// mapped into the query context.
			ids := vec.RawIDs()
			out := maybeConvertToDictOrConst(ids, func(vals []uint32) vector.Any {
				return vec.WithTypeIDs(vals)
			})
			return NewPrimitiveEncoder(typedefs, out, false)
		}
		return NewTypeValueEncoder(typedefs, vec)
	case *vector.Null:
		return NewNullEncoder(vec.Len())
	case *vector.Missing:
		return NewMissingEncoder(vec.Len())
	case *vector.None:
		return NewNoneEncoder(vec.Len())
	default:
		panic(fmt.Sprintf("unsupported type in BSUP file: %T", vec))
	}
}

func maybeConvertToDictOrConst[E comparable](in []E, fn func([]E) vector.Any) vector.Any {
	vals, index, counts := comparableDict(in)
	if vals == nil || !isValidDict(len(in), len(vals)) {
		return fn(in)
	}
	flat := fn(vals)
	if len(vals) == 1 {
		return vector.NewConst(flat, counts[0])
	}
	return vector.NewDict(flat, index, counts)
}

func isValidDict(len, card int) bool {
	return card >= 1 && card < len
}

func comparableDict[T comparable](in []T) ([]T, []byte, []uint32) {
	m := make(map[T]byte)
	var counts []uint32
	index := make([]byte, len(in))
	var vals []T
	for k, v := range in {
		tag, ok := m[v]
		if !ok {
			if len(counts) > math.MaxUint8 {
				return nil, nil, nil
			}
			tag = byte(len(counts))
			m[v] = tag
			counts = append(counts, 0)
			vals = append(vals, v)
		}
		index[k] = tag
		counts[tag]++
	}
	return vals, index, counts
}

type ErrorEncoder struct {
	Encoder
}

func (e *ErrorEncoder) Locate(w *LocatorWriter) {
	w.WriteKind(KindPlain)
	w.Locate(e.Encoder)
}
