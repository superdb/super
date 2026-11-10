package op

import (
	"context"
	"encoding/binary"
	"sync/atomic"

	"github.com/superdb/super"
	"github.com/superdb/super/runtime"
	"github.com/superdb/super/runtime/expr"
	"github.com/superdb/super/scode"
	"github.com/superdb/super/vector"
	"github.com/superdb/super/vector/vio"
	"golang.org/x/sync/errgroup"
)

type HashJoin struct {
	rctx       *runtime.Context
	style      string
	left       vio.Puller
	right      vio.Puller
	leftKey    expr.Evaluator
	rightKey   expr.Evaluator
	leftAlias  string
	rightAlias string

	hashJoin *hashJoin
}

func NewHashJoin(rctx *runtime.Context, style string, left, right vio.Puller,
	leftKey, rightKey expr.Evaluator, leftAlias, rightAlias string) *HashJoin {
	if style == "right" {
		leftKey, rightKey = rightKey, leftKey
		left, right = right, left
	}
	if style == "cross" {
		panic("cross join not compatible with hash join")
	}
	return &HashJoin{
		rctx:       rctx,
		style:      style,
		left:       left,
		right:      right,
		leftKey:    leftKey,
		rightKey:   rightKey,
		leftAlias:  leftAlias,
		rightAlias: rightAlias,
	}
}

func (h *HashJoin) Pull(done bool) (vector.Any, error) {
	if done {
		_, err := h.left.Pull(true)
		if err == nil {
			_, err = h.right.Pull(true)
		}
		h.hashJoin = nil
		return nil, err
	}
	if h.hashJoin == nil {
		if err := h.tableInit(); err != nil {
			return nil, err
		}
	}
	vec, err := h.hashJoin.Pull()
	if vec == nil || err != nil {
		h.hashJoin = nil
	}
	return vec, err
}

func (h *HashJoin) tableInit() error {
	// Read from both leftBuf and rightBuf parent and find the shortest parent to
	// create the table from.
	leftBuf, rightBuf, err := pullRace(h.rctx.Context, h.left, h.right)
	if err != nil {
		return err
	}
	var table map[string][]super.Value
	var left, right vio.Puller
	if rightBuf.EOS {
		table = buildTable(rightBuf.vecs, h.rightKey)
		left = leftBuf
	} else {
		table = buildTable(leftBuf.vecs, h.leftKey)
		right = rightBuf
	}
	h.hashJoin = &hashJoin{
		sctx:       h.rctx.Sctx,
		style:      h.style,
		table:      table,
		left:       left,
		right:      right,
		leftAlias:  h.leftAlias,
		rightAlias: h.rightAlias,
		leftKey:    h.leftKey,
		rightKey:   h.rightKey,
		hits:       make(map[string]bool),
	}
	return nil
}

func buildTable(vecs []vector.Any, key expr.Evaluator) map[string][]super.Value {
	var n int
	for _, vec := range vecs {
		n += int(vec.Len())
	}
	table := make(map[string][]super.Value, n)
	var sb scode.Builder
	for _, vec := range vecs {
		rightKeyVec := key.Eval(vec)
		for i := range vec.Len() {
			keyVal := vector.ValueAt(&sb, rightKeyVec, i)
			if keyVal.IsPlainNone() {
				continue
			}
			key := hashKey(keyVal)
			table[key] = append(table[key], vector.ValueAt(&sb, vec, i).Copy())
		}
	}
	return table
}

// pullRace pulls from a and b concurrently until one reaches EOS.  It returns
// bufPullers for a and b containing the vectors pulled from each.
func pullRace(ctx context.Context, a, b vio.Puller) (*bufPuller, *bufPuller, error) {
	var aBuf, bBuf *bufPuller
	var done atomic.Bool
	group, ctx := errgroup.WithContext(ctx)
	group.Go(func() error {
		var err error
		aBuf, err = pullUntilEOSOrDone(ctx, &done, a)
		return err
	})
	group.Go(func() error {
		var err error
		bBuf, err = pullUntilEOSOrDone(ctx, &done, b)
		return err
	})
	err := group.Wait()
	return aBuf, bBuf, err
}

func pullUntilEOSOrDone(ctx context.Context, done *atomic.Bool, parent vio.Puller) (*bufPuller, error) {
	b := &bufPuller{puller: parent}
	for ctx.Err() == nil && !done.Load() {
		vec, err := parent.Pull(false)
		if vec == nil || err != nil {
			done.Store(true)
			b.EOS = true
			return b, err
		}
		b.vecs = append(b.vecs, vec)
	}
	return b, nil
}

