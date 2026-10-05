package op

import (
	"errors"
	"sync"

	"github.com/superdb/super/db"
	"github.com/superdb/super/db/data"
	"github.com/superdb/super/pkg/field"
	"github.com/superdb/super/runtime"
	"github.com/superdb/super/runtime/expr"
	"github.com/superdb/super/runtime/vcache"
	"github.com/superdb/super/vector"
	"github.com/superdb/super/vector/vio"
)

type Searcher struct {
	cache      *vcache.Cache
	filter     expr.Evaluator
	once       sync.Once
	parent     *objectPuller
	pool       *db.Pool
	projection field.Projection
	rctx       *runtime.Context
	resultCh   chan searchResult
	doneCh     chan struct{}
}

func NewSearcher(rctx *runtime.Context, cache *vcache.Cache, parent vio.Puller, pool *db.Pool, filter expr.Evaluator, project []field.Path) (*Searcher, error) {
	return &Searcher{
		cache:      cache,
		filter:     filter,
		parent:     newObjectPuller(parent),
		pool:       pool,
		projection: field.NewProjection(project),
		rctx:       rctx,
		resultCh:   make(chan searchResult),
		doneCh:     make(chan struct{}),
	}, nil
}

func (s *Searcher) Pull(done bool) (*data.Object, *vector.Bool, error) {
	s.once.Do(func() { go s.run() })
	if done {
		select {
		case s.doneCh <- struct{}{}:
			return nil, nil, nil
		case <-s.rctx.Done():
			return nil, nil, s.rctx.Err()
		}
	}
	if r, ok := <-s.resultCh; ok {
		return r.obj, r.bits, r.err
	}
	return nil, nil, s.rctx.Err()
}

func (s *Searcher) run() {
	for {
		meta, err := s.parent.Pull(false)
		if meta == nil {
			s.sendResult(nil, nil, err)
			return
		}
		object, err := s.cache.Fetch(s.rctx.Context, meta.VectorURI(s.pool.DataPath), meta.ID)
		if err != nil {
			s.sendResult(nil, nil, err)
			return
		}
		vec, err := object.Fetch(s.rctx.Sctx, s.projection)
		if err != nil {
			s.sendResult(nil, nil, err)
			return
		}
		b, ok := s.filter.Eval(vec).(*vector.Bool)
		if !ok {
			s.sendResult(nil, nil, errors.New("system error: runtime.Searcher encountered a non-boolean filter result"))
			return
		}
		s.sendResult(meta, b, nil)
	}
}

func (s *Searcher) sendResult(o *data.Object, b *vector.Bool, err error) {
	select {
	case s.resultCh <- searchResult{o, b, err}:
	case <-s.doneCh:
		_, pullErr := s.parent.Pull(true)
		if err == nil {
			err = pullErr
		}
		if err != nil {
			select {
			case s.resultCh <- searchResult{err: err}:
			case <-s.rctx.Done():
			}
		}
	case <-s.rctx.Done():
	}
}

type searchResult struct {
	obj  *data.Object
	bits *vector.Bool
	err  error
}
