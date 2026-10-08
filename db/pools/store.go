package pools

import (
	"context"
	"errors"
	"fmt"

	"uuid"
	"github.com/superdb/super/db/journal"
	"github.com/superdb/super/pkg/storage"
	"go.uber.org/zap"
)

var (
	ErrExists   = errors.New("pool already exists")
	ErrNotFound = errors.New("pool not found")
)

type Store struct {
	store *journal.Store
}

func CreateStore(ctx context.Context, engine storage.Engine, logger *zap.Logger, path *storage.URI) (*Store, error) {
	store, err := journal.CreateStore(ctx, engine, logger, path, Config{})
	if err != nil {
		return nil, err
	}
	return &Store{store}, nil
}

func OpenStore(ctx context.Context, engine storage.Engine, logger *zap.Logger, path *storage.URI) (*Store, error) {
	store, err := journal.OpenStore(ctx, engine, logger, path, Config{})
	if err != nil {
		return nil, err
	}
	return &Store{store}, nil
}

func (s *Store) All(ctx context.Context) ([]Config, error) {
	entries, err := s.store.All(ctx)
	if err != nil {
		return nil, err
	}
	list := make([]Config, 0, len(entries))
	for _, entry := range entries {
		pool, ok := entry.(*Config)
		if !ok {
			return nil, errors.New("corrupt pool config journal")
		}
		list = append(list, *pool)
	}
	return list, nil
}

func (s *Store) LookupByID(ctx context.Context, id uuid.UUID) (*Config, error) {
	list, err := s.All(ctx)
	if err == nil {
		for k, config := range list {
			if config.ID == id {
				return &list[k], nil
			}
		}
	}
	return nil, fmt.Errorf("%s: %w", id, ErrNotFound)
}

func (s *Store) LookupByName(ctx context.Context, name string) *Config {
	list, err := s.All(ctx)
	if err != nil {
		return nil
	}
	for k, config := range list {
		if config.Name == name {
			return &list[k]
		}
	}
	return nil
}

func (s *Store) Add(ctx context.Context, config *Config) error {
	return s.store.Insert(ctx, config)
}

func (s *Store) Rename(ctx context.Context, id uuid.UUID, newName string) error {
	config, err := s.LookupByID(ctx, id)
	if err != nil {
		return err
	}
	oldName := config.Name
	config.Name = newName
	err = s.store.Move(ctx, oldName, config)
	switch err {
	case journal.ErrKeyExists:
		return fmt.Errorf("%s: %w", newName, ErrExists)
	case journal.ErrNoSuchKey:
		return fmt.Errorf("%s: %w", config.ID, ErrNotFound)
	}
	return err
}

// Remove deletes a pool from the configuration journal.
func (s *Store) Remove(ctx context.Context, config Config) error {
	err := s.store.Delete(ctx, config.Name, func(v journal.Entry) bool {
		p, ok := v.(*Config)
		if !ok {
			return false
		}
		return p.ID == config.ID
	})
	if err != nil {
		if err == journal.ErrNoSuchKey {
			return fmt.Errorf("%s: %w", config.ID, ErrNotFound)
		}
		if err == journal.ErrConstraint {
			return fmt.Errorf("%s: pool %q renamed during removal", config.Name, config.ID)
		}
		return err
	}
	return nil
}
