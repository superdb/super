package rungen

import (
	"github.com/superdb/super/compiler/dag"
	"github.com/superdb/super/pkg/field"
	"github.com/superdb/super/vector/vio"
)

type pushdown struct {
	projection field.Projection
	metaFilter *dagFilter
	dataFilter *dagFilter
	builder    *Builder
	unordered  bool
}

type dagFilter struct {
	projection field.Projection
	expr       dag.Expr
}

var _ vio.Pushdown = (*pushdown)(nil)

func (p *pushdown) DataFilter() (*vio.Filter, error) {
	if p.dataFilter == nil {
		return nil, nil
	}
	e, err := p.builder.compileVamExpr(p.dataFilter.expr)
	if err != nil {
		return nil, err
	}
	return &vio.Filter{Expr: e, Projection: p.dataFilter.projection}, nil
}

func (p *pushdown) SearchFilter() (vio.Searcher, error) {
	// XXX coming soon
	/*
		if p.dataFilter == nil {
			return nil, nil
		}
		return CompileBufferFilter(p.builder.sctx(), p.dataFilter)
	*/
	return nil, nil
}

func (p *pushdown) MetaFilter() (*vio.ValueFilter, error) {
	if p.metaFilter == nil {
		return nil, nil
	}
	e, err := p.builder.compileExpr(p.metaFilter.expr)
	if err != nil {
		return nil, err
	}
	return &vio.ValueFilter{Expr: e, Projection: p.metaFilter.projection}, nil
}

func (p *pushdown) Projection() field.Projection {
	return p.projection
}

func (p *pushdown) Unordered() bool {
	return p.unordered
}
