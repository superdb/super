package commits

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"runtime"
	"slices"
	"sync"

	"uuid"

	arc "github.com/hashicorp/golang-lru/arc/v2"
	"github.com/superdb/super"
	"github.com/superdb/super/bsupbytes"
	"github.com/superdb/super/db/data"
	"github.com/superdb/super/pkg/nano"
	"github.com/superdb/super/pkg/storage"
	"github.com/superdb/super/sio"
	"github.com/superdb/super/sio/bsupio"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"
)

var (
	ErrBadCommitObject = errors.New("first record of object not a commit")
	ErrExists          = errors.New("commit object already exists")
	ErrNotFound        = errors.New("commit object not found")
)

type Store struct {
	engine storage.Engine
	logger *zap.Logger
	path   *storage.URI

	cache     *arc.ARCCache[uuid.UUID, *Object]
	paths     *arc.ARCCache[uuid.UUID, []uuid.UUID]
	snapshots *arc.ARCCache[uuid.UUID, *Snapshot]
}

func OpenStore(engine storage.Engine, logger *zap.Logger, path *storage.URI) (*Store, error) {
	cache, err := arc.NewARC[uuid.UUID, *Object](1024)
	if err != nil {
		return nil, err
	}
	paths, err := arc.NewARC[uuid.UUID, []uuid.UUID](1024)
	if err != nil {
		return nil, err
	}
	snapshots, err := arc.NewARC[uuid.UUID, *Snapshot](32)
	if err != nil {
		return nil, err
	}
	return &Store{
		engine:    engine,
		logger:    logger.Named("commits"),
		path:      path,
		cache:     cache,
		paths:     paths,
		snapshots: snapshots,
	}, nil
}

func (s *Store) Get(ctx context.Context, commit uuid.UUID) (*Object, error) {
	if o, ok := s.cache.Get(commit); ok {
		return o, nil
	}
	r, err := s.engine.Get(ctx, s.pathOf(commit))
	if err != nil {
		return nil, err
	}
	reader, err := bsupio.NewValueReader(ctx, super.NewContext(), r)
	if err != nil {
		return nil, err
	}
	o, err := DecodeObject(reader)
	if err == ErrBadCommitObject {
		err = fmt.Errorf("system error: %s: %w", s.pathOf(commit), ErrBadCommitObject)
	}
	if closeErr := r.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return nil, err
	}
	s.cache.Add(commit, o)
	return o, nil
}

func (s *Store) pathOf(commit uuid.UUID) *storage.URI {
	return s.path.JoinPath(commit.String() + ".bsup")
}

func (s *Store) Put(ctx context.Context, o *Object) error {
	b, err := o.Serialize()
	if err != nil {
		return err
	}
	return storage.Put(ctx, s.engine, s.pathOf(o.Commit), bytes.NewReader(b))
}

// DANGER ZONE - objects should only be removed when GC says they are not used.
func (s *Store) Remove(ctx context.Context, o *Object) error {
	return s.engine.Delete(ctx, s.pathOf(o.Commit))
}

func (s *Store) Snapshot(ctx context.Context, leaf uuid.UUID) (*Snapshot, error) {
	if snap, ok := s.snapshots.Get(leaf); ok {
		return snap, nil
	}
	if snap, err := s.getSnapshot(ctx, leaf); err != nil && !errors.Is(err, fs.ErrNotExist) {
		s.logger.Error("Loading snapshot", zap.Error(err))
	} else if err == nil {
		s.snapshots.Add(leaf, snap)
		return snap, nil
	}
	snap, err := s.buildSnapshot(ctx, leaf)
	if err != nil {
		return nil, err
	}
	if err := s.putSnapshot(ctx, leaf, snap); err != nil {
		s.logger.Error("Storing snapshot", zap.Error(err))
	}
	s.snapshots.Add(leaf, snap)
	return snap, nil
}

func (s *Store) buildSnapshot(ctx context.Context, leaf uuid.UUID) (*Snapshot, error) {
	var objects []*Object
	var base *Snapshot
	for at := leaf; at != uuid.Nil(); {
		if snap, ok := s.snapshots.Get(at); ok {
			base = snap
			break
		}
		var o *Object
		var oErr error
		var wg sync.WaitGroup
		// Start fetching the next data object.
		wg.Go(func() {
			o, oErr = s.Get(ctx, at)
		})
		// Concurrently check for a snapshot.
		if snap, err := s.getSnapshot(ctx, at); err != nil && !errors.Is(err, fs.ErrNotExist) {
			s.logger.Error("Loading snapshot", zap.Error(err))
		} else if err == nil {
			s.snapshots.Add(at, snap)
			base = snap
			break
		}
		// No snapshot found, so wait for data object.
		wg.Wait()
		if oErr != nil {
			if errors.Is(oErr, fs.ErrNotExist) {
				// If object get error is not exists then perhaps commits have
				// been vacated at this point, check if previous is a base
				// commit.
				snap, err := s.getBase(ctx, at)
				if err != nil {
					return nil, fmt.Errorf("system error: error fetching base: %w", err)
				}
				base = snap
				break
			}
			return nil, oErr
		}
		objects = append(objects, o)
		at = o.Parent
	}
	var snap *Snapshot
	if base == nil {
		snap = NewSnapshot()
	} else {
		snap = base.Copy()
	}
	for _, o := range slices.Backward(objects) {
		for _, action := range o.Actions {
			if err := PlayAction(snap, action); err != nil {
				return nil, err
			}
		}
	}
	return snap, nil
}

