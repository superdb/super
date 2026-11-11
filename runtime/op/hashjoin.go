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
	// sideErrs is non-nil if the join outputs a SideErrors for the values
	// whose key has an error in it.  Otherwise, those keys are hashed like
	// any other.
	sideErrs *sideErrs
	// eos is true if EOS is due once the pending side errors are output.
	eos bool

	hashJoin *hashJoin
}

// sideErrs holds the side errors of a hash join that are yet to be output.
type sideErrs struct {
	sctx    *super.Context
	pending []vector.Any
}

// keyErrors records a side error for each value of vec whose key, the value at
// the same slot of keyVec, has an error in it and returns a function that's
// true for the slots of those values.  The values are wrapped in a record
// with the field named alias, as for an outer join.
func (s *sideErrs) keyErrors(vec, keyVec vector.Any, alias string) func(uint32) bool {
	if s == nil {
		return func(uint32) bool { return false }
	}
	slots := hasErrorSlots(s.sctx, keyVec)
	if len(slots) == 0 {
		return func(uint32) bool { return false }
	}
	on := vector.Pick(vec, slots)
	b := vector.NewDynamicValueBuilder()
	var sb scode.Builder
	for slot := range on.Len() {
		val := vector.ValueAt(&sb, on, slot).Deunion()
		typ := s.sctx.MustLookupTypeRecord([]super.Field{super.NewField(alias, val.Type())})
		b.Write(super.NewValue(typ, scode.Append(nil, val.Bytes())))
	}
	s.pending = append(s.pending, NewSideErrors(s.sctx, "join: key error", b.Build(s.sctx), vector.Pick(keyVec, slots)))
	set := make(map[uint32]struct{}, len(slots))
	for _, slot := range slots {
		set[slot] = struct{}{}
	}
	return func(slot uint32) bool {
		_, ok := set[slot]
		return ok
	}
}

func (s *sideErrs) next() vector.Any {
	if s == nil || len(s.pending) == 0 {
		return nil
	}
	vec := s.pending[0]
	s.pending = s.pending[1:]
	return vec
}

func NewHashJoin(rctx *runtime.Context, style string, left, right vio.Puller,
	leftKey, rightKey expr.Evaluator, leftAlias, rightAlias string, catch bool) *HashJoin {
	if style == "right" {
		leftKey, rightKey = rightKey, leftKey
		left, right = right, left
	}
	if style == "cross" {
		panic("cross join not compatible with hash join")
	}
	h := &HashJoin{
		rctx:       rctx,
		style:      style,
		left:       left,
		right:      right,
		leftKey:    leftKey,
		rightKey:   rightKey,
		leftAlias:  leftAlias,
		rightAlias: rightAlias,
	}
	if catch {
		h.sideErrs = &sideErrs{sctx: rctx.Sctx}
	}
	return h
}

// aliases returns the aliases of h.left and h.right, which are swapped for a
// right join.
func (h *HashJoin) aliases() (string, string) {
	if h.style == "right" {
		return h.rightAlias, h.leftAlias
	}
	return h.leftAlias, h.rightAlias
}

