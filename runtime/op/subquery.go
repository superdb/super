package op

import (
	"context"

	"github.com/superdb/super"
	"github.com/superdb/super/vector"
	"github.com/superdb/super/vector/vbuild"
	"github.com/superdb/super/vector/vio"
)

// Subquery is a subquery mechanism with an Eval method to act as an expression
// and a Pull method to act as the parent of the embedded query that implements
// the expression.  When Eval is called, it repeatedly constructs a single-slot
// vector from the current slot of the this vector, sends it to the embedded
// query, pulls from the query until EOS, appends the value to the result
// vector, and then advances to the next slot.  If the query produces no
// values, the result is null if sql is true (as in SQL) and otherwise none.
type Subquery struct {
	ctx    context.Context
	sctx   *super.Context
	sql    bool
	create func() *Subquery

	body vio.Puller
	ch   chan vector.Any
	eos  bool

	stack []*Subquery
	tos   int
}

func NewSubquery(ctx context.Context, sctx *super.Context, sql bool, create func() *Subquery) *Subquery {
	return &Subquery{
		ctx:    ctx,
		sctx:   sctx,
		sql:    sql,
		create: create,
		ch:     make(chan vector.Any, 1),
		tos:    -2,
	}
}

func (s *Subquery) SetBody(body vio.Puller) {
	s.body = body
}

func (s *Subquery) Eval(this vector.Any) vector.Any {
	s.tos++
	defer func() {
		s.tos--
	}()
	if s.tos >= 0 {
		// We're re-entering this subquery instance before it's done evaluating
		// the previous invocation.  This happens when a subquery is invoked
		// inside of a recursive function so the same instance ends up being
		// called by different call frames.  To deal with this, we keep a stack
		// of Subquery duplicates where each duplicate is not shared and extend
		// the stack as needed.  If the stack overflows, we return an error.
		if s.tos >= 10000 {
			return vector.NewWrappedError(s.sctx, "subquery recursion depth exceeded", this)
		}
		if s.tos >= len(s.stack) {
			s.stack = append(s.stack, s.create())
		}
		return s.stack[s.tos].Eval(this)
	}
	db := vbuild.NewDynamicBuilder()
	index := make([]uint32, this.Len())
	for i := range this.Len() {
		index[i] = i
		select {
		case s.ch <- vector.Pick(this, index[i:i+1]):
		case <-s.ctx.Done():
			msg := s.ctx.Err().Error()
			return vector.NewStringError(s.sctx, msg, this.Len())
		}
		vec, err := subqueryBodyPull(s.sctx, s.body, s.sql)
		if err != nil {
			vec = s.bodyErr(err)
		}
		db.Write(vec)
	}
	return db.Build()
}

// subqueryBodyPull pulls the result of one subquery evaluation from p.
// An error from p is returned as err so the caller can reset the body.
func subqueryBodyPull(sctx *super.Context, p vio.Puller, sql bool) (vector.Any, error) {
	vec, err := p.Pull(false)
	if err != nil {
		return nil, err
	}
	if vec == nil {
		if sql {
			return vector.NewNull(1), nil
		}
		return vector.NewNone(1), nil
	}
	vec2, err := p.Pull(false)
	if err != nil {
		return nil, err
	}
	if vec2 != nil {
		if _, err := p.Pull(true); err != nil {
			return nil, err
		}
	}
	if vec2 != nil || vec.Len() > 1 {
		return vector.NewStringError(sctx, "query expression produced multiple values (consider [subquery])", 1), nil
	}
	return vec, nil
}

// bodyErr returns err as an error value after resetting the body so that it
// is ready for the next value.  The body stopped at the error, so it may not
// have pulled this subquery to EOS and its operators may hold partial state.
func (s *Subquery) bodyErr(err error) vector.Any {
	s.body.Pull(true)
	s.eos = false
	select {
	case <-s.ch:
	default:
	}
	return vector.NewStringError(s.sctx, err.Error(), 1)
}

func (s *Subquery) Pull(done bool) (vector.Any, error) {
	if done {
		// The body is done with the current value whether or not it
		// pulled it, so don't block waiting for another.
		s.eos = false
		return nil, nil
	}
	if s.eos {
		s.eos = false
		return nil, nil
	}
	s.eos = true
	select {
	case vec := <-s.ch:
		return vec, nil
	case <-s.ctx.Done():
		return nil, s.ctx.Err()
	}
}
