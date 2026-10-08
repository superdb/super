package loader

import (
	"fmt"
	"slices"
	"sync"

	"github.com/superdb/super"
	"github.com/superdb/super/bsup"
	"github.com/superdb/super/pkg/field"
	"github.com/superdb/super/sup"
	"github.com/superdb/super/vector"
)

type record struct {
	mu     sync.Mutex
	meta   *bsup.Record
	len    uint32
	fields []shadow
}

func newRecord(cctx *bsup.Context, meta *bsup.Record) *record {
	fields := make([]shadow, len(meta.Fields))
	len := meta.Len(cctx)
	return &record{
		meta:   meta,
		len:    len,
		fields: fields,
	}
}

func (r *record) length() uint32 {
	return r.len
}

func (r *record) unmarshal(cctx *bsup.Context, projection field.Projection) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(projection) == 0 {
		// Unmarshal all the fields of this record.  We're either loading all on demand (nil paths)
		// or loading this record because it's referenced at the end of a projected path.
		for k := range r.fields {
			if r.fields[k] == nil {
				r.fields[k] = newShadow(cctx, r.meta.Fields[k].Values)
			}
			r.fields[k].unmarshal(cctx, nil)
		}
		return
	}
	for _, node := range projection {
		if k := indexOfField(node.Name, r.meta); k >= 0 {
			if r.fields[k] == nil {
				r.fields[k] = newShadow(cctx, r.meta.Fields[k].Values)
			}
			r.fields[k].unmarshal(cctx, node.Proj)
		}
	}
}

func (r *record) project(loader *loader, projection field.Projection) vector.Any {
	valFields := make([]vector.Any, 0, len(r.fields))
	types := make([]super.Field, 0, len(r.fields))
	if len(projection) == 0 {
		// Build the whole record.  We're either loading all on demand (nil paths)
		// or loading this record because it's referenced at the end of a projected path.
		for k := range r.fields {
			val := r.fields[k].project(loader, nil)
			valFields = append(valFields, val)
			types = append(types, super.NewField(r.meta.Fields[k].Name, val.Type()))
		}
		return vector.NewRecord(loader.sctx.MustLookupTypeRecord(types), valFields, r.length())
	}
	fields := make([]super.Field, 0, len(r.fields))
	for _, node := range projection {
		var val vector.Any
		if k := indexOfField(node.Name, r.meta); k >= 0 && r.fields[k] != nil {
			val = r.fields[k].project(loader, node.Proj)
		} else {
			val = vector.NewStringError(loader.sctx, fmt.Sprintf("no such field %s", sup.QuotedName(node.Name)), r.length())
		}
		valFields = append(valFields, val)
		fields = append(fields, super.NewField(node.Name, val.Type()))
	}
	return vector.NewRecord(loader.sctx.MustLookupTypeRecord(fields), valFields, r.length())
}

func indexOfField(name string, r *bsup.Record) int {
	return slices.IndexFunc(r.Fields, func(f bsup.Field) bool {
		return f.Name == name
	})
}
