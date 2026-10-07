package runtime

import (
	"github.com/segmentio/ksuid"
	"github.com/superdb/super/compiler/parser"
	"github.com/superdb/super/db/data"
	"github.com/superdb/super/dbid"
	"github.com/superdb/super/vector/vio"
)

type Environment interface {
	NewQuery(*Context, *parser.AST, []vio.Puller, int) (Query, error)
	NewDeleteQuery(*Context, *parser.AST, *dbid.Committish) (DeleteQuery, error)
	NewObjectScanner(rctx *Context, poolID ksuid.KSUID, objects []*data.Object) (vio.Puller, error)
}

type Query interface {
	vio.Puller
	vio.Meter
}

type DeleteQuery interface {
	Query
	DeletionSet() []ksuid.KSUID
}
