package semantic

import (
	"errors"

	"github.com/superdb/super"
	"github.com/superdb/super/compiler/ast"
	"github.com/superdb/super/compiler/rungen"
	"github.com/superdb/super/compiler/semantic/sem"
	"github.com/superdb/super/sup"
)

// evaluator provides a means to compile and run expressions in the runtime
// to determinte compile-time constants, which are return as super.Values
// in the passed in sctx.  Any types created during this process are not
// entered into the current query's typedefs table.
type evaluator struct {
	translator *translator
	in         map[string]*funcDef
	seen       map[string]bool
	errs       errlist
	constThis  bool
	bad        bool
	// readData allows the expression to read data (as with the query of
	// a type declaration).
	readData bool
}

type errloc struct {
	loc ast.Node
	err error
}

func newEvaluator(t *translator, funcs map[string]*funcDef) *evaluator {
	return &evaluator{
		translator: t,
		in:         funcs,
		seen:       make(map[string]bool),
	}
}

func (e *evaluator) mustEval(sctx *super.Context, expr sem.Expr) (super.Value, bool) {
	val, ok := e.maybeEval(sctx, expr)
	e.errs.flushErrs(e.translator.reporter)
	return val, ok
}

func (e *evaluator) maybeEval(sctx *super.Context, expr sem.Expr) (super.Value, bool) {
	if literal, ok := expr.(*sem.PrimitiveExpr); ok {
		val, err := sup.ParseValue(sctx, literal.Value)
		if err != nil {
			e.errs.error(literal.Node, err)
			return val, false
		}
		return val, true
	}
	e.expr(expr)
	if len(e.errs) > 0 || e.bad {
		return super.Value{}, false
	}
	main := newDagen(e.translator.reporter).assembleExpr(expr, e.translator.getTypes(), e.translator.resolver.funcs)
	var val super.Value
	var err error
	if e.readData {
		val, err = rungen.EvalAtCompileTimeWithEnv(e.translator.ctx, sctx, e.translator.env, main)
	} else {
		val, err = rungen.EvalAtCompileTime(sctx, main)
	}
	if err != nil {
		e.errs.error(expr, err)
		return val, false
	}
	return val, true
}

func (e *evaluator) seq(seq sem.Seq) bool {
	save := e.constThis
	for _, op := range seq {
		e.constThis = e.op(op)
	}
	constThis := e.constThis
	e.constThis = save
	return constThis
}

func (e *evaluator) op(op sem.Op) bool {
	switch op := op.(type) {
	//
	// Scanners first
	//
	case *sem.FileScan,
		*sem.HTTPScan,
		*sem.TableScan,
		*sem.RobotScan,
		*sem.DBMetaScan,
		*sem.TableMetaScan,
		*sem.CommitMetaScan,
		*sem.DeleteScan:
		if !e.readData {
			e.errs.error(op, errors.New("cannot read data in constant expression"))
		}
		return false
	case *sem.NullScan:
		return true
	//
	// Ops in alphabetical oder
	//
	case *sem.AggregateOp:
		return e.assignments(op.Keys) && e.assignments(op.Aggs)
	case *sem.BadOp:
		e.bad = true
		return false
	case *sem.CutOp:
		return e.assignments(op.Args)
	case *sem.DebugOp:
		return e.expr(op.Expr) && e.constThis
	case *sem.DistinctOp:
		return e.expr(op.Expr) && e.constThis
	case *sem.DropOp:
		return e.exprs(op.Args) && e.constThis
	case *sem.FilterOp:
		return e.expr(op.Expr) && e.constThis
	case *sem.ForkOp:
		isConst := true
		for _, seq := range op.Paths {
			if !e.seq(seq) {
				isConst = false
			}
		}
		return isConst
	case *sem.FuseOp:
		return e.constThis
	case *sem.HeadOp:
		return e.constThis
	case *sem.JoinOp:
		// This join depends on the parents but this is handled in the fork above.
		// If any path of parents are non-const, then constThis will be false on
		// entering here.
		return e.expr(op.Cond) && e.constThis
	case *sem.LoadOp:
		return true
	case *sem.MergeOp:
		// Like join, if any of the parent legs is non-const, the constThis if false here
		return e.sortExprs(op.Exprs) && e.constThis
	case *sem.OutputOp:
		return true
	case *sem.PutOp:
		return e.assignments(op.Args) && e.constThis
	case *sem.RenameOp:
		return e.assignments(op.Args) && e.constThis
	case *sem.SkipOp:
		return e.constThis
	case *sem.SortOp:
		return e.sortExprs(op.Exprs) && e.constThis
	case *sem.SwitchOp:
		e.constThis = e.expr(op.Expr)
		isConst := true
		for _, c := range op.Cases {
			if !e.expr(c.Expr) {
				isConst = false
			}
			if !e.seq(c.Path) {
				isConst = false
			}
		}
		return isConst
	case *sem.TailOp:
		return e.constThis
	case *sem.TopOp:
		return e.sortExprs(op.Exprs) && e.constThis
	case *sem.UniqOp:
		return e.constThis
	case *sem.UnnestOp:
		e.constThis = e.expr(op.Expr)
		return e.seq(op.Body)
	case *sem.ValuesOp:
		return e.exprs(op.Exprs)
	default:
		panic(op)
	}
}