type hashJoin struct {
	sctx       *super.Context
	style      string
	table      map[string][]super.Value
	left       vio.Puller
	right      vio.Puller
	leftAlias  string
	rightAlias string
	leftKey    expr.Evaluator
	rightKey   expr.Evaluator
	builder    scode.Builder

	// for left side hash joins
	hits map[string]bool
}

func (j *hashJoin) Pull() (vector.Any, error) {
	if j.left != nil {
		return j.probeLeft()
	}
	return j.probeRight()
}

func (j *hashJoin) probeLeft() (vector.Any, error) {
	var sb scode.Builder
	for {
		vec, err := j.left.Pull(false)
		if vec == nil || err != nil {
			return nil, err
		}
		leftKeyVec := j.leftKey.Eval(vec)
		b := vector.NewDynamicValueBuilder()
		for i := range vec.Len() {
			keyVal := vector.ValueAt(&sb, leftKeyVec, i)
			if keyVal.IsPlainNone() {
				continue
			}
			var rightVals []super.Value
			var ok bool
			if !keyVal.IsNull() {
				rightVals, ok = j.table[hashKey(keyVal)]
			}
			leftVal := vector.ValueAt(&sb, vec, i)
			if !ok {
				if j.style != "inner" {
					b.Write(j.wrap(leftVal.Ptr(), nil))
				}
				continue
			}
			if j.style == "anti" {
				continue
			}
			for _, rightVal := range rightVals {
				b.Write(j.wrap(leftVal.Ptr(), rightVal.Ptr()))
			}
		}
		out := b.Build(j.sctx)
		if out.Len() > 0 {
			return out, nil
		}
	}
}

func (j *hashJoin) probeRight() (vector.Any, error) {
	var sb scode.Builder
	for {
		vec, err := j.right.Pull(false)
		if err != nil {
			return nil, err
		}
		if vec == nil {
			if j.hits != nil && j.style != "inner" {
				return j.drainLeftTable(), nil
			}
			return nil, nil
		}
		rightKeyVec := j.rightKey.Eval(vec)
		b := vector.NewDynamicValueBuilder()
		for i := range vec.Len() {
			keyVal := vector.ValueAt(&sb, rightKeyVec, i)
			if keyVal.IsPlainNone() {
				continue
			}
			var leftVals []super.Value
			if !keyVal.IsNull() {
				key := hashKey(keyVal)
				var ok bool
				leftVals, ok = j.table[key]
				if ok {
					j.hits[key] = true
				}
			}
			if j.style == "anti" {
				continue
			}
			rightVal := vector.ValueAt(&sb, vec, i)
			for _, leftVal := range leftVals {
				b.Write(j.wrap(leftVal.Ptr(), rightVal.Ptr()))
			}
		}
		out := b.Build(j.sctx)
		if out.Len() > 0 {
			return out, nil
		}
	}
}

func (j *hashJoin) drainLeftTable() vector.Any {
	b := vector.NewDynamicValueBuilder()
	for key, vals := range j.table {
		if j.hits[key] {
			continue
		}
		for _, val := range vals {
			b.Write(j.wrap(val.Ptr(), nil))
		}
	}
	j.hits = nil
	return b.Build(j.sctx)
}

func (j *hashJoin) wrap(l, r *super.Value) super.Value {
	if j.style == "right" {
		l, r = r, l
	}
	j.builder.Truncate()
	var fields []super.Field
	if l != nil {
		left := l.Deunion()
		fields = append(fields, super.Field{Name: j.leftAlias, Type: left.Type()})
		j.builder.Append(left.Bytes())
	}
	if r != nil {
		right := r.Deunion()
		fields = append(fields, super.Field{Name: j.rightAlias, Type: right.Type()})
		j.builder.Append(right.Bytes())

	}
	return super.NewValue(j.sctx.MustLookupTypeRecord(fields), j.builder.Bytes())
}

type bufPuller struct {
	vecs   []vector.Any
	EOS    bool
	puller vio.Puller
	done   bool
}

func (b *bufPuller) Pull(done bool) (vector.Any, error) {
	if done {
		if !b.EOS {
			return b.pull(done)
		}
		return nil, nil
	}
	if len(b.vecs) > 0 {
		vec := b.vecs[0]
		b.vecs = b.vecs[1:]
		return vec, nil
	}
	if b.EOS {
		return nil, nil
	}
	return b.pull(false)
}

func (b *bufPuller) pull(done bool) (vector.Any, error) {
	if b.done {
		return nil, nil
	}
	vec, err := b.puller.Pull(done)
	if vec == nil || err != nil {
		b.done = true
	}
	return vec, err
}

func hashKey(val super.Value) string {
	val = val.Deunion()
	return string(binary.LittleEndian.AppendUint32(val.Bytes(), uint32(val.Type().ID())))
}
