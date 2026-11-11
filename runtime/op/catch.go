package op

import (
	"context"

	"github.com/superdb/super"
	"github.com/superdb/super/runtime/expr"
	"github.com/superdb/super/runtime/expr/function"
	"github.com/superdb/super/scode"
	"github.com/superdb/super/vector"
	"github.com/superdb/super/vector/vio"
)

// SideErrors marks, in the output of an operator with a catch handler, the
// operator's side errors: the errors of an expression that the operator
// consumes rather than outputs (e.g., a where predicate), which are dropped if
// there's no handler.  The Catch that follows the operator routes them to the
// handler, so a SideErrors is never output by a query.
type SideErrors struct {
	vector.Any
}

// NewSideErrors returns side errors with message msg for the values of on,
// whose expression produced the errors of errs:
//
//	error({message:msg,on:<value of on>,error:<value of errs>})
func NewSideErrors(sctx *super.Context, msg string, on, errs vector.Any) *SideErrors {
	// Side errors are rare so they're built value by value, which gives
	// each the types of its own values.
	var onb, errb, b scode.Builder
	msgVal := super.NewString(msg)
	out := vector.NewDynamicValueBuilder()
	for slot := range on.Len() {
		onVal := vector.ValueAt(&onb, on, slot).Deunion()
		errVal := vector.ValueAt(&errb, errs, slot).Deunion()
		typ := sctx.MustLookupTypeRecord([]super.Field{
			super.NewField("message", msgVal.Type()),
			super.NewField("on", onVal.Type()),
			super.NewField("error", errVal.Type()),
		})
		b.Truncate()
		b.Append(msgVal.Bytes())
		b.Append(onVal.Bytes())
		b.Append(errVal.Bytes())
		out.Write(super.NewValue(sctx.LookupTypeError(typ), b.Bytes()))
	}
	return &SideErrors{out.Build(sctx)}
}

// Catch splits the output of its parent into the values that pass through
// and the values that are caught, which are sent to a catch handler.
type Catch struct {
	router *router
	split  func(vector.Any) (vector.Any, vector.Any)
	pass   *route
	caught *route
}

// NewCatch returns a Catch whose split function returns the values of a vector
// that pass and the values that are caught.  Either may be nil.
func NewCatch(ctx context.Context, parent vio.Puller, split func(vector.Any) (vector.Any, vector.Any)) *Catch {
	c := &Catch{split: split}
	c.router = newRouter(ctx, c, parent)
	c.pass = c.router.addRoute()
	c.caught = c.router.addRoute()
	return c
}

// Pass returns the puller for the values that pass through.
func (c *Catch) Pass() vio.Puller {
	return c.pass
}

// Caught returns the puller for the values that are caught.
func (c *Catch) Caught() vio.Puller {
	return c.caught
}

func (c *Catch) forward(vec vector.Any) bool {
	pass, caught := c.split(vec)
	if pass != nil && pass.Len() != 0 && !c.pass.send(pass, nil) {
		return false
	}
	if caught != nil && caught.Len() != 0 && !c.caught.send(caught, nil) {
		return false
	}
	return true
}

// SplitSideErrors is the split function of a Catch for an operator with a
// catch handler, which catches the operator's side errors.
func SplitSideErrors(vec vector.Any) (vector.Any, vector.Any) {
	if s, ok := vec.(*SideErrors); ok {
		return nil, s.Any
	}
	return vec, nil
}

// NewHasErrorSplit returns the split function of a Catch for the catch
// operator, which catches the values with an error in them (i.e., for which
// has_error(this) is true).
func NewHasErrorSplit(sctx *super.Context) func(vector.Any) (vector.Any, vector.Any) {
	hasError := expr.NewCall(sctx, function.NewHasError(sctx), []expr.Evaluator{&expr.This{}})
	return func(vec vector.Any) (vector.Any, vector.Any) {
		caught, _ := expr.BoolMask(hasError.Eval(vec))
		switch n := caught.GetCardinality(); {
		case n == 0:
			return vec, nil
		case n == uint64(vec.Len()):
			return nil, vec
		}
		pass := caught.Clone()
		pass.Flip(0, uint64(vec.Len()))
		return vector.Pick(vec, pass.ToArray()), vector.Pick(vec, caught.ToArray())
	}
}

// hasErrorSlots returns the slots of vec whose values have an error in them
// (i.e., for which has_error(this) is true).
func hasErrorSlots(sctx *super.Context, vec vector.Any) []uint32 {
	hasError := expr.NewCall(sctx, function.NewHasError(sctx), []expr.Evaluator{&expr.This{}})
	slots, _ := expr.BoolMask(hasError.Eval(vec))
	return slots.ToArray()
}

// Halt implements the halt operator, which sends the value of expr for its
// first input value to the query's outputs as a halt (see Outputs) and outputs
// nothing.  A plain none is skipped, as for the values operator.
type Halt struct {
	ctx     context.Context
	parent  vio.Puller
	expr    expr.Evaluator
	outputs *Outputs
	halted  bool
}

func NewHalt(ctx context.Context, parent vio.Puller, expr expr.Evaluator, outputs *Outputs) *Halt {
	return &Halt{ctx: ctx, parent: parent, expr: expr, outputs: outputs}
}

func (h *Halt) Pull(done bool) (vector.Any, error) {
	if done || h.halted {
		return h.parent.Pull(true)
	}
	for {
		vec, err := h.parent.Pull(false)
		if vec == nil || err != nil {
			return nil, err
		}
		vals := dropPlainNones(h.expr.Eval(vec))
		if vals == nil || vals.Len() == 0 {
			continue
		}
		h.halted = true
		if err := h.outputs.halt(h.ctx, vector.Pick(vals, []uint32{0})); err != nil {
			return nil, err
		}
		return nil, nil
	}
}
