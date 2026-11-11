package aggregate

import (
	"fmt"
	"math/rand/v2"
	"strconv"
	"testing"

	"github.com/superdb/super"
	"github.com/superdb/super/pkg/field"
	"github.com/superdb/super/runtime/expr"
	"github.com/superdb/super/runtime/expr/agg"
	"github.com/superdb/super/vector"
)

const (
	benchBatchLen = 8192
	benchRows     = 1 << 20
)

var benchCards = []int{100, 10_000, 1_000_000}

type slicePuller struct {
	vecs []vector.Any
	i    int
}

func (s *slicePuller) Pull(bool) (vector.Any, error) {
	if s.i == len(s.vecs) {
		return nil, nil
	}
	vec := s.vecs[s.i]
	s.i++
	return vec, nil
}

type benchAgg struct {
	op     string
	arg    string // field name, or "" for no argument
	output string
}

// benchInput returns benchRows records of {s:string,i:int64}, with keys
// drawn uniformly from card values, split into batches.
func benchInput(sctx *super.Context, card int) []vector.Any {
	b, err := vector.NewRecordBuilder(sctx, field.List{{"s"}, {"i"}})
	if err != nil {
		panic(err)
	}
	r := rand.New(rand.NewPCG(1, uint64(card)))
	var vecs []vector.Any
	for range benchRows / benchBatchLen {
		strs := vector.NewStringEmpty(benchBatchLen)
		ints := make([]int64, benchBatchLen)
		for k := range benchBatchLen {
			n := r.IntN(card)
			strs.Append("key" + strconv.Itoa(n))
			ints[k] = int64(n)
		}
		vecs = append(vecs, b.New([]vector.Any{strs, vector.NewInt(super.TypeInt64, ints)}))
	}
	return vecs
}

func newBenchAggregate(b *testing.B, sctx *super.Context, parent *slicePuller, keys []string, aggs []benchAgg, partialsIn, partialsOut, noFastPath bool) *Aggregate {
	var keyNames []field.Path
	var keyExprs []expr.Evaluator
	for _, k := range keys {
		keyNames = append(keyNames, field.Path{k})
		keyExprs = append(keyExprs, expr.NewDottedExpr(sctx, field.NewChain(k)))
	}
	var aggNames []field.Path
	var aggExprs []expr.Evaluator
	var aggregators []*expr.Aggregator
	for _, a := range aggs {
		pattern, err := agg.NewPattern(sctx, a.op, false, a.arg != "")
		if err != nil {
			b.Fatal(err)
		}
		var arg expr.Evaluator
		if a.arg != "" {
			arg = expr.NewDottedExpr(sctx, field.NewChain(a.arg))
		}
		aggregator, err := expr.NewAggregator(a.op, false, arg, nil, pattern)
		if err != nil {
			b.Fatal(err)
		}
		aggNames = append(aggNames, field.Path{a.output})
		aggExprs = append(aggExprs, expr.NewDottedExpr(sctx, field.NewChain(a.output)))
		aggregators = append(aggregators, aggregator)
	}
	op, err := New(parent, sctx, aggNames, aggExprs, aggregators, keyNames, keyExprs, partialsIn, partialsOut)
	if err != nil {
		b.Fatal(err)
	}
	op.noFastPath = noFastPath
	return op
}

func drain(b *testing.B, op *Aggregate) []vector.Any {
	var out []vector.Any
	for {
		vec, err := op.Pull(false)
		if err != nil {
			b.Fatal(err)
		}
		if vec == nil {
			return out
		}
		out = append(out, vec)
	}
}

func BenchmarkAggregate(b *testing.B) {
	cases := []struct {
		name       string
		keys       []string
		aggs       []benchAgg
		noFastPath bool
	}{
		{"count-by-string/fastpath", []string{"s"}, []benchAgg{{"count", "", "c"}}, false},
		{"count-by-string", []string{"s"}, []benchAgg{{"count", "", "c"}}, true},
		{"sum-by-int", []string{"i"}, []benchAgg{{"sum", "i", "sum"}}, false},
		{"count-sum-by-string-int", []string{"s", "i"}, []benchAgg{{"count", "", "c"}, {"sum", "i", "sum"}}, false},
	}
	for _, c := range cases {
		for _, card := range benchCards {
			sctx := super.NewContext()
			input := benchInput(sctx, card)
			b.Run(fmt.Sprintf("%s/card=%d", c.name, card), func(b *testing.B) {
				for b.Loop() {
					drain(b, newBenchAggregate(b, sctx, &slicePuller{vecs: input}, c.keys, c.aggs, false, false, c.noFastPath))
				}
			})
			// Merge side: feed the partials of each batch (as one
			// worker per batch would produce) into a partialsIn aggregate.
			var partials []vector.Any
			for _, vec := range input {
				p := newBenchAggregate(b, sctx, &slicePuller{vecs: []vector.Any{vec}}, c.keys, c.aggs, false, true, c.noFastPath)
				partials = append(partials, drain(b, p)...)
			}
			b.Run(fmt.Sprintf("%s/merge/card=%d", c.name, card), func(b *testing.B) {
				for b.Loop() {
					drain(b, newBenchAggregate(b, sctx, &slicePuller{vecs: partials}, c.keys, c.aggs, true, false, c.noFastPath))
				}
			})
		}
	}
}
