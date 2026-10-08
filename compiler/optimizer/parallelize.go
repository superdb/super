package optimizer

import (
	"reflect"

	"github.com/superdb/super/compiler/dag"
	"github.com/superdb/super/order"
)

// Parallelize tries to parallelize the DAG by splitting each source
// path as much as possible of the sequence into n parallel branches.
func (o *Optimizer) Parallelize(main *dag.Main, concurrency int) error {
	// Compute the number of parallel paths across all input sources to
	// achieve the desired level of concurrency.  At some point, we should
	// use a semaphore here and let each possible path use the max concurrency.
	if o.nent == 0 {
		return nil
	}
	concurrency = max(concurrency/o.nent, 2)
	seq, err := walkEntries(main.Body, func(seq dag.Seq) (dag.Seq, error) {
		if len(seq) == 0 {
			return seq, nil
		}
		var front, parallel dag.Seq
		var err error
		if lister, slicer, rest := matchSource(seq); lister != nil {
			// We parallelize the scanning to achieve the desired concurrency,
			// then the step below pulls downstream operators into the parallel
			// branches when possible, e.g., to parallelize aggregations etc.
			front.Append(lister)
			if slicer != nil {
				front.Append(slicer)
			}
			parallel, err = o.parallelizeSeqScan(rest, concurrency)
		} else if scan, ok := seq[0].(*dag.FileScan); ok {
			front.Append(scan)
			parallel, err = o.parallelizeFileScan(seq[1:], concurrency)
		}
		if err != nil {
			return nil, err
		}
		if parallel == nil {
			// Leave the source path unmodified.
			return seq, nil
		}
		// Replace the source path with the parallelized gadget.
		return append(front, parallel...), nil
	})
	if err != nil {
		return err
	}
	o.optimizeParallels(seq)
	main.Body = removePassOps(seq)
	return nil
}

func matchSource(seq dag.Seq) (*dag.ListerScan, *dag.SlicerOp, dag.Seq) {
	lister, ok := seq[0].(*dag.ListerScan)
	if !ok {
		return nil, nil, nil
	}
	seq = seq[1:]
	slicer, ok := seq[0].(*dag.SlicerOp)
	if ok {
		seq = seq[1:]
	}
	if _, ok := seq[0].(*dag.SeqScan); !ok {
		panic("parseSource: no SeqScan")
	}
	return lister, slicer, seq
}

func (o *Optimizer) parallelizeFileScan(seq dag.Seq, replicas int) (dag.Seq, error) {
	// Prepend a pass so we can parallelize seq[0].
	seq = append(dag.Seq{dag.Pass}, seq...)
	n, sortExprs, _, err := o.concurrentPath(seq, nil)
	if err != nil {
		return nil, err
	}
	if n < len(seq) {
		switch seq[n].(type) {
		case *dag.AggregateOp, *dag.SortOp, *dag.TopOp:
			return parallelizeHead(seq, n, sortExprs, replicas), nil
		}
	}
	return nil, nil
}

func (o *Optimizer) parallelizeSeqScan(seq dag.Seq, replicas int) (dag.Seq, error) {
	scan := seq[0].(*dag.SeqScan)
	if len(seq) == 1 && scan.Filter == nil {
		// We don't try to parallelize the path if it's simply scanning and does no
		// other work.  We might want to revisit this down the road if
		// the system would benefit for parallel reading and merging.
		return nil, nil
	}
	srcSortKeys, err := o.sortKeysOfSource(scan)
	if err != nil {
		return nil, err
	}
	if len(srcSortKeys) > 1 {
		// XXX Don't yet support multi-key ordering.  See Issue #2657.
		return nil, nil
	}
	// concurrentPath will check that the path consisting of the original source
	// sequence and any lifted sequence is still parallelizable.
	n, sortExprs, _, err := o.concurrentPath(seq[1:], srcSortKeys)
	if err != nil {
		return nil, err
	}
	return parallelizeHead(seq, n+1, sortExprs, replicas), nil
}

