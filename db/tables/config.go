package tables

import (
	"uuid"

	"github.com/superdb/super/bsup"
	"github.com/superdb/super/db/data"
	"github.com/superdb/super/db/journal"
	"github.com/superdb/super/order"
	"github.com/superdb/super/pkg/nano"
	"github.com/superdb/super/pkg/storage"
)

type Config struct {
	Ts        nano.Ts        `super:"ts"`
	Name      string         `super:"name"`
	ID        uuid.UUID      `super:"id"`
	SortKeys  order.SortKeys `super:"sortkeys"`
	ObjectCap uint64         `super:"objectcap"`
	FrameCap  uint64         `super:"framecap"`
	// Type is the encoded type value (see super.EncodeTypeValue) of the
	// type to which the table's values must conform or empty if the table
	// has no type.
	Type []byte `super:"type"`
	// SQL is true if the table was created with SQL columns, in which case
	// a column missing from a loaded value is null if its type admits null.
	SQL bool `super:"sql"`
}

var _ journal.Entry = (*Config)(nil)

func NewConfig(name string, sortKeys order.SortKeys, objectCap, frameCap uint64) *Config {
	if objectCap == 0 {
		objectCap = data.DefaultObjectCap
	}
	if frameCap == 0 {
		frameCap = bsup.DefaultFrameCap
	}
	return &Config{
		Ts:        nano.Now(),
		Name:      name,
		ID:        uuid.NewV7(),
		SortKeys:  sortKeys,
		ObjectCap: objectCap,
		FrameCap:  frameCap,
	}
}

func (p *Config) Key() string {
	return p.Name
}

func (p *Config) Path(root *storage.URI) *storage.URI {
	return root.JoinPath(p.ID.String())
}
