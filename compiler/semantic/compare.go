package semantic

import (
	"slices"

	"github.com/superdb/super/compiler/semantic/sem"
)

// eqSeq performs a deep-equal comparison of a and b
func eqSeq(a, b sem.Seq) bool {
	if len(a) != len(b) {
		return false
	}
	for k := range a {
		if !eqOp(a[k], b[k]) {
			return false
		}
	}
	return true
}

// eqOp performs a deep-equal comparison of ops aop and bop
func eqOp(aop, bop sem.Op) bool {
	switch a := aop.(type) {
	//
	// Scans in alphabetical order
	//
	case *sem.CommitMetaScan:
		b, ok := bop.(*sem.CommitMetaScan)
		if !ok {
			return false
		}
		return ok && a.Table == b.Table && a.Commit == b.Commit && a.Meta == b.Meta && a.Tap == b.Tap
	case *sem.DeleteScan:
		b, ok := bop.(*sem.DeleteScan)
		return ok && a.ID == b.ID && a.Commit == b.Commit
	case *sem.DBMetaScan:
		b, ok := bop.(*sem.DBMetaScan)
		return ok && a.Meta == b.Meta
	case *sem.FileScan:
		b, ok := bop.(*sem.FileScan)
		return ok && slices.Equal(a.Paths, b.Paths) && slices.Equal(a.Formats, b.Formats)
	case *sem.HTTPScan:
		b, ok := bop.(*sem.HTTPScan)
		return ok && a.URL == b.URL && a.Format == b.Format && a.Method == b.Method && a.Body == b.Body && eqHeaders(a.Headers, b.Headers)
	case *sem.NullScan:
		_, ok := bop.(*sem.NullScan)
		return ok
	case *sem.RobotScan:
		b, ok := bop.(*sem.RobotScan)
		return ok && a.Format == b.Format && eqExpr(a.Expr, b.Expr)
	case *sem.TableMetaScan:
		b, ok := bop.(*sem.TableMetaScan)
		return ok && a.ID == b.ID && a.Meta == b.Meta
	case *sem.TableScan:
		b, ok := bop.(*sem.TableScan)
		return ok && a.ID == b.ID && a.Commit == b.Commit
	//
	// Ops in alphabetical order
	//
	case *sem.AggregateOp:
		b, ok := bop.(*sem.AggregateOp)
		return ok && eqAssignments(a.Keys, b.Keys) && eqAssignments(a.Aggs, b.Aggs)
	case *sem.BadOp:
		return false
	case *sem.CutOp:
		b, ok := bop.(*sem.CutOp)
		return ok && eqAssignments(a.Args, b.Args)
	case *sem.DebugOp:
		b, ok := bop.(*sem.DebugOp)
		return ok && eqExpr(a.Expr, b.Expr)
	case *sem.DistinctOp:
		b, ok := bop.(*sem.DistinctOp)
		return ok && eqExpr(a.Expr, b.Expr)
	case *sem.DropOp:
		b, ok := bop.(*sem.DropOp)
		return ok && eqExprs(a.Args, b.Args)
	case *sem.FilterOp:
		b, ok := bop.(*sem.FilterOp)
		return ok && eqExpr(a.Expr, b.Expr)
	case *sem.ForkOp:
		b, ok := bop.(*sem.ForkOp)
		if !ok || len(a.Paths) != len(b.Paths) {
			return false
		}
		for k := range a.Paths {
			if !eqSeq(a.Paths[k], b.Paths[k]) {
				return false
			}
		}
		return true
	case *sem.FuseOp:
		_, ok := bop.(*sem.FuseOp)
		return ok
	case *sem.HeadOp:
		b, ok := bop.(*sem.HeadOp)
		return ok && a.Count == b.Count
	case *sem.JoinOp:
		b, ok := bop.(*sem.JoinOp)
		return ok && a.Style == b.Style && a.LeftAlias == b.LeftAlias && a.RightAlias == b.RightAlias && eqExpr(a.Cond, b.Cond)
	case *sem.LoadOp:
		b, ok := bop.(*sem.LoadOp)
		return ok && a.Table == b.Table && a.Branch == b.Branch && a.Author == b.Author && a.Message == b.Message && a.Meta == b.Meta
	case *sem.MergeOp:
		b, ok := bop.(*sem.MergeOp)
		return ok && eqSortExprs(a.Exprs, b.Exprs)
	case *sem.OutputOp:
		b, ok := bop.(*sem.OutputOp)
		return ok && a.Name == b.Name
	case *sem.PutOp:
		b, ok := bop.(*sem.PutOp)
		return ok && eqAssignments(a.Args, b.Args)
	case *sem.RenameOp:
		b, ok := bop.(*sem.RenameOp)
		return ok && eqAssignments(a.Args, b.Args)
	case *sem.SkipOp:
		b, ok := bop.(*sem.SkipOp)
		return ok && a.Count == b.Count
	case *sem.SortOp:
		b, ok := bop.(*sem.SortOp)
		return ok && a.Reverse == b.Reverse && eqSortExprs(a.Exprs, b.Exprs)
	case *sem.SwitchOp:
		b, ok := bop.(*sem.SwitchOp)
		if !ok || len(a.Cases) != len(b.Cases) || !eqExpr(a.Expr, b.Expr) {
			return false
		}
		for k := range a.Cases {
			if !eqExpr(a.Cases[k].Expr, b.Cases[k].Expr) {
				return false
			}
			if !eqSeq(a.Cases[k].Path, b.Cases[k].Path) {
				return false
			}
		}
		return true
	case *sem.TailOp:
		b, ok := bop.(*sem.TailOp)
		return ok && a.Count == b.Count
	case *sem.TopOp:
		b, ok := bop.(*sem.TopOp)
		return ok && a.Limit == b.Limit && eqSortExprs(a.Exprs, b.Exprs)
	case *sem.UniqOp:
		b, ok := bop.(*sem.UniqOp)
		return ok && a.Cflag == b.Cflag
	case *sem.UnnestOp:
		b, ok := bop.(*sem.UnnestOp)
		return ok && eqExpr(a.Expr, b.Expr) && eqSeq(a.Body, b.Body)
	case *sem.ValuesOp:
		b, ok := bop.(*sem.ValuesOp)
		return ok && eqExprs(a.Exprs, b.Exprs)
	default:
		panic(a)
	}
}

