package rungen

import (
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/superdb/super"
	"github.com/superdb/super/compiler/dag"
	"github.com/superdb/super/pkg/field"
	"github.com/superdb/super/runtime/expr"
	"github.com/superdb/super/runtime/expr/agg"
	"github.com/superdb/super/runtime/op"
	"github.com/superdb/super/runtime/op/aggregate"
	samexpr "github.com/superdb/super/runtime/sam/expr"
	"github.com/superdb/super/sbuf"
	"github.com/superdb/super/vector"
	"github.com/superdb/super/vector/vio"
)

// compile compiles a DAG into a graph of runtime operators, and returns
// the leaves.
func (b *Builder) compileVam(o dag.Op, parents []vio.Puller) ([]vio.Puller, error) {
	switch o := o.(type) {
	case *dag.CombineOp:
		return []vio.Puller{b.combineVam(parents)}, nil
	case *dag.ForkOp:
		return b.compileVamFork(o, b.combineVam(parents))
	case *dag.HashJoinOp:
		if len(parents) != 2 {
			return nil, ErrJoinParents
		}
		leftKey, err := b.compileVamExpr(o.LeftKey)
		if err != nil {
			return nil, err
		}
		rightKey, err := b.compileVamExpr(o.RightKey)
		if err != nil {
			return nil, err
		}
		join := op.NewHashJoin(b.rctx, o.Style, parents[0], parents[1], leftKey, rightKey, o.LeftAlias, o.RightAlias)
		return []vio.Puller{join}, nil
	case *dag.JoinOp:
		if len(parents) != 2 {
			return nil, ErrJoinParents
		}
		var cond expr.Evaluator
		if o.Cond != nil {
			var err error
			cond, err = b.compileVamExpr(o.Cond)
			if err != nil {
				return nil, err
			}
		}
		join := op.NewNestedLoopJoin(b.rctx, parents[0], parents[1], o.Style, o.LeftAlias, o.RightAlias, cond)
		return []vio.Puller{join}, nil
	case *dag.MergeOp:
		exprs, err := b.compileSortExprs(o.Exprs)
		if err != nil {
			return nil, err
		}
		cmp := samexpr.NewComparator(exprs...)
		return []vio.Puller{op.NewMerge(b.rctx, parents, cmp.Compare)}, nil
	case *dag.ScatterOp:
		return b.compileVamScatter(o, parents)
	case *dag.SwitchOp:
		parent := b.combineVam(parents)
		if o.Expr != nil {
			return b.compileVamExprSwitch(o, parent)
		}
		return b.compileVamSwitch(o, parent)
	default:
		p, err := b.compileVamLeaf(o, b.combineVam(parents))
		if err != nil {
			return nil, err
		}
		return []vio.Puller{p}, nil
	}
}

func (b *Builder) combineVam(pullers []vio.Puller) vio.Puller {
	switch len(pullers) {
	case 0:
		return nil
	case 1:
		return pullers[0]
	}
	return op.NewCombine(b.rctx, pullers)
}

func (b *Builder) compileVamFork(fork *dag.ForkOp, parent vio.Puller) ([]vio.Puller, error) {
	var f *op.Fork
	if parent != nil {
		f = op.NewFork(b.rctx, parent)
	}
	var exits []vio.Puller
	for _, seq := range fork.Paths {
		var parent vio.Puller
		if f != nil && !isEntry(seq, false) {
			parent = f.AddBranch()
		}
		exit, err := b.compileVamSeq(seq, []vio.Puller{parent})
		if err != nil {
			return nil, err
		}
		exits = append(exits, exit...)
	}
	return exits, nil
}

