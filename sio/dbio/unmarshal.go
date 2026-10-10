package dbio

import (
	"github.com/superdb/super"
	"github.com/superdb/super/db"
	"github.com/superdb/super/db/commits"
	"github.com/superdb/super/db/data"
	"github.com/superdb/super/db/pools"
	"github.com/superdb/super/pkg/field"
)

var unmarshaler *super.Unmarshaler

func init() {
	unmarshaler = super.NewUnmarshaler()
	unmarshaler.Bind(
		commits.Add{},
		commits.Commit{},
		commits.Delete{},
		field.Path{},
		pools.Config{},
		db.BranchMeta{},
		db.BranchTip{},
		data.Object{},
		data.Partition{},
	)
}
