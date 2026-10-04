package db

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/segmentio/ksuid"
	"github.com/superdb/super"
	"github.com/superdb/super/compiler/parser"
	"github.com/superdb/super/db/branches"
	"github.com/superdb/super/db/commits"
	"github.com/superdb/super/db/data"
	"github.com/superdb/super/db/journal"
	"github.com/superdb/super/dbid"
	"github.com/superdb/super/pkg/plural"
	"github.com/superdb/super/pkg/storage"
	"github.com/superdb/super/runtime"
	"github.com/superdb/super/sio"
	"github.com/superdb/super/sup"
	"github.com/superdb/super/vector/vio"
)

const (
	maxCommitRetries  = 10
	maxMessageObjects = 10
)

var (
	ErrCommitFailed      = fmt.Errorf("exceeded max update attempts (%d) to branch tip: commit failed", maxCommitRetries)
	ErrInvalidCommitMeta = errors.New("cannot parse SUP string")
)

type Branch struct {
	branches.Config
	pool   *Pool
	engine storage.Engine
	//base   commits.View
}

func OpenBranch(ctx context.Context, config *branches.Config, engine storage.Engine, poolPath *storage.URI, pool *Pool) (*Branch, error) {
	return &Branch{
		Config: *config,
		pool:   pool,
		engine: engine,
	}, nil
}

func (b *Branch) Load(ctx context.Context, sctx *super.Context, r sio.Reader, author, message, meta string) (ksuid.KSUID, error) {
	w, err := NewWriter(ctx, sctx, b.pool)
	if err != nil {
		return ksuid.Nil, err
	}
	err = sio.CopyWithContext(ctx, w, r)
	if closeErr := w.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return ksuid.Nil, err
	}
	objects := w.Objects()
	if len(objects) == 0 {
		return ksuid.Nil, commits.ErrEmptyTransaction
	}
	if message == "" {
		message = loadMessage(objects)
	}
	appMeta, err := loadMeta(sctx, meta)
	if err != nil {
		return ksuid.Nil, err
	}
	// The load operation has only added new objects so we know its
	// safe to merge at the tip and there can be no conflicts
	// with other concurrent writers (except for updating the branch pointer
	// which is handled by Branch.commit)
	return b.commit(ctx, func(parent *branches.Config, retries int) (*commits.Object, error) {
		return commits.NewAddsObject(parent.Commit, retries, author, message, appMeta, objects), nil
	})
}

func loadMessage(objects []data.Object) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("loaded %d data object%s\n\n", len(objects), plural.Slice(objects, "s")))
	for k, o := range objects {
		b.WriteString("  ")
		b.WriteString(o.String())
		b.WriteByte('\n')
		if k >= maxMessageObjects {
			b.WriteString("  ...\n")
			break
		}
	}
	return b.String()
}

func loadMeta(sctx *super.Context, meta string) (super.Value, error) {
	if meta == "" {
		return super.Null, nil
	}
	val, err := sup.ParseValue(super.NewContext(), meta)
	if err != nil {
		return sctx.Missing(), fmt.Errorf("%w %q: %s", ErrInvalidCommitMeta, meta, err)
	}
	return val, nil
}

func (b *Branch) Delete(ctx context.Context, ids []ksuid.KSUID, author, message string) (ksuid.KSUID, error) {
	return b.commit(ctx, func(parent *branches.Config, retries int) (*commits.Object, error) {
		snap, err := b.pool.commits.Snapshot(ctx, parent.Commit)
		if err != nil {
			return nil, err
		}
		var objects []*data.Object
		for _, id := range ids {
			o, err := snap.Lookup(id)
			if err != nil {
				return nil, err
			}
			objects = append(objects, o)
		}
		if message == "" {
			var b strings.Builder
			fmt.Fprintf(&b, "deleted %d data object%s\n\n", len(objects), plural.Slice(objects, "s"))
			printObjects(&b, objects, maxMessageObjects)
			message = b.String()
		}
		return commits.NewDeletesObject(parent.Commit, retries, author, message, ids), nil
	})
}