func (s *Store) getSnapshot(ctx context.Context, commit uuid.UUID) (*Snapshot, error) {
	reader, err := bsupbytes.Get(ctx, s.engine, s.snapshotPathOf(commit), ActionTypes)
	if err != nil {
		return nil, err
	}
	snap, err := decodeSnapshot(reader.Reader)
	if closeErr := reader.Close(); err == nil {
		err = closeErr
	}
	return snap, err
}

func (s *Store) putSnapshot(ctx context.Context, commit uuid.UUID, snap *Snapshot) error {
	b, err := snap.serialize()
	if err != nil {
		return err
	}
	return storage.Put(ctx, s.engine, s.snapshotPathOf(commit), bytes.NewReader(b))
}

func (s *Store) snapshotPathOf(commit uuid.UUID) *storage.URI {
	return s.path.JoinPath(commit.String() + ".snap.bsup")
}

func (s *Store) getBase(ctx context.Context, commit uuid.UUID) (*Snapshot, error) {
	reader, err := bsupbytes.Get(ctx, s.engine, s.basePathOf(commit), ActionTypes)
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	return decodeSnapshot(reader.Reader)
}

func (s *Store) putBase(ctx context.Context, snap *Snapshot, commit uuid.UUID) error {
	b, err := snap.serialize()
	if err != nil {
		return err
	}
	return storage.Put(ctx, s.engine, s.basePathOf(commit), bytes.NewReader(b))
}

func (s *Store) basePathOf(commit uuid.UUID) *storage.URI {
	return s.path.JoinPath(commit.String() + ".base.bsup")
}

// Path return the entire path from the commit object to the root
// in leaf to root order.
func (s *Store) Path(ctx context.Context, leaf uuid.UUID) ([]uuid.UUID, error) {
	if leaf == uuid.Nil() {
		return nil, errors.New("no path for nil commit ID")
	}
	if path, ok := s.paths.Get(leaf); ok {
		return path, nil
	}
	path, err := s.PathRange(ctx, leaf, uuid.Nil())
	if err != nil {
		return nil, err
	}
	s.paths.Add(leaf, path)
	return path, nil
}

func (s *Store) PathRange(ctx context.Context, from, to uuid.UUID) ([]uuid.UUID, error) {
	var path []uuid.UUID
	for at := from; at != uuid.Nil(); {
		if cache, ok := s.paths.Get(at); ok {
			for _, id := range cache {
				path = append(path, id)
				if id == to {
					break
				}
			}
			break
		}
		o, err := s.Get(ctx, at)
		if err != nil {
			// If we get fs.ErrNotExist it means we have vacated and so we can
			// just return the path at this point.
			if errors.Is(err, fs.ErrNotExist) && to == uuid.Nil() {
				break
			}
			return nil, err
		}
		path = append(path, at)
		if at == to {
			break
		}
		at = o.Parent
	}
	return path, nil
}

func (s *Store) GetBytes(ctx context.Context, commit uuid.UUID) ([]byte, *Commit, error) {
	b, err := storage.Get(ctx, s.engine, s.pathOf(commit))
	if err != nil {
		return nil, nil, err
	}
	reader, err := bsupbytes.NewReaderFromBytes(ctx, b, ActionTypes)
	entry, err := reader.Read()
	if err != nil {
		return nil, nil, err
	}
	first, ok := entry.(*Commit)
	if !ok {
		return nil, nil, fmt.Errorf("system error: first record of commit object is not a commit action: %s", s.pathOf(commit))
	}
	return b, first, nil
}

func (s *Store) ReadAll(ctx context.Context, commit, stop uuid.UUID) ([]byte, error) {
	var size int
	var buffers [][]byte
	for commit != uuid.Nil() && commit != stop {
		b, commitObject, err := s.GetBytes(ctx, commit)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				break
			}
			return nil, err
		}
		size += len(b)
		buffers = append(buffers, b)
		commit = commitObject.Parent
	}
	out := make([]byte, 0, size)
	for _, b := range slices.Backward(buffers) {
		out = append(out, b...)
	}
	return out, nil
}

func (s *Store) Open(ctx context.Context, commit, stop uuid.UUID) (io.Reader, error) {
	b, err := s.ReadAll(ctx, commit, stop)
	if err != nil {
		return nil, err
	}
	return bytes.NewReader(b), nil
}

func (s *Store) OpenAsBSUP(ctx context.Context, sctx *super.Context, commit, stop uuid.UUID) (sio.Reader, error) {
	r, err := s.Open(ctx, commit, stop)
	if err != nil {
		return nil, err
	}
	return bsupio.NewValueReader(ctx, sctx, r)
}

