package expr

import (
	"slices"

	"github.com/superdb/super"
	"github.com/superdb/super/vector"
	"github.com/superdb/super/vector/vbuild"
)

type ListElem struct {
	Value  Evaluator
	Spread Evaluator
}

type ArrayExpr struct {
	elems  []ListElem
	defuse *Defuse
	sctx   *super.Context
}

func NewArrayExpr(sctx *super.Context, elems []ListElem) *ArrayExpr {
	return &ArrayExpr{
		elems:  elems,
		defuse: NewDefuse(sctx),
		sctx:   sctx,
	}
}

func (a *ArrayExpr) Eval(this vector.Any) vector.Any {
	if len(a.elems) == 0 {
		typ := a.sctx.LookupTypeArray(super.TypeMissing)
		offsets := make([]uint32, this.Len()+1)
		nullVec := vector.NewNull(0)
		return vector.NewArray(typ, offsets, nullVec)
	}
	var vecs []vector.Any
	for _, e := range a.elems {
		var vec vector.Any
		if e.Spread != nil {
			vec = a.defuse.Eval(e.Spread.Eval(this))
		} else {
			vec = vector.AddNoRip(a.defuse.Eval(e.Value.Eval(this)))
		}
		vecs = append(vecs, vec)
	}
	return vector.Apply(vector.ApplyRipUnions, a.eval, vecs...)
}

func (a *ArrayExpr) eval(in ...vector.Any) vector.Any {
	offsets, inner := buildList(a.sctx, a.elems, in)
	return vector.NewArray(a.sctx.LookupTypeArray(inner.Type()), offsets, inner)
}

func buildList(sctx *super.Context, elems []ListElem, in []vector.Any) ([]uint32, vector.Any) {
	var hasSpreads bool
	var vecs []vector.Any
	var spreadOffsets [][]uint32
	for i, elem := range elems {
		var offsets []uint32
		vec := in[i]
		if elem.Spread != nil {
			vec, offsets = unwrapSpread(in[i])
			if vec == nil {
				// drop unspreadable elements.
				continue
			}
			hasSpreads = true
		}
		vecs = append(vecs, vec)
		spreadOffsets = append(spreadOffsets, offsets)
	}
	if len(vecs) == 0 {
		n := in[0].Len()
		offsets := make([]uint32, n+1)
		return offsets, vector.NewNull(n)
	}
	if len(vecs) == 1 {
		offsets := spreadOffsets[0]
		if offsets == nil {
			offsets = buildStaticOffsets(1, vecs[0].Len())
		}
		return offsets, vecs[0]
	}
	var tags, offsets []uint32
	if hasSpreads {
		offsets, tags = buildListOffsetsAndTagsForSpreads(vecs, spreadOffsets, in[0].Len())
	} else {
		offsets, tags = buildListOffsetsAndTags(vecs)
	}
	d := vector.FlattenUnions(vector.NewDynamic(tags, vecs))
	inner := vbuild.MergeSameTypesInDynamic(d)
	if d, ok := inner.(*vector.Dynamic); ok {
		inner = vector.NewUnionFromDynamic(sctx, d)
	}
	return offsets, inner
}

func buildListOffsetsAndTags(vecs []vector.Any) ([]uint32, []uint32) {
	var repeat []uint32
	for i := range uint32(len(vecs)) {
		repeat = append(repeat, i)
	}
	tags := slices.Repeat(repeat, int(vecs[0].Len()))
	return buildStaticOffsets(uint32(len(vecs)), vecs[0].Len()), tags
}

func buildListOffsetsAndTagsForSpreads(vecs []vector.Any, spreadOffsets [][]uint32, length uint32) ([]uint32, []uint32) {
	var tags []uint32
	offsets := []uint32{0}
	var off uint32
	for i := range length {
		for k := range vecs {
			offlen := uint32(1)
			if offsets := spreadOffsets[k]; offsets != nil {
				offlen = offsets[i+1] - offsets[i]
			}
			for range offlen {
				tags = append(tags, uint32(k))
			}
			off += offlen
		}
		offsets = append(offsets, off)
	}
	return offsets, tags
}

func buildStaticOffsets(incr, len uint32) []uint32 {
	offsets := make([]uint32, len+1)
	for i := range len + 1 {
		offsets[i] = incr * i
	}
	return offsets
}

func unwrapSpread(vec vector.Any) (vector.Any, []uint32) {
	if k := vec.Kind(); k != vector.KindArray && k != vector.KindSet {
		return nil, nil
	}
	switch vec := vector.PushView(vec).(type) {
	case *vector.Array:
		return vec.Values, vec.Offsets
	case *vector.Set:
		return vec.Values, vec.Offsets
	default:
		panic(vec)
	}
}