func (b *Branch) DeleteWhere(ctx context.Context, c runtime.Compiler, ast *parser.AST, author, message, meta string) (ksuid.KSUID, error) {
	sctx := super.NewContext()
	appMeta, err := loadMeta(sctx, meta)
	if err != nil {
		return ksuid.Nil, err
	}
	return b.commit(ctx, func(parent *branches.Config, retries int) (*commits.Object, error) {
		rctx := runtime.NewContext(ctx, sctx)
		defer rctx.Cancel()
		// XXX It would be great to not do this since and just pass the snapshot
		// into c.NewDeleteQuery since we have to load the snapshot later
		// anyways. Unfortunately there's quite a few layers of plumbing needed
		// to get this working in compiler.
		committish := &dbid.Committish{
			Pool:   b.pool.Name,
			Branch: parent.Commit.String(),
		}
		query, err := c.NewDeleteQuery(rctx, ast, committish)
		if err != nil {
			return nil, err
		}
		defer query.Pull(true)
		w, err := NewWriter(ctx, sctx, b.pool)
		if err != nil {
			return nil, err
		}
		err = vio.Copy(w, query)
		if closeErr := w.Close(); err == nil {
			err = closeErr
		}
		if err != nil {
			return nil, err
		}
		base, err := b.pool.commits.Snapshot(ctx, parent.Commit)
		if err != nil {
			return nil, err
		}
		deleted := query.DeletionSet()
		if len(deleted) == 0 {
			return nil, commits.ErrEmptyTransaction
		}
		patch := commits.NewPatch(base)
		for _, oid := range deleted {
			patch.DeleteObject(oid)
		}
		for _, o := range w.Objects() {
			patch.AddDataObject(&o)
		}
		if message == "" {
			var deletedObjs []*data.Object
			for _, id := range deleted {
				o, _ := base.Lookup(id)
				deletedObjs = append(deletedObjs, o)
			}
			var added []*data.Object
			for _, o := range w.Objects() {
				added = append(added, &o)
			}
			message = deleteWhereMessage(deletedObjs, added)
		}
		return patch.NewCommitObject(parent.Commit, retries, author, message, appMeta), nil
	})
}

func deleteWhereMessage(deleted, added []*data.Object) string {
	var b strings.Builder
	fmt.Fprintf(&b, "deleted %d data object%s\n\n", len(deleted), plural.Slice(deleted, "s"))
	printObjects(&b, deleted, maxMessageObjects)
	if len(added) > 0 {
		fmt.Fprintf(&b, "\nadded %d data object%s\n\n", len(added), plural.Slice(added, "s"))
		printObjects(&b, added, maxMessageObjects-len(deleted))
	}
	return b.String()
}

func printObjects(b *strings.Builder, objects []*data.Object, maxMessageObjects int) {
	for k, o := range objects {
		fmt.Fprintf(b, "  %s\n", o)
		if k >= maxMessageObjects {
			b.WriteString("  ...\n")
		}
	}
}

func (b *Branch) Revert(ctx context.Context, commit ksuid.KSUID, author, message string) (ksuid.KSUID, error) {
	return b.commit(ctx, func(parent *branches.Config, retries int) (*commits.Object, error) {
		patch, err := b.pool.commits.PatchOfCommit(ctx, commit)
		if err != nil {
			return nil, fmt.Errorf("commit not found: %s", commit)
		}
		tip, err := b.pool.commits.Snapshot(ctx, parent.Commit)
		if err != nil {
			return nil, err
		}
		if message == "" {
			message = fmt.Sprintf("reverted commit %s", commit)
		}
		object, err := patch.Revert(tip, ksuid.New(), parent.Commit, retries, author, message)
		if err != nil {
			return nil, err
		}
		return object, nil
	})
}

func (b *Branch) CommitCompact(ctx context.Context, src, rollup []*data.Object, author, message, meta string) (ksuid.KSUID, error) {
	if len(rollup) < 1 {
		return ksuid.Nil, errors.New("compact: one or more rollup objects required")
	}
	sctx := super.NewContext()
	appMeta, err := loadMeta(sctx, meta)
	if err != nil {
		return ksuid.Nil, err
	}
	return b.commit(ctx, func(parent *branches.Config, retries int) (*commits.Object, error) {
		base, err := b.pool.commits.Snapshot(ctx, parent.Commit)
		if err != nil {
			return nil, err
		}
		patch := commits.NewPatch(base)
		for _, o := range rollup {
			if err := patch.AddDataObject(o); err != nil {
				return nil, err
			}
		}
		for _, o := range src {
			if err := patch.DeleteObject(o.ID); err != nil {
				return nil, err
			}
		}
		if message == "" {
			var b strings.Builder
			fmt.Fprintf(&b, "compacted %d object%s\n\n", len(src), plural.Slice(src, "s"))
			printObjects(&b, src, maxMessageObjects)
			fmt.Fprintf(&b, "\ncreated %d object%s\n\n", len(rollup), plural.Slice(rollup, "s"))
			printObjects(&b, rollup, maxMessageObjects-len(src))
			message = b.String()
		}
		commit := patch.NewCommitObject(parent.Commit, retries, author, message, appMeta)
		return commit, nil
	})
}

func (b *Branch) mergeInto(ctx context.Context, parent *Branch, author, message string) (ksuid.KSUID, error) {
	if b == parent {
		return ksuid.Nil, errors.New("cannot merge branch into itself")
	}
	return parent.commit(ctx, func(head *branches.Config, retries int) (*commits.Object, error) {
		return b.buildMergeObject(ctx, head, retries, author, message)
	})
	//XXX we should follow parent commit with a child rebase... do this
	// next... we want to fast forward the child to any pending commits
	// that happened on the child branch while we were merging into the parent.
	// and rebase the child branch to point at the parent where we grafted on
	// it's ok if new commits are arriving past the parent graft on point...
}

