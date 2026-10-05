package vcache

import (
	"context"
	"errors"
	"sync"

	"github.com/segmentio/ksuid"
	"github.com/superdb/super"
	"github.com/superdb/super/bsup"
	"github.com/superdb/super/pkg/storage"
)

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
	objects map[ksuid.KSUID]*Object
	locks   map[ksuid.KSUID]*sync.Mutex
}

func NewCache(engine storage.Engine) *Cache {
	return &Cache{
		engine:  engine,
		objects: make(map[ksuid.KSUID]*Object),
		locks:   make(map[ksuid.KSUID]*sync.Mutex),
	}
}

func (c *Cache) lock(id ksuid.KSUID) {
	c.mu.Lock()
	defer c.mu.Unlock()
	mu, ok := c.locks[id]
	if !ok {
		mu = &sync.Mutex{}
		c.locks[id] = mu
	}
	mu.Lock()
}

func (c *Cache) unlock(id ksuid.KSUID) {
	c.mu.Lock()
	c.locks[id].Unlock()
	c.mu.Unlock()
}

func (c *Cache) Fetch(ctx context.Context, uri *storage.URI, id ksuid.KSUID) (*Object, error) {
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
	fit := bsup.NewSeekable(super.NewContext(), r)
	frame, err := fit.Next()
	if err != nil {
		return nil, err
	}
	colFrame, ok := frame.(*bsup.ColFrame)
	if !ok {
		return nil, errors.New("input not in BSUP column form")
	}
	object = NewReader(colFrame)
	c.mu.Lock()
	c.objects[id] = object
	c.mu.Unlock()
	return object, nil
}
