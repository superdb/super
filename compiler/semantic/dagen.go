package semantic

import (
	"errors"
	"slices"
	"strings"

	"github.com/superdb/super/compiler/ast"
	"github.com/superdb/super/compiler/dag"
	"github.com/superdb/super/compiler/semantic/sem"
	"github.com/superdb/super/pkg/field"
	"github.com/superdb/super/sup"
)

type dagen struct {
	reporter
	funcs   map[string]*dag.FuncDef
	outputs map[*dag.OutputOp]ast.Node
}

func newDagen(r reporter) *dagen {
	return &dagen{
		reporter: r,
		funcs:    make(map[string]*dag.FuncDef),
		outputs:  make(map[*dag.OutputOp]ast.Node),
	}
}

func (d *dagen) assemble(seq sem.Seq, types []byte, funcs map[string]*funcDef) *dag.Main {
	dagSeq := d.seq(seq)
	dagSeq = d.checkOutputs(true, dagSeq)
	dagFuncs := make([]*dag.FuncDef, 0, len(d.funcs))
	for _, f := range funcs {
		dagFuncs = append(dagFuncs, d.fn(f))
	}
	// Sort function entries so they are consistently ordered by integer tag strings.
	slices.SortFunc(dagFuncs, func(a, b *dag.FuncDef) int {
		return strings.Compare(a.Tag, b.Tag)
	})
	return &dag.Main{Types: types, Funcs: dagFuncs, Body: dagSeq}
}

func (d *dagen) assembleExpr(e sem.Expr, types []byte, funcs map[string]*funcDef) *dag.MainExpr {
	dagExpr := d.expr(e)
	dagFuncs := make([]*dag.FuncDef, 0, len(d.funcs))
	for _, f := range funcs {
		dagFuncs = append(dagFuncs, d.fn(f))
	}
	return &dag.MainExpr{Types: types, Funcs: dagFuncs, Expr: dagExpr}
}

func (d *dagen) seq(seq sem.Seq) dag.Seq {
	var out dag.Seq
	for _, op := range seq {
		out = append(out, d.op(op))
	}
	return out
}