func parallelizeHead(seq dag.Seq, n int, sortExprs []dag.SortExpr, replicas int) dag.Seq {
	head := seq[:n]
	tail := seq[n:]
	scatter := &dag.ScatterOp{
		Kind:  "ScatterOp",
		Paths: make([]dag.Seq, replicas),
	}
	for k := range replicas {
		scatter.Paths[k] = dag.CopySeq(head)
	}
	var merge dag.Op
	if len(sortExprs) > 0 {
		// At this point, we always insert a merge as we don't know if the
		// downstream DAG requires the sort order.  A later step will look at
		// the fanin from this parallel structure and see if the merge can be
		// removed while also pushing additional ops from the output segment up into
		// the parallel branches to enhance concurrency.
		merge = &dag.MergeOp{Kind: "MergeOp", Exprs: sortExprs}
	} else {
		merge = &dag.CombineOp{Kind: "CombineOp"}
	}
	return append(dag.Seq{scatter, merge}, tail...)
}

func (o *Optimizer) optimizeParallels(seq dag.Seq) {
	Walk(seq, func(seq dag.Seq) dag.Seq {
		for ops := seq; len(ops) >= 2; ops = ops[1:] {
			o.liftIntoParPaths(ops)
		}
		return seq
	})
}

// liftIntoParPaths examines seq to see if there's an opportunity to
// lift operations downstream from a parallel op into its parallel paths to
// enhance concurrency.  If so, we modify the sequential ops in place.
func (o *Optimizer) liftIntoParPaths(seq dag.Seq) {
	if len(seq) < 2 {
		// Need a parallel, an optional merge/combine, and something downstream.
		return
	}
	paths, ok := parallelPaths(seq[0])
	if !ok {
		return
	}
	egress := 1
	var merge *dag.MergeOp
	switch op := seq[1].(type) {
	case *dag.MergeOp:
		merge = op
		egress = 2
	case *dag.CombineOp:
		egress = 2
	}
	if egress >= len(seq) {
		return
	}
	switch op := seq[egress].(type) {
	case *dag.AggregateOp:
		// To decompose the aggregate, we split the flowgraph into
		// branches that run up to and including an aggregate,
		// followed by a post-merge aggregate that composes the results.
		// Copy the aggregator into the tail of the trunk and arrange
		// for partials to flow between them.
		if op.PartialsIn || op.PartialsOut {
			// Need an unmodified aggregate to split into its parials pieces.
			return
		}
		for k := range paths {
			partial := dag.CopyOp(op).(*dag.AggregateOp)
			partial.PartialsOut = true
			paths[k].Append(partial)
		}
		op.PartialsIn = true
		// The upstream aggregators will compute any key expressions
		// so the ingress aggregator should simply reference the key
		// by its name.  This loop updates the ingress to do so.
		for k := range op.Keys {
			op.Keys[k].RHS = op.Keys[k].LHS
		}
	case *dag.SortOp:
		if len(op.Exprs) == 0 {
			return
		}
		seq[1] = &dag.MergeOp{Kind: "MergeOp", Exprs: op.Exprs}
		if egress > 1 {
			seq[2] = dag.Pass
		}
		for k := range paths {
			paths[k].Append(dag.CopyOp(op))
		}
	case *dag.TopOp:
		if len(op.Exprs) == 0 {
			return
		}
		seq[1] = &dag.MergeOp{Kind: "MergeOp", Exprs: op.Exprs}
		seq[2] = &dag.HeadOp{Kind: "HeadOp", Count: op.Limit}
		for k := range paths {
			paths[k].Append(dag.CopyOp(op))
		}
	case *dag.HeadOp, *dag.TailOp:
		// Copy the head or tail into the parallel path and leave the original in
		// place which will apply another head or tail after the merge.
		for k := range paths {
			paths[k].Append(dag.CopyOp(op))
		}
	case *dag.CutOp, *dag.DropOp, *dag.FilterOp, *dag.PutOp, *dag.RenameOp:
		if merge != nil {
			// See if this op would disrupt the merge-on key
			mergeKey, err := o.propagateSortKeyOp(merge, []order.SortKeys{nil})
			if err != nil || mergeKey[0].IsNil() {
				// Bail if there's a merge with a non-key expression.
				return
			}
			key, err := o.propagateSortKeyOp(op, mergeKey)
			if err != nil || !key[0].Equal(mergeKey[0]) {
				// This operator destroys the merge order so we cannot
				// lift it up into the parallel legs in front of the merge.
				return
			}
		}
		for k := range paths {
			paths[k].Append(dag.CopyOp(op))
		}
		// this will get removed later
		seq[egress] = dag.Pass
	}
}

