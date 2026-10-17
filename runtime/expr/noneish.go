package expr

import (
	"github.com/superdb/super/vector"
)

type Noneish struct {
	lhs Evaluator
	rhs Evaluator
}

func NewNoneish(lhs, rhs Evaluator) *Noneish {
	return &Noneish{lhs, rhs}
}

func (i *Noneish) Eval(this vector.Any) vector.Any {
	lhs := i.lhs.Eval(this)
	rhs := i.rhs.Eval(this)
	return vector.Apply(vector.ApplyRipUnions|vector.ApplyRipFusions, i.eval, lhs, vector.AddNoRip(rhs))
}

func (i *Noneish) eval(vecs ...vector.Any) vector.Any {
	lhs := vecs[0]
	rhs := vecs[1]
	k := vector.Under(lhs).Kind()
	if k == vector.KindNull || k == vector.KindMissing {
		return rhs
	}
	if k == vector.KindOption {
		return vector.UnderOption(lhs).Coaelesce(rhs)
	}
	return lhs
}