func (d *dagen) op(op sem.Op) dag.Op {
	switch op := op.(type) {
	//
	/// Scanners in alphabetical order
	//
	case *sem.CommitMetaScan:
		return &dag.CommitMetaScan{
			Kind:   "CommitMetaScan",
			Table:  op.Table,
			Commit: op.Commit,
			Meta:   op.Meta,
			Tap:    op.Tap,
		}
	case *sem.DBMetaScan:
		return &dag.DBMetaScan{
			Kind: "DBMetaScan",
			Meta: op.Meta,
		}
	case *sem.DeleteScan:
		return &dag.DeleteScan{
			Kind:   "DeleteScan",
			ID:     op.ID,
			Commit: op.Commit,
		}
	case *sem.FileScan:
		var typ string
		if !isUnknown(op.Type) {
			typ = sup.FormatType(op.Type)
		}
		return &dag.FileScan{
			Kind:    "FileScan",
			Paths:   op.Paths,
			Formats: op.Formats,
			Type:    typ,
		}
	case *sem.HTTPScan:
		var typ string
		if op.Type != nil && !isUnknown(op.Type) {
			typ = sup.FormatType(op.Type)
		}
		return &dag.HTTPScan{
			Kind:    "HTTPScan",
			URL:     op.URL,
			Format:  op.Format,
			Method:  op.Method,
			Headers: op.Headers,
			Body:    op.Body,
			Type:    typ,
		}
	case *sem.NullScan:
		return &dag.NullScan{
			Kind: "NullScan",
		}
	case *sem.RobotScan:
		return &dag.RobotScan{
			Kind:   "RobotScan",
			Expr:   d.expr(op.Expr),
			Format: op.Format,
		}
	case *sem.TableMetaScan:
		return &dag.TableMetaScan{
			Kind: "TableMetaScan",
			ID:   op.ID,
			Meta: op.Meta,
		}
	case *sem.TableScan:
		var typ string
		if op.Type != nil && !isUnknown(op.Type) {
			typ = sup.FormatType(op.Type)
		}
		return &dag.TableScan{
			Kind:   "TableScan",
			ID:     op.ID,
			Commit: op.Commit,
			Type:   typ,
		}

	//
	// Ops in alphabetical order
	//
	case *sem.AggregateOp:
		return &dag.AggregateOp{
			Kind:  "AggregateOp",
			Limit: op.Limit,
			Keys:  d.assignments(op.Keys),
			Aggs:  d.aggAssignments(op.Aggs),
		}
	case *sem.CountOp:
		var e dag.Expr
		if op.Expr != nil {
			e = d.expr(op.Expr)
		}
		return &dag.CountOp{
			Kind:  "CountOp",
			Alias: op.Alias,
			Expr:  e,
		}
	case *sem.CutOp:
		rec, err := dag.NewRecordExprFromAssignments(d.assignments(op.Args))
		if err != nil {
			panic(err)
		}
		return &dag.ValuesOp{
			Kind:  "ValuesOp",
			Exprs: []dag.Expr{rec},
		}
	case *sem.DebugOp:
		return &dag.DebugOp{
			Kind:   "DebugOp",
			Expr:   d.expr(op.Expr),
			Filter: d.expr(op.Filter),
		}
	case *sem.DistinctOp:
		return &dag.DistinctOp{
			Kind: "DistinctOp",
			Expr: d.expr(op.Expr),
		}
	case *sem.DropOp:
		return &dag.DropOp{
			Kind: "DropOp",
			Args: d.exprs(op.Args),
		}
	case *sem.FilterOp:
		return &dag.FilterOp{
			Kind: "FilterOp",
			Expr: d.expr(op.Expr),
		}
	case *sem.ForkOp:
		return &dag.ForkOp{
			Kind:  "ForkOp",
			Paths: d.paths(op.Paths),
		}
	case *sem.FuseOp:
		return &dag.FuseOp{
			Kind:     "FuseOp",
			Complete: op.Complete,
		}
	case *sem.HeadOp:
		return &dag.HeadOp{
			Kind:  "HeadOp",
			Count: op.Count,
		}
	case *sem.InferOp:
		return &dag.InferOp{
			Kind:  "InferOp",
			Limit: op.Limit,
		}
	case *sem.JoinOp:
		return &dag.JoinOp{
			Kind:       "JoinOp",
			Style:      op.Style,
			LeftAlias:  op.LeftAlias,
			RightAlias: op.RightAlias,
			Cond:       d.expr(op.Cond),
		}
	case *sem.LoadOp:
		return &dag.LoadOp{
			Kind:    "LoadOp",
			Table:   op.Table,
			Branch:  op.Branch,
			Author:  op.Author,
			Message: op.Message,
			Meta:    op.Meta,
		}
	case *sem.MergeOp:
		if len(op.Exprs) == 0 {
			return &dag.CombineOp{Kind: "CombineOp"}
		}
		return &dag.MergeOp{
			Kind:  "MergeOp",
			Exprs: d.sortExprs(op.Exprs),
		}
	case *sem.OutputOp:
		o := &dag.OutputOp{
			Kind: "OutputOp",
			Name: op.Name,
		}
		d.outputs[o] = op
		return o
	case *sem.PassOp:
		return &dag.PassOp{
			Kind: "PassOp",
		}
	case *sem.PutOp:
		return &dag.PutOp{
			Kind: "PutOp",
			Args: d.assignments(op.Args),
		}
	case *sem.RenameOp:
		return &dag.RenameOp{
			Kind: "RenameOp",
			Args: d.assignments(op.Args),
		}
	case *sem.SkipOp:
		return &dag.SkipOp{
			Kind:  "SkipOp",
			Count: op.Count,
		}
	case *sem.SortOp:
		return &dag.SortOp{
			Kind:    "SortOp",
			Exprs:   d.sortExprs(op.Exprs),
			Reverse: op.Reverse,
		}
	case *sem.SwitchOp:
		return &dag.SwitchOp{
			Kind:  "SwitchOp",
			Expr:  d.expr(op.Expr),
			Cases: d.cases(op.Cases),
		}
	case *sem.TailOp:
		return &dag.TailOp{
			Kind:  "TailOp",
			Count: op.Count,
		}
	case *sem.TopOp:
		return &dag.TopOp{
			Kind:    "TopOp",
			Limit:   op.Limit,
			Exprs:   d.sortExprs(op.Exprs),
			Reverse: op.Reverse,
		}
	case *sem.UniqOp:
		return &dag.UniqOp{
			Kind:  "UniqOp",
			Cflag: op.Cflag,
		}
	case *sem.UnnestOp:
		return d.unnest(op)
	case *sem.ValuesOp:
		return &dag.ValuesOp{
			Kind:  "ValuesOp",
			Exprs: d.exprs(op.Exprs),
		}
	}
	panic(op)
}

