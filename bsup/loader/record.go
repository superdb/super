package loader

import (
	"slices"

	"github.com/superdb/super"
	"github.com/superdb/super/pkg/field"
	"github.com/superdb/super/vector"
)

// Record is a record vector, whose locators are its kind and those of each
// of its fields (or its count if it has no fields).
type Record struct {
	Node
	Names  []string
	Fields []Shadow
}

func (r *Record) project(loader *FrameLoader, projection field.Projection) vector.Any {
	valFields := make([]vector.Any, 0, len(r.Fields))
	types := make([]super.Field, 0, len(r.Fields))
	if len(projection) == 0 {
		// Build the whole record.  We're either loading all on demand (nil paths)
		// or loading this record because it's referenced at the end of a projected path.
		for k := range r.Fields {
			val := r.Fields[k].project(loader, nil)
			valFields = append(valFields, val)
			types = append(types, super.NewField(r.Names[k], val.Type()))
		}
		return vector.NewRecord(loader.sctx.MustLookupTypeRecord(types), valFields, r.Length)
	}
	if projection.HasTypes() {
		// Build the whole record with any field not in the projection
		// projected for its type only.
		for k, name := range r.Names {
			proj, ok := projection.Lookup(name)
			if !ok {
				proj = typesOnly
			}
			val := project(loader, r.Fields[k], proj)
			valFields = append(valFields, val)
			types = append(types, super.NewField(name, val.Type()))
		}
		return vector.NewRecord(loader.sctx.MustLookupTypeRecord(types), valFields, r.Length)
	}
	for _, node := range projection {
		var val vector.Any
		if k := slices.Index(r.Names, node.Name); k >= 0 {
			val = project(loader, r.Fields[k], node.Proj)
		} else {
			val = vector.NewMissing(r.Length)
		}
		valFields = append(valFields, val)
		types = append(types, super.NewField(node.Name, val.Type()))
	}
	return vector.NewRecord(loader.sctx.MustLookupTypeRecord(types), valFields, r.Length)
}
