package agg

import (
	"github.com/superdb/super"
	"github.com/superdb/super/vector"
)

// logical computes "and" or "or" for each group.
type logical struct {
	isAnd bool
	vals  []bool
	has   []bool // whether a group has seen a value
}

func newAnd() *logical { return &logical{isAnd: true} }
func newOr() *logical  { return &logical{} }

func (l *logical) Resize(n uint32) {
	l.vals = append(l.vals, make([]bool, n-uint32(len(l.vals)))...)
	l.has = append(l.has, make([]bool, n-uint32(len(l.has)))...)
}

func (l *logical) Consume(groups []uint32, vec vector.Any) {
	if vec.Type().ID() != super.IDBool {
		return
	}
	for i, g := range orZeros(groups, vec.Len()) {
		b := vector.BoolValue(vec, uint32(i))
		if !l.has[g] {
			l.has[g] = true
			l.vals[g] = b
		} else if l.isAnd {
			l.vals[g] = l.vals[g] && b
		} else {
			l.vals[g] = l.vals[g] || b
		}
	}
}

func (l *logical) Result(*super.Context) vector.Any {
	tags := make([]uint8, len(l.vals))
	for g, has := range l.has {
		if has {
			if l.vals[g] {
				tags[g] = 2
			} else {
				tags[g] = 1
			}
		}
	}
	return buildByTag(tags, 3, func(tag uint8, groups []uint32) vector.Any {
		n := uint32(len(l.vals))
		if groups != nil {
			n = uint32(len(groups))
		}
		switch tag {
		case 0:
			return vector.NewNull(n)
		case 1:
			return vector.NewFalse(n)
		default:
			return vector.NewTrue(n)
		}
	})
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