func (d *dagen) unnest(op *sem.UnnestOp) dag.Op {
	u := &dag.UnnestOp{
		Kind: "UnnestOp",
		Expr: d.expr(op.Expr),
	}
	if op.Body == nil {
		return u
	}
	body := d.seq(op.Body)
	body.Prepend(u)
	body = collectThis(body)
	s := &dag.SubqueryExpr{
		Kind:       "SubqueryExpr",
		Correlated: true,
		Body:       body,
	}
	return &dag.UnnestOp{Kind: "UnnestOp", Expr: s}
}

func (d *dagen) paths(paths []sem.Seq) []dag.Seq {
	var out []dag.Seq
	for _, path := range paths {
		out = append(out, d.seq(path))
	}
	return out
}

func (d *dagen) cases(cases []sem.Case) []dag.Case {
	var out []dag.Case
	for _, c := range cases {
		out = append(out, dag.Case{Expr: d.expr(c.Expr), Path: d.seq(c.Path)})
	}
	return out
}

func (d *dagen) assignments(assignments []sem.Assignment) []dag.Assignment {
	var out []dag.Assignment
	for _, e := range assignments {
		out = append(out, dag.Assignment{
			Kind: "Assignment",
			LHS:  d.expr(e.LHS),
			RHS:  d.expr(e.RHS),
		})
	}
	return out
}

func (d *dagen) aggAssignments(assignments []sem.Assignment) []dag.Assignment {
	var out []dag.Assignment
	for _, e := range assignments {
		out = append(out, dag.Assignment{
			Kind: "Assignment",
			LHS:  d.expr(e.LHS),
			RHS:  d.aggExpr(e.RHS),
		})
	}
	return out
}

func (d *dagen) aggExpr(e sem.Expr) dag.Expr {
	agg, ok := e.(*sem.AggFunc)
	if !ok {
		return &dag.BadExpr{Kind: "BadExpr"}
	}
	return &dag.AggExpr{
		Kind:     "AggExpr",
		Name:     agg.Name,
		Distinct: agg.Distinct,
		Expr:     d.expr(agg.Expr),
		Filter:   d.expr(agg.Filter),
	}
}

func (d *dagen) sortExprs(exprs []sem.SortExpr) []dag.SortExpr {
	var out []dag.SortExpr
	for _, e := range exprs {
		sortExpr := dag.SortExpr{
			Key:   d.expr(e.Expr),
			Order: e.Order,
			Nulls: e.Nulls,
		}
		out = append(out, sortExpr)
	}
	return out
}

func (d *dagen) exprs(exprs []sem.Expr) []dag.Expr {
	var out []dag.Expr
	for _, e := range exprs {
		out = append(out, d.expr(e))
	}
	return out
}

