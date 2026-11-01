package loader

import (
	"fmt"
	"slices"

	"github.com/superdb/super"
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
	project(*FrameLoader, field.Projection) vector.Any
	strings(*collector)
}

// typesOnly is the projection for a value whose type is needed but not its data.
var typesOnly = field.Projection{{Types: true}}

// hasFusion returns true if typ has a fusion type that superdefuse would
// defuse (see the FusionChecker in runtime/sam/expr/function).
func hasFusion(typ super.Type) bool {
	switch typ := typ.(type) {
	case *super.TypeRecord:
		return slices.ContainsFunc(typ.Fields, func(f super.Field) bool { return hasFusion(f.Type) })
	case *super.TypeArray:
		return hasFusion(typ.Type)
	case *super.TypeSet:
		return hasFusion(typ.Type)
	case *super.TypeUnion:
		return slices.ContainsFunc(typ.Types, hasFusion)
	case *super.TypeMap:
		return hasFusion(typ.KeyType) || hasFusion(typ.ValType)
	case *super.TypeError:
		return hasFusion(typ.Type)
	case *super.TypeFusion:
		return true
	}
	return false
}

// newShadow decodes the BSUP metadata structure to the appropriate shadow object.
// No vector data data is actually loaded here.
func newShadow(bctx *bsup.Context, id bsup.ID) shadow {
	switch meta := bctx.Lookup(id).(type) {
	case *bsup.Dynamic:
		return newDynamic(meta)
	case *bsup.Error:
		return newError(bctx, meta)
	case *bsup.Named:
		return newNamed(meta, newShadow(bctx, meta.Values))
	case *bsup.Record:
		return newRecord(bctx, meta)
	case *bsup.Array:
		return newArray(bctx, meta)
	case *bsup.Set:
		return newSet(bctx, meta)
	case *bsup.Map:
		return newMap(bctx, meta)
	case *bsup.Union:
		return newUnion(bctx, meta)
	case *bsup.Enum:
		return newEnum(meta, newShadow(bctx, meta.Values))
	case *bsup.Fusion:
		return newFusion(bctx, meta)
	case *bsup.Option:
		return newOption(bctx, meta)
	case *bsup.Any:
		return newAny(bctx, meta)
	case *bsup.Dict:
		return newDict(bctx, meta)
	case *bsup.Int:
		return newInt(bctx, meta)
	case *bsup.Uint:
		return newUint(bctx, meta)
	case *bsup.Float:
		return newFloat(bctx, meta)
	case *bsup.Bool:
		return newBool(meta)
	case *bsup.Bytes:
		return newBytes(bctx, meta)
	case *bsup.IP:
		return newIP(meta)
	case *bsup.Net:
		return newNet(meta)
	case *bsup.Null:
		return newNull(meta)
	case *bsup.None:
		return newNone(meta)
	case *bsup.Missing:
		return newMissing(meta)
	case *bsup.TypeValue:
		return newTypeValue(bctx, meta)
	case *bsup.Const:
		return newConst(bctx, meta)
	case *bsup.Empty:
		return newEmpty(meta)
	default:
		panic(fmt.Sprintf("vector cache: type %T not supported", meta))
	}
}
