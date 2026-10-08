package describe

import (
	"context"
	"fmt"
	"uuid"

	"github.com/superdb/super/compiler/dag"
	"github.com/superdb/super/compiler/optimizer"
	"github.com/superdb/super/compiler/parser"
	"github.com/superdb/super/compiler/semantic"
	"github.com/superdb/super/db"
	"github.com/superdb/super/order"
	"github.com/superdb/super/pkg/field"
	"github.com/superdb/super/runtime/exec"
)

type Info struct {
	Sources  []Source  `json:"sources"`
	Channels []Channel `json:"channels"`
}

type Source interface {
	Source()
}

type (
	DBMeta struct {
		Kind string `json:"kind"`
		Meta string `json:"meta"`
	}
	Pool struct {
		Kind string    `json:"kind"`
		Name string    `json:"name"`
		ID   uuid.UUID `json:"id"`
	}
	Path struct {
		Kind string `json:"kind"`
		URI  string `json:"uri"`
	}
	Null struct {
		Kind string `json:"kind"`
	}
)

func (*DBMeta) Source() {}
func (*Pool) Source()   {}
func (*Path) Source()   {}
func (*Null) Source()   {}

type Channel struct {
	Name            string         `json:"name"`
	AggregationKeys field.List     `json:"aggregation_keys"`
	Sort            order.SortKeys `json:"sort"`
}

func Analyze(ctx context.Context, query string, src *exec.Environment) (*Info, error) {
	ast, err := parser.ParseText(query)
	if err != nil {
		return nil, err
	}
	entry, err := semantic.Analyze(ctx, ast, src, false)
	if err != nil {
		return nil, err
	}
	return AnalyzeDAG(ctx, entry, src)
}

func AnalyzeDAG(ctx context.Context, main *dag.Main, src *exec.Environment) (*Info, error) {
	entry := main.Body
	var err error
	var info Info
	if info.Sources, err = describeSources(ctx, src.DB(), entry[0]); err != nil {
		return nil, err
	}
	sortKeys, err := optimizer.New(ctx, src).SortKeys(entry)
	if err != nil {
		return nil, err
	}
	aggKeys := describeAggs(entry, []field.List{nil})
	outputs := collectOutputs(entry)
	m := make(map[string]int)
	for i, s := range sortKeys {
		name := outputs[i].Name
		if k, ok := m[name]; ok {
			// If output already exists, this means the outputs will be
			// combined so nil everything out.
			// XXX This is currently what happens but is this right?
			c := &info.Channels[k]
			c.Sort, c.AggregationKeys = nil, nil
			continue
		}
		info.Channels = append(info.Channels, Channel{
			Name:            name,
			Sort:            s,
			AggregationKeys: aggKeys[i],
		})
		m[name] = i
	}
	return &info, nil
}

func describeSources(ctx context.Context, root *db.Root, o dag.Op) ([]Source, error) {
	switch o := o.(type) {
	case *dag.ForkOp:
		var s []Source
		for _, p := range o.Paths {
			out, err := describeSources(ctx, root, p[0])
			if err != nil {
				return nil, err
			}
			s = append(s, out...)
		}
		return s, nil
	case *dag.NullScan:
		return []Source{&Null{Kind: "Null"}}, nil
	case *dag.FileScan:
		var sources []Source
		for _, p := range o.Paths {
			sources = append(sources, &Path{Kind: "Path", URI: p})
		}
		return sources, nil
	case *dag.HTTPScan:
		return []Source{&Path{Kind: "Path", URI: o.URL}}, nil
	case *dag.PoolScan:
		return sourceOfPool(ctx, root, o.ID)
	case *dag.ListerScan:
		return sourceOfPool(ctx, root, o.Pool)
	case *dag.SeqScan:
		return sourceOfPool(ctx, root, o.Pool)
	case *dag.CommitMetaScan:
		return sourceOfPool(ctx, root, o.Pool)
	case *dag.DBMetaScan:
		return []Source{&DBMeta{Kind: "DBMeta", Meta: o.Meta}}, nil
	default:
		return nil, fmt.Errorf("unsupported source type %T", o)
	}
}

func sourceOfPool(ctx context.Context, root *db.Root, id uuid.UUID) ([]Source, error) {
	p, err := root.OpenPool(ctx, id)
	if err != nil {
		return nil, err
	}
	return []Source{&Pool{
		Kind: "Pool",
		ID:   id,
		Name: p.Name,
	}}, nil
}

func describeAggs(seq dag.Seq, parents []field.List) []field.List {
	for _, op := range seq {
		parents = describeOpAggs(op, parents)
	}
	return parents
}

func describeOpAggs(op dag.Op, parents []field.List) []field.List {
	switch op := op.(type) {
	case *dag.AggregateOp:
		// The field list for aggregation with no keys is an empty slice and
		// not nil.
		keys := field.List{}
		for _, k := range op.Keys {
			keys = append(keys, k.RHS.(*dag.ThisExpr).Chain.Path())
		}
		return []field.List{keys}
	case *dag.ForkOp:
		var aggs []field.List
		for _, p := range op.Paths {
			aggs = append(aggs, describeAggs(p, []field.List{nil})...)
		}
		return aggs
	case *dag.ScatterOp:
		var aggs []field.List
		for _, p := range op.Paths {
			aggs = append(aggs, describeAggs(p, []field.List{nil})...)
		}
		return aggs
	}
	// If more than one parent reset to nil aggregation.
	if len(parents) > 1 {
		return []field.List{nil}
	}
	return parents
}

func collectOutputs(seq dag.Seq) []*dag.OutputOp {
	var outputs []*dag.OutputOp
	optimizer.Walk(seq, func(seq dag.Seq) dag.Seq {
		if len(seq) > 0 {
			if o, ok := seq[len(seq)-1].(*dag.OutputOp); ok {
				outputs = append(outputs, o)
			}
		}
		return seq
	})
	return outputs
}
