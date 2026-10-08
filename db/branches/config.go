package branches

import (
	"uuid"
	"github.com/superdb/super/pkg/nano"
)

type Config struct {
	Ts     nano.Ts     `super:"ts"`
	Name   string      `super:"name"`
	Commit uuid.UUID `super:"commit"`

	// audit info
}

func NewConfig(name string, commit uuid.UUID) *Config {
	return &Config{
		Ts:     nano.Now(),
		Name:   name,
		Commit: commit,
	}
}

func (c *Config) Key() string {
	return c.Name
}