func (b *Builder) compileVamScatter(scatter *dag.ScatterOp, parents []vio.Puller) ([]vio.Puller, error) {
	if len(parents) != 1 {
		return nil, errors.New("internal error: scatter operator requires a single parent")
	}
	var concurrentPullers []vio.Puller
	if f, ok := parents[0].(*op.FileScan); ok {
		concurrentPullers = f.NewConcurrentPullers(len(scatter.Paths))
	}
	var ops []vio.Puller
	for i, seq := range scatter.Paths {
		parent := parents[0]
		if len(concurrentPullers) > 0 {
			parent = concurrentPullers[i]
		}
		op, err := b.compileVamSeq(seq, []vio.Puller{parent})
		if err != nil {
			return nil, err
		}
		ops = append(ops, op...)
	}
	return ops, nil
}

func (b *Builder) compileVamExprSwitch(swtch *dag.SwitchOp, parent vio.Puller) ([]vio.Puller, error) {
	e, err := b.compileVamExpr(swtch.Expr)
	if err != nil {
		return nil, err
	}
	s := op.NewExprSwitch(b.rctx, parent, e)
	var exits []vio.Puller
	for _, c := range swtch.Cases {
		var val *super.Value
		if c.Expr != nil {
			val2, err := b.evalAtCompileTime(c.Expr)
			if err != nil {
				return nil, err
			}
			if val2.IsError() {
				return nil, errors.New("switch case is not a constant expression")
			}
			val = &val2
		}
		parents, err := b.compileVamSeq(c.Path, []vio.Puller{s.AddCase(val)})
		if err != nil {
			return nil, err
		}
		exits = append(exits, parents...)
	}
	return exits, nil
}

func (b *Builder) compileVamSwitch(swtch *dag.SwitchOp, parent vio.Puller) ([]vio.Puller, error) {
	s := op.NewSwitch(b.rctx, parent)
	var exits []vio.Puller
	for _, c := range swtch.Cases {
		e, err := b.compileVamExpr(c.Expr)
		if err != nil {
			return nil, fmt.Errorf("compiling switch case filter: %w", err)
		}
		exit, err := b.compileVamSeq(c.Path, []vio.Puller{s.AddCase(e)})
		if err != nil {
			return nil, err
		}
		exits = append(exits, exit...)
	}
	return exits, nil
}

func (b *Builder) compileVamMain(main *dag.Main, parents []vio.Puller) ([]vio.Puller, error) {
	for _, f := range main.Funcs {
		b.funcs[f.Tag] = f
	}
	return b.compileVamSeq(main.Body, parents)
}

