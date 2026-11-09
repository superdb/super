package vector

import (
	"slices"

	"github.com/superdb/super"
	"github.com/superdb/super/pkg/field"
)

type RecordBuilder struct {
	sctx *super.Context
	base *rec
	// NoneDrops is the number of leading fields (e.g., an aggregate's group
	// keys) that disappear from a record when their values are plain nones
	// (e.g., from ok() or ?.), as in a record expression.
	NoneDrops int
}

// NewRecordBuilder returns a builder of records with the leaf fields in
// fields.  Paths with a common prefix are nested in the same record, in the
// order in which the prefix first appears.
func NewRecordBuilder(sctx *super.Context, fields field.List) (*RecordBuilder, error) {
	base := &rec{}
	for k, path := range fields {
		if err := addPath(base, path, k); err != nil {
			return nil, err
		}
	}
	return &RecordBuilder{sctx: sctx, base: base}, nil
}

// New returns a record whose leaf fields are vecs in the order of the fields
// passed to NewRecordBuilder.
func (r *RecordBuilder) New(vecs []Any) *Record {
	return r.base.build(r.sctx, vecs, r.NoneDrops)
}

type rec struct {
	paths []string
	// recs[k] is the record for paths[k] or nil if it's a leaf, in which
	// case leaves[k] is the index of its value in the vectors passed to New.
	recs   []*rec
	leaves []int
}

func addPath(r *rec, path field.Path, leaf int) error {
	for k, name := range path {
		idx := slices.Index(r.paths, name)
		if k == len(path)-1 {
			if idx > -1 {
				return &super.DuplicateFieldError{Name: path.String()}
			}
			r.paths = append(r.paths, path[k])
			r.recs = append(r.recs, nil)
			r.leaves = append(r.leaves, leaf)
			return nil
		}
		if idx == -1 {
			idx = len(r.paths)
			r.paths = append(r.paths, name)
			r.recs = append(r.recs, &rec{})
			r.leaves = append(r.leaves, -1)
		}
		if r.recs[idx] == nil {
			return &super.DuplicateFieldError{Name: path[:k+1].String()}
		}
		r = r.recs[idx]
	}
	return nil
}

// build returns a record whose leaf fields are taken from leafs.  A leaf field
// whose index is less than noneDrops and whose value is a plain none
// disappears from the record.
func (r *rec) build(sctx *super.Context, leafs []Any, noneDrops int) *Record {
	n := leafs[0].Len()
	var fields []super.Field
	var out []Any
	for i, name := range r.paths {
		var vec Any
		if r.recs[i] != nil {
			vec = r.recs[i].build(sctx, leafs, noneDrops)
		} else {
			leaf := r.leaves[i]
			vec = leafs[leaf]
			if leaf < noneDrops && vec.Type() == super.TypeNone {
				continue
			}
		}
		fields = append(fields, super.NewField(name, vec.Type()))
		out = append(out, vec)
	}
	typ := sctx.MustLookupTypeRecord(fields)
	return NewRecord(typ, out, n)
}