func (s *Store) OpenCommitLog(ctx context.Context, sctx *super.Context, commit, stop uuid.UUID) sio.Reader {
	return newLogReader(ctx, sctx, s, commit, stop)
}

// PatchOfCommit computes the snapshot at the parent of the indicated commit
// then computes the difference between that snapshot and the child commit,
// returning the difference as a patch.
func (s *Store) PatchOfCommit(ctx context.Context, commit uuid.UUID) (*Patch, error) {
	path, err := s.Path(ctx, commit)
	if err != nil {
		return nil, err
	}
	if len(path) == 0 {
		return nil, errors.New("system error: no error on pathless commit")
	}
	var base *Snapshot
	if len(path) == 1 {
		// For first commit in branch, just create an empty base ...
		base = NewSnapshot()
	} else {
		parent := path[1]
		base, err = s.Snapshot(ctx, parent)
		if err != nil {
			return nil, err
		}
	}
	patch := NewPatch(base)
	object, err := s.Get(ctx, commit)
	if err != nil {
		return nil, err
	}
	for _, action := range object.Actions {
		if err := PlayAction(patch, action); err != nil {
			return nil, err
		}
	}
	return patch, nil
}

func (s *Store) PatchOfPath(ctx context.Context, base *Snapshot, baseID, commit uuid.UUID) (*Patch, error) {
	path, err := s.PathRange(ctx, commit, baseID)
	if err != nil {
		return nil, err
	}
	patch := NewPatch(base)
	if len(path) < 2 {
		// There are no changes past the base.  Return the empty patch.
		return patch, nil
	}
	// Play objects in forward order skipping over the last path element
	// as that is the base and the difference is relative to it.
	for k := len(path) - 2; k >= 0; k-- {
		o, err := s.Get(ctx, path[k])
		if err != nil {
			return nil, err
		}
		for _, action := range o.Actions {
			if err := PlayAction(patch, action); err != nil {
				return nil, err
			}
		}
	}
	return patch, nil
}

// FindNearestToTs finds the last commit that is greater than or equal to ts.
func (s *Store) FindNearestToTs(ctx context.Context, tail uuid.UUID, ts nano.Ts) (uuid.UUID, error) {
	at := tail
	var prev uuid.UUID
	for {
		_, commit, err := s.GetBytes(ctx, at)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				// Vacated commit.
				return prev, nil
			}
			return uuid.Nil(), err
		}
		if ts >= commit.Date {
			return commit.ID, nil
		}
		prev = at
		at = commit.Parent
	}
}

// SetBase establishes a new base (snapshot) at the provided commit and resets
// the attached caches, then deletes all prior commits.
func (s *Store) SetBase(ctx context.Context, commit uuid.UUID) ([]uuid.UUID, error) {
	path, err := s.Path(ctx, commit)
	if err != nil {
		return nil, err
	}
	if len(path) <= 1 {
		return nil, errors.New("cannot set base on earliest commit")
	}
	// Create snapshot of previous commit.
	snap, err := s.buildSnapshot(ctx, path[1])
	if err != nil {
		return nil, err
	}
	if err := s.putBase(ctx, snap, path[1]); err != nil {
		return nil, err
	}
	s.cache.Purge()
	s.paths.Purge()
	s.snapshots.Purge()
	s.snapshots.Add(path[1], snap)
	return path[1:], s.deletePath(ctx, path[1:])
}

// DANGER ZONE - commits should only be removed once a new base has been
// established.
func (s *Store) deletePath(ctx context.Context, path []uuid.UUID) error {
	deleteIfExists := func(path *storage.URI) error {
		err := s.engine.Delete(ctx, path)
		if errors.Is(err, fs.ErrNotExist) {
			err = nil
		}
		return err
	}
	// Attempt to delete prior base (if it exists).
	_, tail, err := s.GetBytes(ctx, path[len(path)-1])
	if err == nil && tail.Parent != uuid.Nil() {
		deleteIfExists(s.basePathOf(tail.Parent))
	}
	group, ctx := errgroup.WithContext(ctx)
	group.SetLimit(runtime.GOMAXPROCS(0))
	for _, c := range path {
		group.Go(func() error {
			return deleteIfExists(s.pathOf(c))
		})
		group.Go(func() error {
			return deleteIfExists(s.snapshotPathOf(c))
		})
	}
	return group.Wait()
}

// Vacuumable returns the set of data.Objects in the path of leaf that are not referenced
// by the leaf's snapshot.
func (s *Store) Vacuumable(ctx context.Context, leaf uuid.UUID, out chan<- *data.Object) error {
	snap, err := s.Snapshot(ctx, leaf)
	if err != nil {
		return err
	}
	for at := leaf; at != uuid.Nil(); {
		o, err := s.Get(ctx, at)
		if err != nil {
			return nil
		}
		at = o.Parent
		if o.Commit == leaf {
			// skip the leaf commit.
			continue
		}
		for _, action := range o.Actions {
			switch a := action.(type) {
			case *Add:
				if !snap.Exists(a.Object.ID) {
					select {
					case out <- &a.Object:
					case <-ctx.Done():
					}
				}
			default:
				continue
			}
		}
	}
	return nil
}
