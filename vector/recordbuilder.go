package vector

import (
	"slices"

	"github.com/superdb/super"
	"github.com/superdb/super/pkg/field"
)

type RecordBuilder struct {
	sctx *super.Context
	base *rec
	// NoneDrops is the number of leading leaf fields (e.g., an aggregate's
	// group keys) that disappear from a record when their values are plain
	// nones (e.g., from ok() or ?.), as in a record expression.
	NoneDrops int
}

func NewRecordBuilder(sctx *super.Context, fields field.List) (*RecordBuilder, error) {
	base := &rec{}
	for _, path := range fields {
		if err := addPath(base, path); err != nil {
			return nil, err
		}
	}
	return &RecordBuilder{sctx: sctx, base: base}, nil
}

func (r *RecordBuilder) New(vecs []Any) *Record {
	n := len(vecs)
	rec, _ := r.base.build(r.sctx, vecs, func(left int) bool {
		// The leaf's index is the number of leaves before it.
		return n-left < r.NoneDrops
	})
	return rec
}

type rec struct {
	paths []string
	recs  []*rec
}

func addPath(r *rec, path field.Path) error {
	for k, name := range path {
		idx := slices.Index(r.paths, name)
		if k == len(path)-1 {
			if idx > -1 {
				return &super.DuplicateFieldError{Name: path.String()}
			}
			r.paths = append(r.paths, path[k])
			r.recs = append(r.recs, nil)
			return nil
		}
		if idx == -1 {
			idx = len(r.paths)
			r.paths = append(r.paths, name)
			r.recs = append(r.recs, &rec{})
		}
		if r.recs[idx] == nil {
			return &super.DuplicateFieldError{Name: path[:k+1].String()}
		}
		r = r.recs[idx]
	}
	return nil
}

// build returns a record whose leaf fields are the leading values of leafs
// and the rest of leafs.  A leaf field whose value is a plain none disappears
// from the record if dropNone is true for it, where dropNone is called with
// the number of leaves left including it.
func (r *rec) build(sctx *super.Context, leafs []Any, dropNone func(int) bool) (*Record, []Any) {
	n := leafs[0].Len()
	var fields []super.Field
	var out []Any
	for i, name := range r.paths {
		var vec Any
		if r.recs[i] != nil {
			vec, leafs = r.recs[i].build(sctx, leafs, dropNone)
		} else {
			drop := leafs[0].Type() == super.TypeNone && dropNone(len(leafs))
			vec, leafs = leafs[0], leafs[1:]
			if drop {
				continue
			}
		}
		fields = append(fields, super.NewField(name, vec.Type()))
		out = append(out, vec)
	}
	typ := sctx.MustLookupTypeRecord(fields)
	return NewRecord(typ, out, n), leafs
}
