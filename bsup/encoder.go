package bsup

import (
	"fmt"
	"io"
	"math"
	"net/netip"

	"github.com/superdb/super"
	"github.com/superdb/super/scode"
	"github.com/superdb/super/vector"
	"golang.org/x/sync/errgroup"
)

type Encoder interface {
	// Encode encodes all in-memory vector data into its storage-ready serialized format.
	// Vectors may be encoded concurrently and errgroup.Group is used to sync
	// and return errors.
	Encode(*errgroup.Group)
	// Metadata returns the data structure conforming to the BSUP specification
	// describing the layout of vectors.  This is called after all data is
	// written and encoded by the Encode with the result marshaled to build
	// the header section of the BSUP object.  An offset is passed down into
	// the traversal representing where in the data section the vector data
	// will land.  This is called in a sequential fashion (no parallelism) so
	// that the metadata can be computed and the BSUP header written before the
	// vector data is written via Emit.
	Metadata(*Context, uint64) (uint64, ID)
	Emit(w io.Writer) error
}

func NewEncoder(bctx *Context, vec vector.Any) Encoder {
	switch vec := vec.(type) {
	case *vector.Record:
		return NewRecordEncoder(bctx, vec)
	case *vector.Array:
		return NewArrayEncoder(bctx, vec)
	case *vector.Set:
		return NewSetEncoder(bctx, vec)
	case *vector.Map:
		return NewMapEncoder(bctx, vec)
	case *vector.Union:
		return NewUnionEncoder(bctx, vec)
	case *vector.Enum:
		return NewEnumEncoder(vec)
	case *vector.Error:
		return &ErrorEncoder{NewEncoder(bctx, vec.Vals)}
	case *vector.Named:
		return NewNamedEncoder(bctx, vec)
	case *vector.Fusion:
		if super.IsTypeAny(vec.Type()) {
			return NewAnyEncoder(bctx, vec)
		}
		return NewFusionEncoder(bctx, vec)
	case *vector.Option:
		return NewOptionEncoder(bctx, vec)
	default:
		return NewPrimitiveEncoder(bctx, vec, true)
	}
}

func NewPrimitiveEncoder(bctx *Context, vec vector.Any, root bool) Encoder {
	switch vec := vec.(type) {
	case *vector.Dict:
		return &DictEncoder{
			counts: NewUint32Encoder(vec.Counts),
			values: NewPrimitiveEncoder(bctx, vec.Any, false),
			index:  vec.Index,
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
			return NewPrimitiveEncoder(bctx, out, false)
		}
		return &UintEncoder{typ: vec.Typ, vals: vec.Values}
	case *vector.Int:
		if root {
			out := maybeConvertToDictOrConst(vec.Values, func(vals []int64) vector.Any {
				return vector.NewInt(vec.Typ, vals)
			})
			return NewPrimitiveEncoder(bctx, out, false)
		}
		return &IntEncoder{typ: vec.Typ, vals: vec.Values}
	case *vector.Float:
		if root {
			out := maybeConvertToDictOrConst(vec.Values, func(vals []float64) vector.Any {
				return vector.NewFloat(vec.Typ, vals)
			})
			return NewPrimitiveEncoder(bctx, out, false)
		}
		return NewFloatEncoder(vec.Typ, vec.Values)
	case *vector.Bool:
		// XXX can convert all trues and all falses to consts.
		return NewBoolEncoder(vec)
	case *vector.String:
		if root {
			out := maybeStringBytesDict(super.TypeString, vec.Table())
			return NewPrimitiveEncoder(bctx, out, false)
		}
		return NewBytesEncoder(super.TypeString, vec.Table())
	case *vector.Bytes:
		if root {
			out := maybeStringBytesDict(super.TypeBytes, vec.Table())
			return NewPrimitiveEncoder(bctx, out, false)
		}
		return NewBytesEncoder(super.TypeBytes, vec.Table())
	case *vector.IP:
		if root {
			out := maybeConvertToDictOrConst(vec.Values, func(vals []netip.Addr) vector.Any {
				return vector.NewIP(vals)
			})
			return NewPrimitiveEncoder(bctx, out, false)
		}
		return NewIPEncoder(vec.Values)
	case *vector.Net:
		if root {
			out := maybeConvertToDictOrConst(vec.Values, func(vals []netip.Prefix) vector.Any {
				return vector.NewNet(vals)
			})
			return NewPrimitiveEncoder(bctx, out, false)
		}
		return NewNetEncoder(vec.Values)
	case *vector.TypeValue:
		if root {
			out := maybeConvertToDictOrConst(vec.Types(), func(vals []super.Type) vector.Any {
				return vector.NewTypeValue(vals)
			})
			return NewPrimitiveEncoder(bctx, out, false)
		}
		return NewTypeValueEncoder(bctx, vec)
	case *vector.Null:
		return NewNullEncoder(vec.Len())
	case *vector.Missing:
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

func (e *ErrorEncoder) Metadata(bctx *Context, off uint64) (uint64, ID) {
	off, id := e.Encoder.Metadata(bctx, off)
	return off, bctx.enter(&Error{id})
}
