package vcache

import (
	"context"
	"sync"

	"uuid"
	"github.com/superdb/super"
	"github.com/superdb/super/bsup"
	"github.com/superdb/super/pkg/storage"
)

// Cache manages all accesses to storage objects used by databases
// providing a means to retrieve metadata, fused types, and column
// frames from objects effeciently from storage and cached in memory.
// It works for both BSUP columns and rows frames but is generally used
// for BSUP columns data stored in databases.
type Cache struct {
	mu     sync.Mutex
	engine storage.Engine
	// objects is currently a simple map but we will turn this into an
	// LRU cache sometime soon.  First step is object-level granularity, though
	// we might want LRU inside of objects based on vectors.  We can do that
	// later if measurements warrant it.  XXX note that we keep the storage
	// reader open for every object and never close it.  We should timeout
	// files and close them and then reopen them when needed to access
	// vectors that haven't yet been loaded.
	objects map[uuid.UUID]*Object
	locks   map[uuid.UUID]*sync.Mutex
}

func NewCache(engine storage.Engine) *Cache {
	return &Cache{
		engine:  engine,
		objects: make(map[uuid.UUID]*Object),
		locks:   make(map[uuid.UUID]*sync.Mutex),
	}
}

func (c *Cache) lock(id uuid.UUID) {
	c.mu.Lock()
	defer c.mu.Unlock()
	mu, ok := c.locks[id]
	if !ok {
		mu = &sync.Mutex{}
		c.locks[id] = mu
	}
	mu.Lock()
}

func (c *Cache) unlock(id uuid.UUID) {
	c.mu.Lock()
	c.locks[id].Unlock()
	c.mu.Unlock()
}

func (c *Cache) Fetch(ctx context.Context, uri *storage.URI, id uuid.UUID) (*Object, error) {
	c.mu.Lock()
	object, ok := c.objects[id]
	c.mu.Unlock()
	if ok {
		return object, nil
	}
	c.lock(id)
	defer c.unlock(id)
	c.mu.Lock()
	object, ok = c.objects[id]
	c.mu.Unlock()
	if ok {
		return object, nil
	}
	local := storage.NewLocalEngine()
	r, err := local.Get(ctx, uri)
	if err != nil {
		return nil, err
	}
	// XXX we need to refactor this interface since it's no longer aligned one
	// cached entity per file (cache should operate on frames like parquet row groups)
	object, err = NewObject(bsup.NewSeekable(super.NewContext(), r))
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	c.objects[id] = object
	c.mu.Unlock()
	return object, nil
}