func (h *HashJoin) Pull(done bool) (vector.Any, error) {
	if done {
		_, err := h.left.Pull(true)
		if err == nil {
			_, err = h.right.Pull(true)
		}
		h.hashJoin = nil
		h.eos = false
		if h.sideErrs != nil {
			h.sideErrs.pending = nil
		}
		return nil, err
	}
	if vec := h.sideErrs.next(); vec != nil {
		return vec, nil
	}
	if h.eos {
		h.eos = false
		return nil, nil
	}
	if h.hashJoin == nil {
		if err := h.tableInit(); err != nil {
			return nil, err
		}
		if vec := h.sideErrs.next(); vec != nil {
			return vec, nil
		}
	}
	vec, err := h.hashJoin.Pull()
	if vec == nil || err != nil {
		h.hashJoin = nil
		if err == nil {
			// Output any side errors of the last vectors probed
			// before EOS.
			if vec := h.sideErrs.next(); vec != nil {
				h.eos = true
				return vec, nil
			}
		}
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
	var errVals []super.Value
	var left, right vio.Puller
	leftAlias, rightAlias := h.aliases()
	if rightBuf.EOS {
		table, _ = buildTable(rightBuf.vecs, h.rightKey, h.sideErrs, rightAlias)
		left = leftBuf
	} else {
		table, errVals = buildTable(leftBuf.vecs, h.leftKey, h.sideErrs, leftAlias)
		right = rightBuf
	}
	h.hashJoin = &hashJoin{
		sctx:       h.rctx.Sctx,
		style:      h.style,
		table:      table,
		errVals:    errVals,
		left:       left,
		right:      right,
		leftAlias:  h.leftAlias,
		rightAlias: h.rightAlias,
		leftKey:    h.leftKey,
		rightKey:   h.rightKey,
		hits:       make(map[string]bool),
		sideErrs:   h.sideErrs,
	}
	h.hashJoin.keyAliases[0], h.hashJoin.keyAliases[1] = leftAlias, rightAlias
	return nil
}

// buildTable returns a hash table of vecs by key and the values whose key has an
// error in it, which match nothing, if sideErrs is non-nil.
func buildTable(vecs []vector.Any, key expr.Evaluator, sideErrs *sideErrs, alias string) (map[string][]super.Value, []super.Value) {
	var n int
	for _, vec := range vecs {
		n += int(vec.Len())
	}
	table := make(map[string][]super.Value, n)
	var errVals []super.Value
	var sb scode.Builder
	for _, vec := range vecs {
		rightKeyVec := key.Eval(vec)
		isErr := sideErrs.keyErrors(vec, rightKeyVec, alias)
		for i := range vec.Len() {
			keyVal := vector.ValueAt(&sb, rightKeyVec, i)
			if keyVal.IsMissing() {
				continue
			}
			if isErr(i) {
				errVals = append(errVals, vector.ValueAt(&sb, vec, i).Copy())
				continue
			}
			key := hashKey(keyVal)
			table[key] = append(table[key], vector.ValueAt(&sb, vec, i).Copy())
		}
	}
	return table, errVals
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
	sctx  *super.Context
	style string
	table map[string][]super.Value
	// errVals are the values of the table's side whose key has an error in
	// it, which match nothing.
	errVals    []super.Value
	left       vio.Puller
	right      vio.Puller
	leftAlias  string
	rightAlias string
	leftKey    expr.Evaluator
	rightKey   expr.Evaluator
	builder    scode.Builder
	sideErrs   *sideErrs
	// keyAliases are the aliases of left and right for side errors.
	keyAliases [2]string

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
		isErr := j.sideErrs.keyErrors(vec, leftKeyVec, j.keyAliases[0])
		b := vector.NewDynamicValueBuilder()
		for i := range vec.Len() {
			keyVal := vector.ValueAt(&sb, leftKeyVec, i)
			if keyVal.IsMissing() {
				continue
			}
			var rightVals []super.Value
			var ok bool
			// A key with an error matches nothing.
			if !keyVal.IsNull() && !isErr(i) {
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
		if vec := j.sideErrs.next(); vec != nil {
			return vec, nil
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
		isErr := j.sideErrs.keyErrors(vec, rightKeyVec, j.keyAliases[1])
		b := vector.NewDynamicValueBuilder()
		for i := range vec.Len() {
			keyVal := vector.ValueAt(&sb, rightKeyVec, i)
			if keyVal.IsMissing() || isErr(i) {
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
		if vec := j.sideErrs.next(); vec != nil {
			return vec, nil
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
	for _, val := range j.errVals {
		b.Write(j.wrap(val.Ptr(), nil))
	}
	j.hits = nil
	j.errVals = nil
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
