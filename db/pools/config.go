package pools

import (
	"github.com/segmentio/ksuid"
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
	ID        ksuid.KSUID    `super:"id"`
	SortKeys  order.SortKeys `super:"layout"`
	Threshold int64          `super:"threshold"`
}

var _ journal.Entry = (*Config)(nil)

func NewConfig(name string, sortKeys order.SortKeys, thresh int64) *Config {
	if sortKeys.IsNil() {
		sortKeys = order.SortKeys{order.NewSortKey(order.Desc, field.Dotted("ts"))}
	}
	if thresh == 0 {
		thresh = data.DefaultThreshold
	}
	return &Config{
		Ts:        nano.Now(),
		Name:      name,
		ID:        ksuid.New(),
		SortKeys:  sortKeys,
		Threshold: thresh,
	}
}

func (p *Config) Key() string {
	return p.Name
}

func (p *Config) Path(root *storage.URI) *storage.URI {
	return root.JoinPath(p.ID.String())
}