func (b *Builder) compileVamLeaf(o dag.Op, parent vio.Puller) (vio.Puller, error) {
	switch o := o.(type) {
	case *dag.AggregateOp:
		return b.compileVamAggregate(o, parent)
	case *dag.CommitMetaScan:
		var pruner samexpr.Evaluator
		if o.Tap && o.KeyPruner != nil {
			var err error
			pruner, err = compileExpr(o.KeyPruner)
			if err != nil {
				return nil, err
			}
		}
		return op.NewCommitMetaScanner(b.rctx.Context, b.sctx(), b.env.DB(), o.Table, o.Commit, o.Meta, pruner)
	case *dag.CountOp:
		var e expr.Evaluator
		if o.Expr != nil {
			var err error
			if e, err = b.compileVamExpr(o.Expr); err != nil {
				return nil, err
			}
		}
		return op.NewCount(b.rctx.Sctx, parent, o.Alias, e), nil
	case *dag.DBMetaScan:
		return op.NewDBMetaScanner(b.rctx.Context, b.sctx(), b.env.DB(), o.Meta)
	case *dag.DebugOp:
		e, err := b.compileVamExpr(o.Expr)
		if err != nil {
			return nil, err
		}
		filter, err := b.compileVamExprWithEmpty(o.Filter)
		if err != nil {
			return nil, err
		}
		d := op.NewDebug(b.rctx, e, filter, b.debugs, parent)
		return d, nil
	case *dag.DeleterScan:
		table, err := b.lookupTable(o.Table)
		if err != nil {
			return nil, err
		}
		var pruner samexpr.Evaluator
		if o.KeyPruner != nil {
			pruner, err = compileExpr(o.KeyPruner)
			if err != nil {
				return nil, err
			}
		}
		if b.deletes == nil {
			b.deletes = &sync.Map{}
		}
		var whereNot expr.Evaluator
		if o.Where != nil {
			whereNot, err = b.compileVamExpr(invertForDeleteWhere(o.Where))
			if err != nil {
				return nil, err
			}
		}
		return op.NewDeleter(b.rctx, sbuf.NewMaterializer(parent), table, whereNot, pruner, b.progress, b.deletes), nil
	case *dag.DistinctOp:
		e, err := b.compileVamExpr(o.Expr)
		if err != nil {
			return nil, err
		}
		return op.NewDistinct(b.sctx(), parent, e), nil
	case *dag.DropOp:
		fields := make(field.List, 0, len(o.Args))
		for _, e := range o.Args {
			fields = append(fields, e.(*dag.ThisExpr).Chain.Path())
		}
		dropper := expr.NewDropper(b.sctx(), fields)
		return op.NewValues(b.sctx(), parent, []expr.Evaluator{dropper}), nil
	case *dag.FileScan:
		if parent == nil {
			parent = vio.NewPuller(vector.NewNull(1))
		}
		var metaFilter *dagFilter
		if mf := o.Pushdown.MetaFilter; mf != nil {
			metaFilter = &dagFilter{field.NewProjectionMaybe(mf.HasProjection, mf.Projection, mf.TypeProjection), mf.Expr}
		}
		var dataFilter *dagFilter
		if df := o.Pushdown.DataFilter; df != nil {
			dataFilter = &dagFilter{field.NewProjectionMaybe(df.HasProjection, df.Projection, df.TypeProjection), df.Expr}
		}
		var searchFilter *dagFilter
		if sf := o.Pushdown.SearchFilter; sf != nil {
			searchFilter = &dagFilter{nil, sf.Expr}
		}
		projection := field.NewProjectionMaybe(o.Pushdown.HasProjection, o.Pushdown.Projection, o.Pushdown.TypeProjection)
		pushdown := b.newPushdown(projection, metaFilter, searchFilter, dataFilter, o.Pushdown.Unordered)
		return op.NewFileScan(b.rctx, b.env, parent, o.Paths, o.Formats, pushdown, b.progress), nil
	case *dag.FilterOp:
		e, err := b.compileVamExpr(o.Expr)
		if err != nil {
			return nil, err
		}
		return op.NewFilter(b.sctx(), parent, e), nil
	case *dag.FuseOp:
		return op.NewFuse(b.sctx(), parent, o.Complete), nil
	case *dag.HTTPScan:
		if o.IsPlainGet() {
			// Read a plain GET like any other input so the runtime reuses
			// whatever the compiler read to determine its type.
			return b.env.Open(b.rctx.Context, b.sctx(), o.URL, o.Format, nil, b.progress, 1)
		}
		body := strings.NewReader(o.Body)
		return b.env.OpenHTTP(b.rctx.Context, b.sctx(), o.URL, o.Format, o.Method, o.Headers, body, nil)
	case *dag.HeadOp:
		return op.NewHead(parent, o.Count), nil
	case *dag.InferOp:
		return op.NewInfer(b.rctx, parent, o.Limit), nil
	case *dag.ListerScan:
		if parent != nil {
			return nil, errors.New("internal error: data source cannot have a parent operator")
		}
		table, err := b.lookupTable(o.Table)
		if err != nil {
			return nil, err
		}
		var pruner samexpr.Evaluator
		if o.KeyPruner != nil {
			pruner, err = compileExpr(o.KeyPruner)
			if err != nil {
				return nil, err
			}
		}
		return op.NewLister(b.rctx.Context, b.mctx, table, o.Commit, pruner)
	case *dag.LoadOp:
		return op.NewLoad(b.rctx, b.env.DB(), parent, o.Table, o.Branch, o.Author, o.Message, o.Meta), nil
	case *dag.NullScan:
		return vio.NewPuller(vector.NewNull(1)), nil
	case *dag.OutputOp:
		b.channels[o.Name] = append(b.channels[o.Name], parent)
		return parent, nil
	case *dag.PassOp:
		return parent, nil
	case *dag.PutOp:
		rec, err := dag.NewRecordExprFromAssignments(o.Args)
		if err != nil {
			return nil, err
		}
		mergeRecordExprWithChain(rec, nil)
		e, err := b.compileVamRecordExpr(rec)
		if err != nil {
			return nil, err
		}
		putter := expr.NewPutter(b.sctx(), e)
		return op.NewValues(b.sctx(), parent, []expr.Evaluator{putter}), nil
	case *dag.RenameOp:
		srcs, dsts, err := b.compileAssignmentsToLvals(o.Args)
		if err != nil {
			return nil, err
		}
		renamer := expr.NewRenamer(b.sctx(), srcs, dsts)
		return op.NewValues(b.sctx(), parent, []expr.Evaluator{renamer}), nil
	case *dag.RobotScan:
		e, err := b.compileVamExpr(o.Expr)
		if err != nil {
			return nil, err
		}
		//XXX The DAG doesn't have pushdowns for robot scans (so the optimizer
		// doesn't compute them), but no reason robot scan shouldn't do projection
		// and metafilter too.  Instead, it just has a filter lifted into it
		// so convert it here to a pushdown.  This will be implemented in a
		// future PR.
		var dataFilter *dagFilter
		if o.Filter != nil {
			dataFilter = &dagFilter{nil, o.Filter}
		}
		return op.NewRobot(b.rctx, b.env, parent, e, o.Format, b.newPushdown(nil, nil, nil, dataFilter, false), b.progress), nil
	case *dag.SeqScan:
		table, err := b.lookupTable(o.Table)
		if err != nil {
			return nil, err
		}
		var pruner samexpr.Evaluator
		if o.KeyPruner != nil {
			pruner, err = compileExpr(o.KeyPruner)
			if err != nil {
				return nil, err
			}
		}
		var filter expr.Evaluator
		if o.Filter != nil {
			filter, err = b.compileVamExpr(o.Filter)
			if err != nil {
				return nil, err
			}
		}
		return op.NewTableScanner(b.rctx, parent, table, filter, pruner, b.progress), nil
	case *dag.SkipOp:
		return op.NewSkip(parent, o.Count), nil
	case *dag.SlicerOp:
		return op.NewSlicer(b.mctx, parent), nil
	case *dag.SortOp:
		exprs, err := b.compileSortExprs(o.Exprs)
		if err != nil {
			return nil, err
		}
		return op.NewSort(b.rctx, parent, exprs, o.Reverse), nil
	case *dag.TailOp:
		return op.NewTail(parent, o.Count), nil
	case *dag.TableScan:
		if parent != nil {
			return nil, errors.New("internal error: table scan cannot have a parent operator")
		}
		// Here we convert TableScan to lister->slicer->seqscan for the slow path as
		// optimizer should do this conversion, but this allows us to run
		// unoptimized scans too.
		table, err := b.lookupTable(o.ID)
		if err != nil {
			return nil, err
		}
		l, err := op.NewLister(b.rctx.Context, b.mctx, table, o.Commit, nil)
		if err != nil {
			return nil, err
		}
		slicer := op.NewSlicer(b.mctx, l)
		return op.NewTableScanner(b.rctx, slicer, table, nil, nil, b.progress), nil
	case *dag.TableMetaScan:
		return op.NewTableMetaScanner(b.rctx.Context, b.sctx(), b.env.DB(), o.ID, o.Meta)
	case *dag.TopOp:
		exprs, err := b.compileSortExprs(o.Exprs)
		if err != nil {
			return nil, err
		}
		return op.NewTop(b.sctx(), parent, o.Limit, exprs, o.Reverse), nil
	case *dag.UniqOp:
		return op.NewUniq(b.sctx(), parent, o.Cflag), nil
	case *dag.UnnestOp:
		e, err := b.compileVamExpr(o.Expr)
		if err != nil {
			return nil, err
		}
		return op.NewUnnest(b.sctx(), parent, e), nil
	case *dag.ValuesOp:
		exprs, err := b.compileVamExprs(o.Exprs)
		if err != nil {
			return nil, err
		}
		return op.NewValues(b.sctx(), parent, exprs), nil
	default:
		return nil, fmt.Errorf("unknown DAG operator %T", o)
	}
}