func parallelPaths(op dag.Op) ([]dag.Seq, bool) {
	if s, ok := op.(*dag.ScatterOp); ok {
		return s.Paths, true
	}
	if f, ok := op.(*dag.ForkOp); ok {
		return f.Paths, true
	}
	return nil, false
}

// concurrentPath returns the largest path within seq from front to end that can
// be parallelized and run concurrently while preserving its semantics where
// the input to seq is known to have an order defined by sortKey (or order.Nil
// if unknown).
// The length of the concurrent path is returned and the sort order at
// exit from that path is returned.  If sortKey is zero, then the
// concurrent path is allowed to include operators that do not guarantee
// an output order.
func (o *Optimizer) concurrentPath(seq dag.Seq, sortKeys order.SortKeys) (length int, outputSortExprs []dag.SortExpr, orderRequired bool, err error) {
	for k := range seq {
		switch op := seq[k].(type) {
		// This should be a boolean in op.go that defines whether
		// function can be parallelized... need to think through
		// what the meaning is here exactly.  This is all still a bit
		// of a heuristic.  See #2660 and #2661.
		case *dag.AggregateOp:
			if isOrderedAggregate(op) {
				return 0, nil, false, nil
			}
			// We want input sorted when we are preserving order into
			// aggregate so we can release values incrementally which is really
			// important when doing a head on the aggregate results
			if isKeyOfAggregate(op, sortKeys) {
				// Keep the input ordered so we can incrementally release
				// results from the aggregate as a streaming operation.
				return k, sortExprsForSortKeys(sortKeys), true, nil
			}
			return k, nil, false, nil
		case *dag.CountOp:
			return k, nil, true, nil
		case *dag.SortOp:
			if len(op.Exprs) == 0 {
				// No analysis for sort without expression since we can't
				// parallelize the heuristic.  We should revisit these semantics
				// and define a global order across types.
				return 0, nil, false, nil
			}
			return k, op.Exprs, false, nil
		case *dag.TopOp:
			if len(op.Exprs) == 0 {
				// No analysis for top without expression since we can't
				// parallelize the heuristic.
				return 0, nil, false, nil
			}
			return k, op.Exprs, false, nil
		case *dag.LoadOp:
			// XXX At some point Load should have an optimization where if the
			// upstream sort is the same as the Load destination sort we
			// request a merge and set the Load operator to do a sorted write.
			return k, nil, false, nil
		case *dag.SwitchOp:
			if hasAggregate(op) {
				return 0, nil, false, nil
			}
			return k, sortExprsForSortKeys(sortKeys), true, nil
		case *dag.ForkOp, *dag.HeadOp, *dag.ScatterOp, *dag.TailOp, *dag.UniqOp, *dag.FuseOp,
			*dag.HashJoinOp, *dag.InferOp, *dag.JoinOp, *dag.OutputOp:
			return k, sortExprsForSortKeys(sortKeys), true, nil
		default:
			next, err := o.analyzeSortKeys(op, sortKeys)
			if err != nil {
				return 0, nil, false, err
			}
			if !sortKeys.IsNil() && next.IsNil() {
				return k, sortExprsForSortKeys(sortKeys), true, nil
			}
			sortKeys = next
		}
	}
	return len(seq), sortExprsForSortKeys(sortKeys), true, nil
}

func hasAggregate(op dag.Op) bool {
	var found bool
	walkT(reflect.ValueOf(op), func(a *dag.AggregateOp) *dag.AggregateOp {
		found = true
		return a
	})
	return found
}

func sortExprsForSortKeys(keys order.SortKeys) []dag.SortExpr {
	var exprs []dag.SortExpr
	for _, k := range keys {
		exprs = append(exprs, dag.SortExpr{
			Key:   dag.NewThis(k.Path.Chain()),
			Order: k.Order,
			Nulls: k.Order.NullsMax(true)},
		)
	}
	return exprs
}