func (d *dagen) expr(e sem.Expr) dag.Expr {
	switch e := e.(type) {
	case nil:
		return nil
	case *sem.AggFunc:
		d.error(e, errors.New("call to aggregate function in non-aggregate context"))
		return &dag.BadExpr{Kind: "BadExpr"}
	case *sem.ArrayExpr:
		return &dag.ArrayExpr{
			Kind:  "ArrayExpr",
			Elems: d.arrayElems(e.Elems),
		}
	case *sem.BinaryExpr:
		return &dag.BinaryExpr{
			Kind: "BinaryExpr",
			Op:   e.Op,
			LHS:  d.expr(e.LHS),
			RHS:  d.expr(e.RHS),
		}
	case *sem.CallExpr:
		return d.call(e)
	case *sem.CondExpr:
		return &dag.CondExpr{
			Kind: "CondExpr",
			Cond: d.expr(e.Cond),
			Then: d.expr(e.Then),
			Else: d.expr(e.Else),
		}
	case *sem.DotExpr:
		return &dag.DotExpr{
			Kind:    "DotExpr",
			LHS:     d.expr(e.LHS),
			RHS:     e.RHS,
			Noneish: e.Noneish,
			Nullish: e.Nullish,
		}
	case *sem.IndexExpr:
		return &dag.IndexExpr{
			Kind:  "IndexExpr",
			Expr:  d.expr(e.Expr),
			Index: d.expr(e.Index),
			Base1: e.Base1,
		}
	case *sem.IsNullExpr:
		return &dag.IsNullExpr{
			Kind: "IsNullExpr",
			Expr: d.expr(e.Expr),
		}
	case *sem.PrimitiveExpr:
		return &dag.PrimitiveExpr{
			Kind:  "PrimitiveExpr",
			Value: e.Value,
		}
	case *sem.MapCallExpr:
		return &dag.MapCallExpr{
			Kind:   "MapCallExpr",
			Expr:   d.expr(e.Expr),
			Lambda: d.call(e.Lambda),
		}
	case *sem.MapExpr:
		return &dag.MapExpr{
			Kind:    "MapExpr",
			Entries: d.entries(e.Entries),
		}
	case *sem.RecordExpr:
		return &dag.RecordExpr{
			Kind:  "RecordExpr",
			Elems: d.recordElems(e.Elems),
		}
	case *sem.RegexpMatchExpr:
		return &dag.RegexpMatchExpr{
			Kind:    "RegexpMatchExpr",
			Pattern: e.Pattern,
			Expr:    d.expr(e.Expr),
		}
	case *sem.RegexpSearchExpr:
		return &dag.RegexpSearchExpr{
			Kind:    "RegexpSearchExpr",
			Pattern: e.Pattern,
			Expr:    d.expr(e.Expr),
		}
	case *sem.SearchTermExpr:
		return &dag.SearchExpr{
			Kind:  "SearchExpr",
			Text:  e.Text,
			Value: e.Value,
			Expr:  d.expr(e.Expr),
		}
	case *sem.SetExpr:
		return &dag.SetExpr{
			Kind:  "SetExpr",
			Elems: d.arrayElems(e.Elems),
		}
	case *sem.SliceExpr:
		return &dag.SliceExpr{
			Kind:  "SliceExpr",
			Expr:  d.expr(e.Expr),
			From:  d.expr(e.From),
			To:    d.expr(e.To),
			Base1: e.Base1,
		}
	case *sem.SubqueryExpr:
		return d.subquery(e)
	case *sem.ThisExpr:
		return &dag.ThisExpr{
			Kind:  "ThisExpr",
			Chain: e.Chain,
		}
	case *sem.TypeExpr:
		return &dag.TypeExpr{
			Kind: "TypeExpr",
			ID:   e.ID,
		}
	case *sem.UnaryExpr:
		return &dag.UnaryExpr{
			Kind:    "UnaryExpr",
			Op:      e.Op,
			Operand: d.expr(e.Operand),
		}
	}
	panic(e)
}

func (d *dagen) arrayElems(elems []sem.ArrayElem) []dag.VectorElem {
	var out []dag.VectorElem
	for _, elem := range elems {
		switch elem := elem.(type) {
		case *sem.SpreadElem:
			out = append(out, d.spread(elem.Expr))
		case *sem.ExprElem:
			out = append(out, &dag.VectorValue{Kind: "VectorValue", Expr: d.expr(elem.Expr)})
		default:
			panic(elem)
		}
	}
	return out
}