func (e *evaluator) assignments(assignments []sem.Assignment) bool {
	isConst := true
	for _, a := range assignments {
		if !e.expr(a.LHS) { //XXX lval needs to be treated differently...
			isConst = false
		}
		if !e.expr(a.RHS) {
			isConst = false
		}
	}
	return isConst
}

func (e *evaluator) sortExprs(exprs []sem.SortExpr) bool {
	isConst := true
	for _, se := range exprs {
		if !e.expr(se.Expr) {
			isConst = false
		}
	}
	return isConst
}

func (e *evaluator) exprs(exprs []sem.Expr) bool {
	isConst := true
	for _, expr := range exprs {
		if !e.expr(expr) {
			isConst = false
		}
	}
	return isConst
}

func (e *evaluator) expr(expr sem.Expr) bool {
	switch expr := expr.(type) {
	case nil:
		return true
	case *sem.AggFunc:
		return e.expr(expr.Expr) && e.expr(expr.Filter)
	case *sem.ArrayExpr:
		return e.arrayElems(expr.Elems)
	case *sem.BadExpr:
		e.bad = true
		return false
	case *sem.BinaryExpr:
		return e.expr(expr.LHS) && e.expr(expr.RHS)
	case *sem.CallExpr:
		isConst := e.exprs(expr.Args)
		if f, ok := e.in[expr.Tag]; ok && !e.seen[expr.Tag] {
			e.seen[expr.Tag] = true
			save := e.constThis
			e.constThis = true
			e.expr(f.body)
			e.constThis = save
		}
		return isConst
	case *sem.CondExpr:
		return e.expr(expr.Cond) && e.expr(expr.Then) && e.expr(expr.Else)
	case *sem.DotExpr:
		return e.expr(expr.LHS)
	case *sem.IndexExpr:
		return e.expr(expr.Expr) && e.expr(expr.Index)
	case *sem.IsNullExpr:
		return e.expr(expr.Expr)
	case *sem.MapCallExpr:
		return e.expr(expr.Expr) && e.expr(expr.Lambda)
	case *sem.MapExpr:
		isConst := true
		for _, entry := range expr.Entries {
			if !e.expr(entry.Key) || !e.expr(entry.Value) {
				isConst = false
			}
		}
		return isConst
	case *sem.PrimitiveExpr, *sem.TypeExpr:
		return true
	case *sem.RecordExpr:
		return e.recordElems(expr.Elems)
	case *sem.RegexpMatchExpr:
		return e.expr(expr.Expr)
	case *sem.RegexpSearchExpr:
		return e.expr(expr.Expr)
	case *sem.SearchTermExpr:
		return e.expr(expr.Expr)
	case *sem.SetExpr:
		return e.arrayElems(expr.Elems)
	case *sem.SliceExpr:
		return e.expr(expr.Expr) && e.expr(expr.From) && e.expr(expr.To)
	case *sem.SubqueryExpr:
		//XXX fix this
		return e.seq(expr.Body)
	case *sem.ThisExpr:
		if !e.constThis {
			// XXX The type checker now reports no such field for non constant
			// exressions so we shouldn't need the constant analysis in this
			// module anymore.  We will verify this and remove this code in
			// a future PR (we still need the evaluator for now).
			//e.errs.error(expr, fmt.Errorf("cannot reference '%s' in constant expression", quotedPath(expr.Path)))
		}
		return e.constThis
	case *sem.UnaryExpr:
		return e.expr(expr.Operand)
	default:
		panic(e)
	}
}

func (e *evaluator) arrayElems(elems []sem.ArrayElem) bool {
	isConst := true
	for _, elem := range elems {
		switch elem := elem.(type) {
		case *sem.SpreadElem:
			if !e.expr(elem.Expr) {
				isConst = false
			}
		case *sem.ExprElem:
			if !e.expr(elem.Expr) {
				isConst = false
			}
		default:
			panic(elem)
		}
	}
	return isConst
}

func (e *evaluator) recordElems(elems []sem.RecordElem) bool {
	isConst := true
	for _, elem := range elems {
		switch elem := elem.(type) {
		case *sem.SpreadElem:
			if !e.expr(elem.Expr) {
				isConst = false
			}
		case *sem.FieldElem:
			if !e.expr(elem.Value) {
				isConst = false
			}
		default:
			panic(elem)
		}
	}
	return isConst
}

type errlist []errloc

func (e *errlist) error(loc ast.Node, err error) {
	*e = append(*e, errloc{loc, err})
}

func (e errlist) flushErrs(r reporter) {
	for _, info := range e {
		r.error(info.loc, info.err)
	}
}
