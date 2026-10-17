package expr

import (
	"github.com/superdb/super"
	"github.com/superdb/super/scode"
	"github.com/superdb/super/vector"
)

type setExpr struct {
	sctx   *super.Context
	defuse *Defuse
	elems  []ListElem
}

func NewSetExpr(sctx *super.Context, elems []ListElem) Evaluator {
	return &setExpr{sctx, NewDefuse(sctx), elems}
}

func (s *setExpr) Eval(this vector.Any) vector.Any {
	if len(s.elems) == 0 {
		typ := s.sctx.LookupTypeSet(super.TypeMissing)
		offsets := make([]uint32, this.Len()+1)
		c := vector.NewNull(0)
		return vector.NewSet(typ, offsets, c)
	}
	var vecs []vector.Any
	for _, e := range s.elems {
		var vec vector.Any
		if e.Spread != nil {
			vec = s.defuse.Eval(e.Spread.Eval(this))
		} else {
			vec = vector.AddNoRip(s.defuse.Eval(e.Value.Eval(this)))
		}
		vecs = append(vecs, vec)
	}
	return vector.Apply(vector.ApplyRipUnions, s.eval, vecs...)
}

func (a *setExpr) eval(in ...vector.Any) vector.Any {
	offsets, inner := buildList(a.sctx, a.elems, in)
	// Dedupe list elems
	vb := vector.NewValueBuilder(a.sctx.LookupTypeSet(inner.Type()))
	var b scode.Builder
	for i := range len(offsets) - 1 {
		b.Truncate()
		for off := offsets[i]; off < offsets[i+1]; off++ {
			inner.Serialize(&b, off)
		}
		vb.Write(super.NormalizeSet(b.Bytes()))
	}
	return vb.Build(a.sctx)
}
