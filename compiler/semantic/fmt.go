package semantic

import (
	"github.com/superdb/super/compiler/semantic/sem"
)

func Clear(seq sem.Seq, funcs map[string]*funcDef) {
	clrSeq(seq)
	for _, f := range funcs {
		clrExpr(f.body)
	}
}

func clrSeq(seq sem.Seq) {
	for _, op := range seq {
		clrOp(op)
	}
}

func clrOp(op sem.Op) {
	switch op := op.(type) {
	//
	// Scanners first
	//
	case *sem.FileScan:
		op.Node = nil
	case *sem.HTTPScan:
		op.Node = nil
	case *sem.TableScan:
		op.Node = nil
	case *sem.RobotScan:
		op.Node = nil
		clrExpr(op.Expr)
		clrExpr(op.Method)
		clrExpr(op.Headers)
		clrExpr(op.Body)
	case *sem.DBMetaScan:
		op.Node = nil
	case *sem.TableMetaScan:
		op.Node = nil
	case *sem.CommitMetaScan:
		op.Node = nil
	case *sem.DeleteScan:
		op.Node = nil
	case *sem.MaterializedScan:
		op.Node = nil
	case *sem.NullScan:
		op.Node = nil
	//
	// Ops in alphabetical oder
	//
	case *sem.AggregateOp:
		op.Node = nil
		clrAssignments(op.Keys)
		clrAssignments(op.Aggs)
	case *sem.BadOp:
	case *sem.CutOp:
		op.Node = nil
		clrAssignments(op.Args)
	case *sem.DebugOp:
		op.Node = nil
		clrExpr(op.Expr)
	case *sem.DistinctOp:
		op.Node = nil
		clrExpr(op.Expr)
	case *sem.DropOp:
		op.Node = nil
		clrExprs(op.Args)
	case *sem.FilterOp:
		op.Node = nil
		clrExpr(op.Expr)
	case *sem.ForkOp:
		op.Node = nil
		for _, seq := range op.Paths {
			clrSeq(seq)
		}
	case *sem.FuseOp:
		op.Node = nil
	case *sem.HeadOp:
		op.Node = nil
	case *sem.JoinOp:
		op.Node = nil
		clrExpr(op.Cond)
	case *sem.LoadOp:
		op.Node = nil
	case *sem.MergeOp:
		op.Node = nil
		clrSortExprs(op.Exprs)
	case *sem.OutputOp:
		op.Node = nil
	case *sem.PutOp:
		op.Node = nil
		clrAssignments(op.Args)
	case *sem.RenameOp:
		op.Node = nil
		clrAssignments(op.Args)
	case *sem.SkipOp:
		op.Node = nil
	case *sem.SortOp:
		op.Node = nil
		clrSortExprs(op.Exprs)
	case *sem.SwitchOp:
		op.Node = nil
		clrExpr(op.Expr)
		for _, c := range op.Cases {
			clrExpr(c.Expr)
			clrSeq(c.Path)
		}
	case *sem.TailOp:
		op.Node = nil
	case *sem.TopOp:
		op.Node = nil
		clrSortExprs(op.Exprs)
	case *sem.UniqOp:
		op.Node = nil
	case *sem.UnnestOp:
		op.Node = nil
		clrExpr(op.Expr)
		clrSeq(op.Body)
	case *sem.ValuesOp:
		op.Node = nil
		clrExprs(op.Exprs)
	default:
		panic(op)
	}
}

func clrAssignments(assignments []sem.Assignment) {
	for _, a := range assignments {
		clrExpr(a.LHS)
		clrExpr(a.RHS)
	}
}

func clrSortExprs(exprs []sem.SortExpr) {
	for _, se := range exprs {
		clrExpr(se.Expr)
	}
}

func clrExprs(exprs []sem.Expr) {
	for _, expr := range exprs {
		clrExpr(expr)
	}
}

func clrExpr(expr sem.Expr) {
	switch expr := expr.(type) {
	case nil:
	case *sem.AggFunc:
		expr.Node = nil
		clrExpr(expr.Expr)
		clrExpr(expr.Filter)
	case *sem.ArrayExpr:
		expr.Node = nil
		clrArrayElems(expr.Elems)
	case *sem.BadExpr:
		expr.Node = nil
	case *sem.BinaryExpr:
		expr.Node = nil
		clrExpr(expr.LHS)
		clrExpr(expr.RHS)
	case *sem.CallExpr:
		expr.Node = nil
		clrExprs(expr.Args)
	case *sem.CondExpr:
		expr.Node = nil
		clrExpr(expr.Cond)
		clrExpr(expr.Then)
		clrExpr(expr.Else)
	case *sem.DotExpr:
		expr.Node = nil
		clrExpr(expr.LHS)
	case *sem.IndexExpr:
		expr.Node = nil
		clrExpr(expr.Expr)
		clrExpr(expr.Index)
	case *sem.IsNullExpr:
		expr.Node = nil
		clrExpr(expr.Expr)
	case *sem.TypeAssertExpr:
		expr.Node = nil
		clrExpr(expr.Expr)
		clrExpr(expr.Type)
	case *sem.MatchExpr:
		expr.Node = nil
		clrExpr(expr.Expr)
		for _, c := range expr.Cases {
			clrExpr(c.Type)
			clrExpr(c.Input)
			clrExpr(c.Expr)
		}
		clrExpr(expr.ElseInput)
		clrExpr(expr.Else)
	case *sem.MapCallExpr:
		expr.Node = nil
		clrExpr(expr.Expr)
		clrExpr(expr.Lambda)
	case *sem.MapExpr:
		expr.Node = nil
		for _, entry := range expr.Entries {
			clrExpr(entry.Key)
			clrExpr(entry.Value)
		}
	case *sem.PrimitiveExpr:
		expr.Node = nil
	case *sem.RecordExpr:
		expr.Node = nil
		clrRecordElems(expr.Elems)
	case *sem.RegexpMatchExpr:
		expr.Node = nil
		clrExpr(expr.Expr)
	case *sem.RegexpSearchExpr:
		expr.Node = nil
		clrExpr(expr.Expr)
	case *sem.SearchTermExpr:
		expr.Node = nil
		clrExpr(expr.Expr)
	case *sem.SetExpr:
		expr.Node = nil
		clrArrayElems(expr.Elems)
	case *sem.SliceExpr:
		expr.Node = nil
		clrExpr(expr.Expr)
		clrExpr(expr.From)
		clrExpr(expr.To)
	case *sem.SubqueryExpr:
		expr.Node = nil
		clrSeq(expr.Body)
	case *sem.ThisExpr:
		expr.Node = nil
	case *sem.UnaryExpr:
		expr.Node = nil
		clrExpr(expr.Operand)
	default:
		panic(expr)
	}
}

func clrArrayElems(elems []sem.ArrayElem) {
	for _, elem := range elems {
		switch elem := elem.(type) {
		case *sem.SpreadElem:
			elem.Node = nil
			clrExpr(elem.Expr)
		case *sem.ExprElem:
			elem.Node = nil
			clrExpr(elem.Expr)
		default:
			panic(elem)
		}
	}
}

func clrRecordElems(elems []sem.RecordElem) {
	for _, elem := range elems {
		switch elem := elem.(type) {
		case *sem.SpreadElem:
			elem.Node = nil
			clrExpr(elem.Expr)
		case *sem.FieldElem:
			elem.Node = nil
			clrExpr(elem.Value)
		default:
			panic(elem)
		}
	}
}