func (b *Branch) buildMergeObject(ctx context.Context, parent *branches.Config, retries int, author, message string) (*commits.Object, error) {
	childPath, err := b.pool.commits.Path(ctx, b.Commit)
	if err != nil {
		return nil, err
	}
	parentPath, err := b.pool.commits.Path(ctx, parent.Commit)
	if err != nil {
		return nil, err
	}
	baseID := commonAncestor(parentPath, childPath)
	if baseID == ksuid.Nil {
		//XXX this shouldn't happen because because all of the branches
		// should live in a single tree.
		//XXX hmm, except if you branch main when it is empty...?
		// we shoudl detect this and not allow it...?
		return nil, errors.New("system error: cannot locate common ancestor for branch merge")
	}
	// Compute the snapshot of the common ancestor then compute patches
	// along each branch and make sure the two patches do not have a
	// delete conflict.  For now, this is the only kind of merge update
	// conflict we detect.
	base, err := b.pool.commits.Snapshot(ctx, baseID)
	if err != nil {
		return nil, err
	}
	childPatch, err := b.pool.commits.PatchOfPath(ctx, base, baseID, b.Commit)
	if err != nil {
		return nil, err
	}
	parentPatch, err := b.pool.commits.PatchOfPath(ctx, base, baseID, parent.Commit)
	if err != nil {
		return nil, err
	}
	if message == "" {
		message = fmt.Sprintf("merged %q into %q", b.Name, parent.Name)
	}
	// Now compute the diff between the parent patch and the child patch so that
	// the diff patch will reflect the changes from the child into the parent.
	// Diff() will also check for delete conflicts.
	diff, err := commits.Diff(parentPatch, childPatch)
	if err != nil {
		return nil, fmt.Errorf("error merging %q into %q: %w", b.Name, parent.Name, err)
	}
	return diff.NewCommitObject(parent.Commit, retries, author, message, super.Null), nil
}

func commonAncestor(a, b []ksuid.KSUID) ksuid.KSUID {
	m := make(map[ksuid.KSUID]struct{})
	for _, id := range a {
		m[id] = struct{}{}
	}
	for _, id := range b {
		if _, ok := m[id]; ok {
			return id
		}
	}
	return ksuid.Nil
}

type constructor func(parent *branches.Config, retries int) (*commits.Object, error)

func (b *Branch) commit(ctx context.Context, create constructor) (ksuid.KSUID, error) {
	// A commit must append new state to the tip of the branch while simultaneously
	// upating the branch pointer in a trasactionally consistent fashion.
	// For example, if we compute a commit object based on a certain tip commit,
	// then commit that object after another writer commits in between,
	// the commit object may be inconsistent against the intervening commit.
	//
	// We do this update optimistically and ensure this consistency with
	// a loop that builds the commit object based on the presumed parent,
	// then moves the branch pointer to the new commit but, using a constraint,
	// only succeeds when the presumed parent is atomically consistent
	// with the branch update.  If the contraint, fails will loop a number
	// of times till it succeeds, or we give up.
	for retries := range maxCommitRetries {
		config, err := b.pool.branches.LookupByName(ctx, b.Name)
		if err != nil {
			return ksuid.Nil, err
		}
		object, err := create(config, retries)
		if err != nil {
			return ksuid.Nil, err
		}
		if err := b.pool.commits.Put(ctx, object); err != nil {
			return ksuid.Nil, fmt.Errorf("branch %q failed to write commit object: %w", b.Name, err)
		}
		// Set the branch pointer to point to this commit object
		// and stash the current commit (that will become the parent)
		// in a local for the constraint check closure.
		parent := config.Commit
		config.Commit = object.Commit
		parentCheck := func(e journal.Entry) bool {
			if entry, ok := e.(*branches.Config); ok {
				return entry.Commit == parent
			}
			return false
		}
		if err := b.pool.branches.Update(ctx, config, parentCheck); err != nil {
			// Branch update failed so remove commit.
			rmerr := b.pool.commits.Remove(ctx, object)
			if err == journal.ErrConstraint {
				// Parent check failed so try again.
				if rmerr != nil {
					return ksuid.Nil, rmerr
				}
				continue
			}
			return ksuid.Nil, err
		}
		return object.Commit, nil
	}
	return ksuid.Nil, fmt.Errorf("branch %q: %w", b.Name, ErrCommitFailed)
}

func (b *Branch) LookupTags(ctx context.Context, tags []ksuid.KSUID) ([]ksuid.KSUID, error) {
	var ids []ksuid.KSUID
	for _, tag := range tags {
		ok, err := b.pool.ObjectExists(ctx, tag)
		if err != nil {
			return nil, err
		}
		if ok {
			ids = append(ids, tag)
			continue
		}
		patch, err := b.pool.commits.PatchOfCommit(ctx, tag)
		if err != nil {
			continue
		}
		ids = append(ids, patch.DataObjects()...)
	}
	return ids, nil
}

func (b *Branch) Pool() *Pool {
	return b.pool
}