func (d *dagen) recordElems(elems []sem.RecordElem) []dag.RecordElem {
	var out []dag.RecordElem
	for _, elem := range elems {
		switch elem := elem.(type) {
		case *sem.SpreadElem:
			out = append(out, d.spread(elem.Expr))
		case *sem.FieldElem:
			out = append(out, &dag.Field{Kind: "Field", Name: elem.Name, Value: d.expr(elem.Value)})
		default:
			panic(elem)
		}
	}
	return out
}

func (d *dagen) spread(e sem.Expr) *dag.Spread {
	return &dag.Spread{
		Kind: "Spread",
		Expr: d.expr(e),
	}
}

func (d *dagen) entries(entries []sem.Entry) []dag.Entry {
	var out []dag.Entry
	for _, entry := range entries {
		out = append(out, dag.Entry{Key: d.expr(entry.Key), Value: d.expr(entry.Value)})
	}
	return out
}

func (d *dagen) subquery(e *sem.SubqueryExpr) *dag.SubqueryExpr {
	subquery := &dag.SubqueryExpr{
		Kind:       "SubqueryExpr",
		Correlated: e.Correlated,
		Body:       d.seq(e.Body),
	}
	if e.Array {
		subquery.Body = collectThis(subquery.Body)
	}
	return subquery
}

func collectThis(seq dag.Seq) dag.Seq {
	collect := dag.Assignment{
		Kind: "Assignment",
		LHS:  dag.NewThis(field.NewChain("collect")),
		RHS:  &dag.AggExpr{Kind: "AggExpr", Name: "collect", Expr: dag.NewThis(nil)},
	}
	aggOp := &dag.AggregateOp{
		Kind: "AggregateOp",
		Aggs: []dag.Assignment{collect},
	}
	emitOp := &dag.ValuesOp{
		Kind:  "ValuesOp",
		Exprs: []dag.Expr{dag.NewThis(field.NewChain("collect"))},
	}
	seq = append(seq, aggOp)
	return append(seq, emitOp)
}

func (d *dagen) call(c *sem.CallExpr) *dag.CallExpr {
	return &dag.CallExpr{
		Kind: "CallExpr",
		Tag:  c.Tag,
		Args: d.exprs(c.Args),
	}
}

func (d *dagen) fn(f *funcDef) *dag.FuncDef {
	return &dag.FuncDef{
		Kind:   "FuncDef",
		Tag:    f.tag,
		Name:   f.name,
		Params: f.params,
		Expr:   d.expr(f.body),
	}
}

func (d *dagen) checkOutputs(isLeaf bool, seq dag.Seq) dag.Seq {
	if len(seq) == 0 {
		return seq
	}
	// - Report an error in any outputs are not located in the leaves.
	// - Add output operators to any leaves where they do not exist.
	lastN := len(seq) - 1
	for i, o := range seq {
		isLast := lastN == i
		switch o := o.(type) {
		case *dag.OutputOp:
			if !isLast || !isLeaf {
				d.error(d.outputs[o], errors.New("output operator must be a flowgraph leaf"))
			}
		case *dag.ScatterOp:
			for k := range o.Paths {
				o.Paths[k] = d.checkOutputs(isLast && isLeaf, o.Paths[k])
			}
		case *dag.ForkOp:
			for k := range o.Paths {
				o.Paths[k] = d.checkOutputs(isLast && isLeaf, o.Paths[k])
			}
		case *dag.SwitchOp:
			for k := range o.Cases {
				o.Cases[k].Path = d.checkOutputs(isLast && isLeaf, o.Cases[k].Path)
			}
		}
	}
	switch seq[lastN].(type) {
	case *dag.ForkOp, *dag.OutputOp, *dag.ScatterOp, *dag.SwitchOp:
	default:
		if isLeaf {
			return append(seq, &dag.OutputOp{Kind: "OutputOp", Name: "main"})
		}
	}
	return seq
}
