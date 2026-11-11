package op

import (
	"iter"
	"slices"

	"github.com/RoaringBitmap/roaring/v2"
	"github.com/superdb/super"
	"github.com/superdb/super/runtime"
	"github.com/superdb/super/runtime/expr"
	"github.com/superdb/super/vector"
	"github.com/superdb/super/vector/vio"
)

type nestedLoopJoin struct {
	rctx       *runtime.Context
	left       vio.Puller
	right      vio.Puller
	style      string
	leftAlias  string
	rightAlias string
	cond       expr.Evaluator
	// catch is true if the join outputs a SideErrors for the joined values
	// on which cond produces an error.  Otherwise, they're dropped.
	catch bool

	next func() (vector.Any, error, bool)
	stop func()

	pickSlotIndexes [][]uint32
}

func NewNestedLoopJoin(rctx *runtime.Context, left, right vio.Puller, style string, leftAlias, rightAlias string, cond expr.Evaluator, catch bool) vio.Puller {
	return &nestedLoopJoin{
		rctx:       rctx,
		left:       left,
		right:      right,
		style:      style,
		leftAlias:  leftAlias,
		rightAlias: rightAlias,
		cond:       cond,
		catch:      catch,
	}
}

func (n *nestedLoopJoin) Pull(done bool) (vector.Any, error) {
	if done {
		n.done()
		_, err := n.left.Pull(true)
		if err == nil {
			_, err = n.right.Pull(true)
		}
		return nil, err
	}
	if n.next == nil {
		n.next, n.stop = iter.Pull2(n.join)
	}
	vec, err, ok := n.next()
	if vec == nil || err != nil || !ok {
		n.done()
	}
	return vec, err
}

func (n *nestedLoopJoin) done() {
	n.next = nil
	if n.stop != nil {
		n.stop()
		n.stop = nil
	}
}

// join performs the join, passing results to yield.  It implements iter.Seq2.
func (n *nestedLoopJoin) join(yield func(vector.Any, error) bool) {
	// outer and inner are inputs for the outer and inner loops below.
	outer, inner, err := pullRace(n.rctx.Context, n.left, n.right)
	if err != nil {
		yield(nil, err)
		return
	}
	var innerIsLeft bool
	if outer.EOS {
		innerIsLeft = true
		outer, inner = inner, outer
	}
	var innerHits []roaring.Bitmap
	var outerHits *roaring.Bitmap
	if innerIsLeft && (n.style == "anti" || n.style == "left") ||
		!innerIsLeft && n.style == "right" {
		innerHits = make([]roaring.Bitmap, len(inner.vecs))
	} else if !innerIsLeft && (n.style == "anti" || n.style == "left") ||
		innerIsLeft && n.style == "right" {
		outerHits = roaring.New()
	}
	for {
		outerVec, err := outer.Pull(false)
		if err != nil {
			yield(nil, err)
			return
		}
		if outerVec == nil {
			break
		}
		for i := range outerVec.Len() {
			for j, innerVec := range inner.vecs {
				outerVec := n.pickSlot(outerVec, i, innerVec.Len())
				leftVec, rightVec := outerVec, innerVec
				if innerIsLeft {
					leftVec, rightVec = rightVec, leftVec
				}
				leftVec, rightVec = vector.Deunion(leftVec), vector.Deunion(rightVec)
				joinedVec := vector.Apply(vector.ApplyNone, n.makeResult, leftVec, rightVec)
				if n.style != "cross" {
					condVec := n.cond.Eval(joinedVec)
					hits, errs := expr.BoolMask(condVec)
					if n.catch && !errs.IsEmpty() {
						slots := errs.ToArray()
						sideErrs := NewSideErrors(n.rctx.Sctx, "join: condition error", vector.Pick(joinedVec, slots), vector.Pick(condVec, slots))
						if !yield(sideErrs, nil) {
							return
						}
					}
					if len(innerHits) > 0 {
						innerHits[j].Or(hits)
					} else if outerHits != nil {
						if !hits.IsEmpty() {
							outerHits.Add(i)
						}
					}
					if n.style == "anti" || hits.IsEmpty() {
						continue
					}
					if hits.GetCardinality() < uint64(joinedVec.Len()) {
						joinedVec = vector.Pick(joinedVec, hits.ToArray())
					}
				}
				if !yield(joinedVec, nil) {
					return
				}
			}
		}
		if outerHits != nil {
			vec, ok := n.makeHitsResult(outerVec, outerHits)
			if ok && !yield(vec, nil) {
				return
			}
			outerHits.Clear()
		}
	}
	for i := range innerHits {
		vec, ok := n.makeHitsResult(inner.vecs[i], &innerHits[i])
		if ok && !yield(vec, nil) {
			return
		}
	}
	yield(nil, nil)
}

func (n *nestedLoopJoin) pickSlot(vec vector.Any, slot, length uint32) vector.Any {
	if l := int(vec.Len()); l > len(n.pickSlotIndexes) {
		n.pickSlotIndexes = slices.Grow(n.pickSlotIndexes[:0], l)[:l]
	}
	index := n.pickSlotIndexes[slot]
	if len(index) < int(length) {
		index = slices.Grow(index[:0], int(length))[:length]
		for i := range index {
			index[i] = slot
		}
		n.pickSlotIndexes[slot] = index
	}
	return vector.Pick(vec, index[:length])
}

func (n *nestedLoopJoin) makeResult(vecs ...vector.Any) vector.Any {
	left, right := vecs[0], vecs[1]
	typ := n.rctx.Sctx.MustLookupTypeRecord([]super.Field{
		super.NewField(n.leftAlias, left.Type()),
		super.NewField(n.rightAlias, right.Type()),
	})
	return vector.NewRecord(typ, []vector.Any{left, right}, left.Len())
}

func (n *nestedLoopJoin) makeHitsResult(vec vector.Any, hits *roaring.Bitmap) (vector.Any, bool) {
	vecLen := uint64(vec.Len())
	if hits.GetCardinality() == vecLen {
		// No misses.
		return nil, false
	}
	hits.Flip(0, vecLen)
	missVec := vector.Pick(vec, hits.ToArray())
	fieldName := n.leftAlias
	if n.style == "right" {
		fieldName = n.rightAlias
	}
	return vector.Apply(vector.ApplyNone, func(vecs ...vector.Any) vector.Any {
		typ := n.rctx.Sctx.MustLookupTypeRecord([]super.Field{
			super.NewField(fieldName, vecs[0].Type()),
		})
		return vector.NewRecord(typ, vecs, vecs[0].Len())
	}, missVec), true
}