func eqAssignments(a, b []sem.Assignment) bool {
	if len(a) != len(b) {
		return false
	}
	for k := range a {
		if !eqExpr(a[k].LHS, b[k].LHS) {
			return false
		}
		if !eqExpr(a[k].RHS, b[k].RHS) {
			return false
		}
	}
	return true
}

func eqSortExprs(a, b []sem.SortExpr) bool {
	if len(a) != len(b) {
		return false
	}
	for k := range a {
		if a[k].Nulls != b[k].Nulls || a[k].Order != b[k].Order {
			return false
		}
		if !eqExpr(a[k].Expr, b[k].Expr) {
			return false
		}
	}
	return true
}

func eqExprs(a, b []sem.Expr) bool {
	if len(a) != len(b) {
		return false
	}
	for k := range a {
		if !eqExpr(a[k], b[k]) {
			return false
		}
	}
	return true
}

// eqOp performs a deep-equal comparison of expressions aexpr and bexpr
func eqExpr(aexpr, bexpr sem.Expr) bool {
	switch a := aexpr.(type) {
	case nil:
		return bexpr == nil
	case *sem.AggFunc:
		b, ok := bexpr.(*sem.AggFunc)
		return ok && a.Name == b.Name && a.Distinct == b.Distinct && eqExpr(a.Expr, b.Expr) && eqExpr(a.Filter, b.Filter)
	case *sem.AggRef:
		b, ok := bexpr.(*sem.AggRef)
		return ok && a.Index == b.Index
	case *sem.ArrayExpr:
		b, ok := bexpr.(*sem.ArrayExpr)
		return ok && eqArrayElems(a.Elems, b.Elems)
	case *sem.BadExpr:
		return false
	case *sem.BinaryExpr:
		b, ok := bexpr.(*sem.BinaryExpr)
		return ok && a.Op == b.Op && eqExpr(a.LHS, b.LHS) && eqExpr(a.RHS, b.RHS)
	case *sem.CallExpr:
		// XXX should calls with side-effects not be equal?
		b, ok := bexpr.(*sem.CallExpr)
		return ok && a.Tag == b.Tag && eqExprs(a.Args, b.Args)
	case *sem.CondExpr:
		b, ok := bexpr.(*sem.CondExpr)
		return ok && eqExpr(a.Cond, b.Cond) && eqExpr(a.Then, b.Then) && eqExpr(a.Else, b.Else)
	case *sem.DotExpr:
		b, ok := bexpr.(*sem.DotExpr)
		return ok && a.RHS == b.RHS && eqExpr(a.LHS, b.LHS)
	case *sem.IndexExpr:
		b, ok := bexpr.(*sem.IndexExpr)
		return ok && eqExpr(a.Expr, b.Expr) && eqExpr(a.Index, b.Index)
	case *sem.IsNullExpr:
		b, ok := bexpr.(*sem.IsNullExpr)
		return ok && eqExpr(a.Expr, b.Expr)
	case *sem.TypeAssertExpr:
		b, ok := bexpr.(*sem.TypeAssertExpr)
		return ok && eqExpr(a.Expr, b.Expr) && eqExpr(a.Type, b.Type)
	case *sem.MatchExpr:
		b, ok := bexpr.(*sem.MatchExpr)
		if !ok || !eqExpr(a.Expr, b.Expr) || !eqExpr(a.Else, b.Else) || len(a.Cases) != len(b.Cases) {
			return false
		}
		for k := range a.Cases {
			if !eqExpr(a.Cases[k].Type, b.Cases[k].Type) || !eqExpr(a.Cases[k].Expr, b.Cases[k].Expr) {
				return false
			}
		}
		return true
	case *sem.MapCallExpr:
		b, ok := bexpr.(*sem.MapCallExpr)
		return ok && eqExpr(a.Expr, b.Expr) && eqExpr(a.Lambda, b.Lambda)
	case *sem.MapExpr:
		b, ok := bexpr.(*sem.MapExpr)
		if !ok {
			return false
		}
		for k := range a.Entries {
			if !eqExpr(a.Entries[k].Key, b.Entries[k].Key) || !eqExpr(a.Entries[k].Value, b.Entries[k].Value) {
				return false
			}
		}
		return true
	case *sem.PrimitiveExpr:
		b, ok := bexpr.(*sem.PrimitiveExpr)
		return ok && a.Value == b.Value
	case *sem.RecordExpr:
		b, ok := bexpr.(*sem.RecordExpr)
		return ok && eqRecordElems(a.Elems, b.Elems)
	case *sem.RegexpMatchExpr:
		b, ok := bexpr.(*sem.RegexpMatchExpr)
		return ok && a.Pattern == b.Pattern && eqExpr(a.Expr, b.Expr)
	case *sem.RegexpSearchExpr:
		b, ok := bexpr.(*sem.RegexpSearchExpr)
		return ok && a.Pattern == b.Pattern && eqExpr(a.Expr, b.Expr)
	case *sem.SearchTermExpr:
		b, ok := bexpr.(*sem.SearchTermExpr)
		return ok && a.Text == b.Text && a.Value == b.Value && eqExpr(a.Expr, b.Expr)
	case *sem.SetExpr:
		b, ok := bexpr.(*sem.SetExpr)
		return ok && eqArrayElems(a.Elems, b.Elems)
	case *sem.SliceExpr:
		b, ok := bexpr.(*sem.SliceExpr)
		return ok && eqExpr(a.Expr, b.Expr) && eqExpr(a.From, b.From) && eqExpr(a.To, b.To)
	case *sem.SubqueryExpr:
		b, ok := bexpr.(*sem.SubqueryExpr)
		return ok && a.Correlated == b.Correlated && a.Array == b.Array && eqSeq(a.Body, b.Body)
	case *sem.ThisExpr:
		b, ok := bexpr.(*sem.ThisExpr)
		if !ok || len(a.Chain) != len(b.Chain) {
			return false
		}
		for k := range a.Chain {
			if a.Chain[k] != b.Chain[k] {
				return false
			}
		}
		return true
	case *sem.TypeExpr:
		b, ok := bexpr.(*sem.TypeExpr)
		return ok && a.ID == b.ID
	case *sem.UnaryExpr:
		b, ok := bexpr.(*sem.UnaryExpr)
		return ok && a.Op == b.Op && eqExpr(a.Operand, b.Operand)
	default:
		panic(aexpr)
	}
}

