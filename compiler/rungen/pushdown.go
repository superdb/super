package rungen

import (
	"github.com/superdb/super/compiler/dag"
	"github.com/superdb/super/pkg/field"
	"github.com/superdb/super/runtime/sam/expr"
	"github.com/superdb/super/sbuf"
)

type pushdown struct {
	dataFilter     dag.Expr
	metaFilter     dag.Expr
	builder        *Builder
	projection     field.Projection
	metaProjection field.Projection
	unordred       bool
}

var _ sbuf.Pushdown = (*pushdown)(nil)

func (p *pushdown) DataFilter() (expr.Evaluator, error) {
	if p.dataFilter == nil {
		return nil, nil
	}
	return p.builder.compileExpr(p.dataFilter)
}

func (p *pushdown) BSUPFilter() (*expr.BufferFilter, error) {
	if p.dataFilter == nil {
		return nil, nil
	}
	return CompileBufferFilter(p.builder.sctx(), p.dataFilter)
}

func (p *pushdown) MetaFilter() (expr.Evaluator, field.Projection, error) {
	if p.metaFilter == nil {
		return nil, nil, nil
	}
	e, err := p.builder.compileExpr(p.metaFilter)
	if err != nil {
		return nil, nil, err
	}
	return e, p.metaProjection, nil
}

func (p *pushdown) Projection() field.Projection {
	return p.projection
}

func (p *pushdown) Unordered() bool {
	return p.unordred
}
