package loader

import (
	"fmt"

	"github.com/superdb/super/bsup"
	"github.com/superdb/super/pkg/field"
	"github.com/superdb/super/vector"
)

// The shadow type mirrors the vector.Any implementations here with locks and
// pointers to shared vector slices.  This lets us page in just the portions
// of vector data that is needed at any given time (which we cache inside the shadow).
// When we need a runtime vector, we build the immutable vector.Any components from
// mutable shadow pieces that are dynamically loaded and maintained here.
//
// Shadows are created incrementally so that a sequence of projections will do the
// minimal work unmarshaling the BSUP metadata as needed.  When processing a sequence
// of BSUP files with a single projection, the incremental capability is not important
// but when caching BSUP objects (e.g., in local from S3), multiple threads operating
// concurrently on a single object benefit from incremental unmarshaling.  This is especially
// important when processing thin projections over objects with lots of heteregenous types.
//
// Note that the shadow doesn't know about the query type context, thereby allowing the shadow
// to be shared across different queries.  Instead, the loader that builds a vector.Any
// is reponsible for computing the shared type from the shadow hierarchy.
//
// Shadows are created with unmarshal and only the portion of the shadow tree is
// created for the passed-in projection.  Any shadow object may be updated
// incrementally and concurrently by calling the unmarshal method to unfurl additional
// legs of a value (e.g., via subsequent calls with different projections).
// The project method creates a vector.Any from the umarshaled shadow, which
// should cover the passed in projection.
//
// The shadow locking also supports incremental expansion and loading from the
// vector runtime via the *Loader implementations of the various vector loader
// interfaces.
type shadow interface {
	length() uint32
	unmarshal(*bsup.Context, field.Projection)
	project(*loader, field.Projection) vector.Any
}

// newShadow decodes the BSUP metadata structure to the appropriate shadow object.
// No vector data data is actually loaded here.
func newShadow(cctx *bsup.Context, id bsup.ID) shadow {
	switch meta := cctx.Lookup(id).(type) {
	case *bsup.Dynamic:
		return newDynamic(meta)
	case *bsup.Error:
		return newError(cctx, meta)
	case *bsup.Named:
		return newNamed(meta, newShadow(cctx, meta.Values))
	case *bsup.Record:
		return newRecord(cctx, meta)
	case *bsup.Array:
		return newArray(cctx, meta)
	case *bsup.Set:
		return newSet(cctx, meta)
	case *bsup.Map:
		return newMap(cctx, meta)
	case *bsup.Union:
		return newUnion(cctx, meta)
	case *bsup.Enum:
		return newEnum(meta, newShadow(cctx, meta.Values))
	case *bsup.Fusion:
		return newFusion(cctx, meta)
	case *bsup.Option:
		return newOption(cctx, meta)
	case *bsup.Any:
		return newAny(cctx, meta)
	case *bsup.Dict:
		return newDict(cctx, meta)
	case *bsup.Int:
		return newInt(cctx, meta)
	case *bsup.Uint:
		return newUint(cctx, meta)
	case *bsup.Float:
		return newFloat(cctx, meta)
	case *bsup.Bool:
		return newBool(meta)
	case *bsup.Bytes:
		return newBytes(cctx, meta)
	case *bsup.IP:
		return newIP(meta)
	case *bsup.Net:
		return newNet(meta)
	case *bsup.Null:
		return newNull(meta)
	case *bsup.None:
		return newNone(meta)
	case *bsup.TypeValue:
		return newTypeValue(cctx, meta)
	case *bsup.Const:
		return newConst(cctx, meta)
	case *bsup.Empty:
		return newEmpty(meta)
	default:
		panic(fmt.Sprintf("vector cache: type %T not supported", meta))
	}
}
