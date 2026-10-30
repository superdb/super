package expr

import (
	"github.com/RoaringBitmap/roaring/v2"
	"github.com/superdb/super"
	"github.com/superdb/super/vector"
)

type conditional struct {
	sctx      *super.Context
	defuse    *SuperDefuse
	predicate Evaluator
	thenExpr  Evaluator
	elseExpr  Evaluator
}

func NewConditional(sctx *super.Context, predicate, thenExpr, elseExpr Evaluator) Evaluator {
	return &conditional{
		sctx:      sctx,
		defuse:    NewSuperDefuse(sctx),
		predicate: predicate,
		thenExpr:  thenExpr,
		elseExpr:  elseExpr,
	}
}

func (c *conditional) Eval(this vector.Any) vector.Any {
	return vector.Apply(vector.ApplyRipUnions, c.eval, c.defuse.Eval(c.predicate.Eval(this)), &vector.NoRip{Any: this})
}

func (c *conditional) eval(vecs ...vector.Any) vector.Any {
	predVec, thisVec := vecs[0], vecs[1]
	switch predVec.Kind() {
	case vector.KindBool:
	case vector.KindNull:
		return c.elseExpr.Eval(thisVec)
	case vector.KindError:
		return predVec
	default:
		return vector.NewWrappedError(c.sctx, "?-operator: bool predicate required", predVec)
	}
	var elseIndex []uint32
	switch predVec := vector.Under(predVec).(type) {
	case *vector.Const:
		if vector.BoolValue(predVec, 0) {
			return c.thenExpr.Eval(thisVec)
		}
		return c.elseExpr.Eval(thisVec)
	case *vector.Bool:
		if predVec.IsZero() {
			return c.elseExpr.Eval(thisVec)
		}
		for i := range predVec.Len() {
			if !predVec.IsSet(i) {
				elseIndex = append(elseIndex, i)
			}
		}
	default:
		for i := range predVec.Len() {
			if !vector.BoolValue(predVec, i) {
				elseIndex = append(elseIndex, i)
			}
		}
	}
	if len(elseIndex) == 0 {
		return c.thenExpr.Eval(thisVec)
	}
	elseVec := c.elseExpr.Eval(thisVec)
	if len(elseIndex) == int(elseVec.Len()) {
		return elseVec
	}
	thenVec := c.thenExpr.Eval(thisVec)
	return combine(thenVec, elseVec, elseIndex)
}

func BoolMask(mask vector.Any) (*roaring.Bitmap, *roaring.Bitmap) {
	mask = vector.Apply(vector.ApplyRipFusions|vector.ApplyRipUnions, func(vecs ...vector.Any) vector.Any {
		return vecs[0]
	}, mask)
	bools := roaring.New()
	other := roaring.New()
	if dynamic, ok := mask.(*vector.Dynamic); ok {
		reverse := dynamic.ReverseTagMap()
		for i, val := range dynamic.Values {
			boolMaskRidx(reverse[i], bools, other, val)
		}
	} else {
		boolMaskRidx(nil, bools, other, mask)
	}
	return bools, other
}

func boolMaskRidx(ridx []uint32, bools, other *roaring.Bitmap, vec vector.Any) {
	switch vec := vec.(type) {
	case *vector.Const:
		if vec.Type().ID() != super.IDBool {
			if ridx != nil {
				other.AddMany(ridx)
			} else {
				other.AddRange(0, uint64(vec.Len()))
			}
			return
		}
		if !vector.BoolValue(vec, 0) {
			return
		}
		if ridx != nil {
			bools.AddMany(ridx)
		} else {
			bools.AddRange(0, uint64(vec.Len()))
		}
	case *vector.Bool:
		trues := vec.Bits
		if ridx != nil {
			for i, idx := range ridx {
				if trues.IsSetDirect(uint32(i)) {
					bools.Add(idx)
				}
			}
		} else {
			bools.Or(roaring.FromDense(trues.GetBits(), true))
		}
	default:
		if ridx != nil {
			other.AddMany(ridx)
		} else {
			other.AddRange(0, uint64(vec.Len()))
		}
	}
}
