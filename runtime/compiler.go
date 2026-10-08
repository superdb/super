package runtime

import (
	"context"
	"uuid"

	"github.com/superdb/super"
	"github.com/superdb/super/compiler/parser"
	"github.com/superdb/super/db/data"
	"github.com/superdb/super/dbid"
	"github.com/superdb/super/vector/vio"
)

type Compiler interface {
	NewQuery(*Context, *parser.AST, []vio.Puller, int) (Query, error)
	NewDeleteQuery(*Context, *parser.AST, *dbid.Committish) (DeleteQuery, error)
	NewObjectScanner(rctx *Context, poolID uuid.UUID, objects []*data.Object) (vio.Puller, error)
}

type Query interface {
	vio.Puller
	vio.Meter
}

type DeleteQuery interface {
	Query
	DeletionSet() []uuid.UUID
}

func CompileQuery(ctx context.Context, sctx *super.Context, c Compiler, ast *parser.AST, readers []vio.Puller) (Query, error) {
	rctx := NewContext(ctx, sctx)
	q, err := c.NewQuery(rctx, ast, readers, 0)
	if err != nil {
		rctx.Cancel()
		return nil, err
	}
	return q, nil
}

func CompileQueryForDB(ctx context.Context, sctx *super.Context, c Compiler, ast *parser.AST) (Query, error) {
	rctx := NewContext(ctx, sctx)
	q, err := c.NewQuery(rctx, ast, nil, 0)
	if err != nil {
		rctx.Cancel()
		return nil, err
	}
	return q, nil
}
