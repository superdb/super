package op

import (
	"github.com/superdb/super"
	"github.com/superdb/super/runtime/sam/expr"
)

type pruner struct {
	pred expr.Evaluator
}

func newPruner(e expr.Evaluator) *pruner {
	return &pruner{
		pred: e,
	}
}

func (p *pruner) prune(val super.Value) bool {
	if p == nil {
		return false
	}
	result := p.pred.Eval(val)
	return result.Type() == super.TypeBool && result.Bool()
}
