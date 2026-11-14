package loader

import (
	"slices"
	"sync"

	"github.com/superdb/super"
	"github.com/superdb/super/bsup"
	"github.com/superdb/super/pkg/field"
	"github.com/superdb/super/vector"
	"github.com/superdb/super/vector/vbuild"
)

// Union is a union vector, whose locators are its kind, count, the segment of
// its tags (run lengths if it has two member types), and the locators of the
// values of each member type.
type Union struct {
	Node
	Tags   bsup.Segment
	Values []Shadow
	mu     sync.Mutex
	// XXX we should store TagMap here so it doesn't have to be recomputed
	tags []uint32
}

func (u *Union) load(loader *FrameLoader) []uint32 {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.tags == nil {
		tags, err := bsup.ReadUint32s(u.Tags, loader.frame)
		if err != nil {
			panic(err)
		}
		u.tags = tags
	}
	return u.tags
}

func (u *Union) project(loader *FrameLoader, projection field.Projection) vector.Any {
	vecs := make([]vector.Any, 0, len(u.Values))
	types := make([]super.Type, 0, len(u.Values))
	for _, shadow := range u.Values {
		vec := project(loader, shadow, projection)
		vecs = append(vecs, vec)
		types = append(types, vec.Type())
	}
	tags := u.load(loader)
	if hasDuplicates(types) {
		// The projection made some member types the same (e.g., records
		// projected to their common fields), so merge their vectors.
		if projection.HasTypesAnywhere() {
			// Type-only vectors have no data to merge, so load the data
			// of the paths projected for their types.
			projection = projection.NoTypes()
			for i, shadow := range u.Values {
				vecs[i] = project(loader, shadow, projection)
			}
		}
		if len(u.Values) == 2 {
			tags, _ = vector.TagsFromRLE(tags, u.Length)
		}
		vec := vbuild.MergeSameTypesInDynamic(vector.NewDynamic(tags, vecs))
		if d, ok := vec.(*vector.Dynamic); ok {
			return vector.NewUnionFromDynamic(loader.sctx, d)
		}
		return vec
	}
	utyp := loader.sctx.MustLookupTypeUnion(types)
	// The tags are run-length encoded if the union as written has two
	// members (a projection may change the number of members of utyp).
	if len(u.Values) == 2 {
		return vector.NewUnionFromRLE(utyp, tags, vecs)
	}
	return vector.NewUnion(utyp, tags, vecs)
}

func hasDuplicates(types []super.Type) bool {
	for i, typ := range types {
		if slices.Contains(types[:i], typ) {
			return true
		}
	}
	return false
}
