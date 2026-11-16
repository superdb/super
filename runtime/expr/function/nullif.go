package function

import (
	"github.com/superdb/super"
	"github.com/superdb/super/runtime/expr"
	"github.com/superdb/super/vector"
)

type NullIf struct {
	compare *expr.Compare
}

func newNullIf(sctx *super.Context) *NullIf {
	return &NullIf{expr.NewLenientCompare(sctx, "==")}
}

func (*NullIf) ApplyOpt() vector.ApplyOpt { return vector.ApplyNone }

func (n *NullIf) Call(vecs ...vector.Any) vector.Any {
	if k := vecs[0].Kind(); k == vector.KindNull || k == vector.KindError {
		return vecs[0]
	}
	if vecs[1].Kind() == vector.KindError {
		return vecs[1]
	}
	bools, _ := expr.BoolMask(vector.Apply(vector.ApplyRipUnions, func(vecs ...vector.Any) vector.Any {
		return n.compare.Compare(vecs[0], vecs[1])
	}, vecs[0], vecs[1]))
	if bools.IsEmpty() {
		return vecs[0]
	}
	if bools.GetCardinality() == uint64(vecs[0].Len()) {
		return vector.NewNull(vecs[0].Len())
	}
	index := bools.ToArray()
	nullsVec := vector.NewNull(uint32(len(index)))
	return vector.Combine(vector.ReversePick(vecs[0], index), index, nullsVec)
}