func eqArrayElems(a, b []sem.ArrayElem) bool {
	if len(a) != len(b) {
		return false
	}
	for k := range a {
		switch aelem := a[k].(type) {
		case *sem.SpreadElem:
			belem, ok := b[k].(*sem.SpreadElem)
			if !ok {
				return false
			}
			if !eqExpr(aelem.Expr, belem.Expr) {
				return false
			}
		case *sem.ExprElem:
			belem, ok := b[k].(*sem.ExprElem)
			if !ok {
				return false
			}
			if !eqExpr(aelem.Expr, belem.Expr) {
				return false
			}
		default:
			panic(aelem)
		}
	}
	return true
}

func eqRecordElems(a, b []sem.RecordElem) bool {
	if len(a) != len(b) {
		return false
	}
	for k := range a {
		switch aelem := a[k].(type) {
		case *sem.SpreadElem:
			belem, ok := b[k].(*sem.SpreadElem)
			if !ok {
				return false
			}
			if !eqExpr(aelem.Expr, belem.Expr) {
				return false
			}
		case *sem.FieldElem:
			belem, ok := b[k].(*sem.FieldElem)
			if !ok {
				return false
			}
			if aelem.Name != belem.Name || !eqExpr(aelem.Value, belem.Value) {
				return false
			}
		default:
			panic(aelem)
		}
	}
	return true
}

func eqHeaders(a, b map[string][]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, as := range a {
		bs, ok := b[k]
		if !ok {
			return false
		}
		if len(as) != len(bs) {
			return false
		}
		for i := range as {
			if as[i] != bs[i] {
				return false
			}
		}
	}
	return true
}
