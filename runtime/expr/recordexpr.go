package expr

import (
	"slices"

	"github.com/superdb/super"
	"github.com/superdb/super/vector"
)

type RecordElem interface {
	recordElemSum()
}

type FieldElem struct {
	Name string
	Expr Evaluator
}

type SpreadElem struct {
	Expr Evaluator
}

func (*FieldElem) recordElemSum()  {}
func (*SpreadElem) recordElemSum() {}

func NewRecordExpr(sctx *super.Context, elems []RecordElem) Evaluator {
	return &recordExpr{
		sctx:         sctx,
		elems:        elems,
		fieldIndexes: map[string]int{},
		defuse:       NewDefuse(sctx),
	}
}

type recordExpr struct {
	sctx  *super.Context
	elems []RecordElem

	elemVecs     []vector.Any
	fields       []super.Field
	fieldIndexes map[string]int
	fieldVecs    []vector.Any
	// defuse is for defusing spread values.
	defuse *Defuse
}

func (r *recordExpr) Eval(this vector.Any) vector.Any {
	if len(r.elems) == 0 {
		typ := r.sctx.MustLookupTypeRecord(nil)
		return vector.NewRecord(typ, nil, this.Len())
	}
	r.elemVecs = r.elemVecs[:0]
	for _, elem := range r.elems {
		var vec vector.Any
		switch elem := elem.(type) {
		case *FieldElem:
			vec = elem.Expr.Eval(this)
		case *SpreadElem:
			vec = r.defuse.Eval(elem.Expr.Eval(this))
		default:
			panic(elem)
		}
		r.elemVecs = append(r.elemVecs, vec)
	}
	return vector.Apply(vector.ApplyRipOptions, r.eval, r.elemVecs...)
}

func (r *recordExpr) eval(vecs ...vector.Any) vector.Any {
	r.fields = r.fields[:0]
	clear(r.fieldIndexes)
	r.fieldVecs = make([]vector.Any, 0, len(r.elems))
	for k, vec := range vecs {
		switch elem := r.elems[k].(type) {
		case *FieldElem:
			if vec.Type() == super.TypeNone {
				r.deleteField(elem.Name)
				continue
			}
			r.addOrUpdateField(elem.Name, vec)
		case *SpreadElem:
			r.spread(vec)
		default:
			panic(elem)
		}
	}
	typ := r.sctx.MustLookupTypeRecord(r.fields)
	return vector.NewRecord(typ, r.fieldVecs, vecs[0].Len())
}

func (r *recordExpr) addOrUpdateField(name string, vec vector.Any) {
	if i, ok := r.fieldIndexes[name]; ok {
		r.fields[i].Type = vec.Type()
		r.fieldVecs[i] = vec
		return
	}
	r.fieldIndexes[name] = len(r.fields)
	r.fields = append(r.fields, super.NewField(name, vec.Type()))
	r.fieldVecs = append(r.fieldVecs, vec)
}

func (r *recordExpr) deleteField(name string) {
	if i, ok := r.fieldIndexes[name]; ok {
		r.fields = slices.Delete(r.fields, i, i+1)
		r.fieldVecs = slices.Delete(r.fieldVecs, i, i+1)
	}
}

func (r *recordExpr) spread(vec vector.Any) {
	// Ignore non-record values.
	switch vec := vector.Under(vec).(type) {
	case *vector.Record:
		for k, f := range super.TypeRecordOf(vec.Type()).Fields {
			r.addOrUpdateField(f.Name, vec.Fields[k])
		}
	case *vector.View:
		if rec, ok := vec.Any.(*vector.Record); ok {
			for k, f := range super.TypeRecordOf(rec.Type()).Fields {
				r.addOrUpdateField(f.Name, vector.Pick(rec.Fields[k], vec.Index))
			}
		}
	}
}
