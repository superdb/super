package exec

import (
	"sync"
	"uuid"

	"github.com/superdb/super/runtime"
	"github.com/superdb/super/vector/vio"
)

type DeleteQuery struct {
	*Query
	deletes *sync.Map
}

var _ runtime.DeleteQuery = (*DeleteQuery)(nil)

func NewDeleteQuery(rctx *runtime.Context, puller vio.Puller, deletes *sync.Map) *DeleteQuery {
	return &DeleteQuery{
		Query:   NewQuery(rctx, puller, nil),
		deletes: deletes,
	}
}

func (d *DeleteQuery) DeletionSet() []uuid.UUID {
	var ids []uuid.UUID
	if d.deletes != nil {
		d.deletes.Range(func(key, value any) bool {
			ids = append(ids, key.(uuid.UUID))
			return true
		})
	}
	return ids
}
