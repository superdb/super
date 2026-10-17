package expr

import (
	"slices"

	"github.com/superdb/super"
	"github.com/superdb/super/vector"
	"github.com/superdb/super/vector/vbuild"
)

type Entry struct {
	Key Evaluator
	Val Evaluator
}

type MapExpr struct {
	sctx    *super.Context
	defuse  *Defuse
	entries []Entry
}

func NewMapExpr(sctx *super.Context, entries []Entry) *MapExpr {
	return &MapExpr{
		sctx:    sctx,
		defuse:  NewDefuse(sctx),
		entries: entries,
	}
}

func (m *MapExpr) Eval(this vector.Any) vector.Any {
	if len(m.entries) == 0 {
		mtyp := m.sctx.LookupTypeMap(super.TypeMissing, super.TypeMissing)
		offsets := make([]uint32, this.Len()+1)
		c := vector.NewNull(0)
		return vector.NewMap(mtyp, offsets, c, c)
	}
	var vecs []vector.Any
	for _, entry := range m.entries {
		vecs = append(vecs, m.defuse.Eval(entry.Key.Eval(this)))
	}
	for _, entry := range m.entries {
		vecs = append(vecs, m.defuse.Eval(entry.Val.Eval(this)))
	}
	return vector.Apply(vector.ApplyNone, m.eval, vecs...)
}

func (m *MapExpr) eval(vecs ...vector.Any) vector.Any {
	n := len(m.entries)
	key := m.build(vecs[:n])
	val := m.build(vecs[n:])
	offsets := buildStaticOffsets(uint32(n), vecs[0].Len())
	mtyp := m.sctx.LookupTypeMap(key.Type(), val.Type())
	return vector.NewMap(mtyp, offsets, key, val)
}

func (m *MapExpr) build(vecs []vector.Any) vector.Any {
	if len(vecs) == 1 {
		return vecs[0]
	}
	repeat := make([]uint32, len(vecs))
	for i := range uint32(len(vecs)) {
		repeat[i] = i
	}
	tags := slices.Repeat(repeat, int(vecs[0].Len()))
	d := vector.FlattenUnions(vector.NewDynamic(tags, vecs))
	out := vbuild.MergeSameTypesInDynamic(d)
	if d, ok := out.(*vector.Dynamic); ok {
		out = vector.NewUnionFromDynamic(m.sctx, d)
	}
	return out
}
