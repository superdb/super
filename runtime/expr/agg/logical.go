package agg

import (
	"github.com/superdb/super"
	"github.com/superdb/super/runtime/expr"
	"github.com/superdb/super/vector"
	"github.com/superdb/super/vector/bitvec"
)

// logical computes "and" or "or" for each group.
type logical struct {
	isAnd bool
	vals  bitvec.Bits
	has   []bool // whether a group has seen a value
}

func newAnd() *logical { return &logical{isAnd: true} }
func newOr() *logical  { return &logical{} }

func (l *logical) Resize(n uint32) {
	l.vals.Resize(n)
	l.has = extend(l.has, n)
}

func (l *logical) Consume(groups []uint32, vec vector.Any) {
	if vec.Type().ID() != super.IDBool {
		return
	}
	bools := expr.FlattenBool(vector.Under(vec))
	for i, g := range groups {
		b := bools.IsSetDirect(uint32(i))
		if !l.has[g] {
			l.has[g] = true
			l.vals.SetTo(g, b)
		} else if l.isAnd {
			l.vals.SetTo(g, l.vals.IsSetDirect(g) && b)
		} else {
			l.vals.SetTo(g, l.vals.IsSetDirect(g) || b)
		}
	}
}

func (l *logical) Result(*super.Context) vector.Any {
	var nulls []uint32
	for g, has := range l.has {
		if !has {
			nulls = append(nulls, uint32(g))
			continue
		}
	}
	bools := vector.NewBool(l.vals)
	if len(nulls) == 0 {
		return bools
	}
	null := vector.NewNull(uint32(len(nulls)))
	if len(nulls) == len(l.has) {
		return null
	}
	return vector.Combine(vector.ReversePick(bools, nulls), nulls, null)
}

func (l *logical) ConsumeAsPartial(groups []uint32, partial vector.Any) {
	if kind := partial.Kind(); kind != vector.KindBool && kind != vector.KindNull {
		panic("and/or: bad partial")
	}
	l.Consume(groups, partial)
}

func (l *logical) ResultAsPartial(*super.Context) vector.Any {
	return l.Result(nil)
}
