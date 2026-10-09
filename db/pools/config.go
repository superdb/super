package pools

import (
	"uuid"

	"github.com/superdb/super/bsup"
	"github.com/superdb/super/db/data"
	"github.com/superdb/super/db/journal"
	"github.com/superdb/super/order"
	"github.com/superdb/super/pkg/field"
	"github.com/superdb/super/pkg/nano"
	"github.com/superdb/super/pkg/storage"
)

type Config struct {
	Ts        nano.Ts        `super:"ts"`
	Name      string         `super:"name"`
	ID        uuid.UUID      `super:"id"`
	SortKeys  order.SortKeys `super:"layout"`
	ObjectCap uint64         `super:"objectcap"`
	FrameCap  uint64         `super:"framecap"`
}

var _ journal.Entry = (*Config)(nil)

func NewConfig(name string, sortKeys order.SortKeys, objectCap, frameCap uint64) *Config {
	if sortKeys.IsNil() {
		sortKeys = order.SortKeys{order.NewSortKey(order.Desc, field.Dotted("ts"))}
	}
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