func invertForDeleteWhere(where dag.Expr) dag.Expr {
	return dag.NewBinaryExpr("or",
		dag.NewUnaryExpr("!", where),
		dag.NewBinaryExpr("or",
			&dag.IsNullExpr{Kind: "IsNullExpr", Expr: where},
			dag.NewCall("is_error", []dag.Expr{where})))
}

func mergeRecordExprWithChain(rec *dag.RecordExpr, chain field.Chain) {
	spread := &dag.Spread{Kind: "Spread", Expr: dag.NewThis(chain)}
	rec.Elems = append([]dag.RecordElem{spread}, rec.Elems...)
	for _, elem := range rec.Elems {
		if field, ok := elem.(*dag.Field); ok {
			if childrec, ok := field.Value.(*dag.RecordExpr); ok {
				mergeRecordExprWithChain(childrec, chain.Append(field.Name, false))
			}
		}
	}
}

func (b *Builder) compileVamSeq(seq dag.Seq, parents []vio.Puller) ([]vio.Puller, error) {
	for _, o := range seq {
		var err error
		parents, err = b.compileVam(o, parents)
		if err != nil {
			return nil, err
		}
	}
	return parents, nil
}

func (b *Builder) compileVamAggregate(s *dag.AggregateOp, parent vio.Puller) (vio.Puller, error) {
	// compile aggs
	var aggNames []field.Path
	var aggExprs []expr.Evaluator
	var aggs []*expr.Aggregator
	for _, assignment := range s.Aggs {
		aggNames = append(aggNames, assignment.LHS.(*dag.ThisExpr).Chain.Path())
		ag, err := b.compileVamAgg(assignment.RHS.(*dag.AggExpr))
		if err != nil {
			return nil, err
		}
		aggs = append(aggs, ag)
		lhs, err := b.compileVamExpr(assignment.LHS)
		if err != nil {
			return nil, err
		}
		if ag.NoRip {
			lhs = expr.NoRipEval(lhs)
		}
		aggExprs = append(aggExprs, lhs)
	}
	// compile keys
	var keyNames []field.Path
	var keyExprs []expr.Evaluator
	for _, assignment := range s.Keys {
		lhs, ok := assignment.LHS.(*dag.ThisExpr)
		if !ok {
			return nil, errors.New("invalid lval in grouping key")
		}
		rhs, err := b.compileVamExpr(assignment.RHS)
		if err != nil {
			return nil, err
		}
		keyNames = append(keyNames, lhs.Chain.Path())
		keyExprs = append(keyExprs, rhs)
	}
	if len(keyExprs) == 0 {
		return aggregate.NewScalar(parent, b.sctx(), aggs, aggNames, aggExprs, s.PartialsIn, s.PartialsOut)
	}
	return aggregate.New(parent, b.sctx(), aggNames, aggExprs, aggs, keyNames, keyExprs, s.PartialsIn, s.PartialsOut)
}

func (b *Builder) compileVamAgg(ag *dag.AggExpr) (*expr.Aggregator, error) {
	name := ag.Name
	var err error
	var arg expr.Evaluator
	if ag.Expr != nil {
		arg, err = b.compileVamExpr(ag.Expr)
		if err != nil {
			return nil, err
		}
	}
	var filter expr.Evaluator
	if ag.Filter != nil {
		filter, err = b.compileVamExpr(ag.Filter)
		if err != nil {
			return nil, err
		}
	}
	pattern, err := agg.NewPattern(b.sctx(), name, ag.Distinct, ag.Expr != nil)
	if err != nil {
		return nil, err
	}
	return expr.NewAggregator(name, ag.Distinct, arg, filter, pattern)
}
