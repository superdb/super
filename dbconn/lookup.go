package dbconn

import (
	"context"
	"fmt"
	"strings"
	"uuid"

	"github.com/superdb/super"
	"github.com/superdb/super/compiler/srcfiles"
	"github.com/superdb/super/db"
	"github.com/superdb/super/db/commits"
	"github.com/superdb/super/db/tables"
	"github.com/superdb/super/dbid"
	"github.com/superdb/super/sbuf"
	"github.com/superdb/super/sio"
	"github.com/superdb/super/vector"
	"github.com/superdb/super/vector/vio"
)

func IsRemote(u string) bool {
	return strings.HasPrefix(u, "http://") || strings.HasPrefix(u, "https://")
}

func LookupTableByName(ctx context.Context, db Conn, name string) (*tables.Config, error) {
	b := newBuffer(tables.Config{})
	query := fmt.Sprintf("from :tables | name == '%s'", name)
	q, err := db.Query(ctx, srcfiles.Plain(query))
	if err != nil {
		return nil, err
	}
	defer q.Close()
	defer q.Pull(true)
	if err := vio.Copy(b, q); err != nil {
		return nil, err
	}
	switch len(b.results) {
	case 0:
		return nil, fmt.Errorf("%q: table not found", name)
	case 1:
		table, ok := b.results[0].(*tables.Config)
		if !ok {
			return nil, fmt.Errorf("internal error: table record has wrong type: %T", b.results[0])
		}
		return table, nil
	default:
		return nil, fmt.Errorf("internal error: multiple tables found with same name: %s", name)
	}
}

func GetTables(ctx context.Context, db Conn) ([]*tables.Config, error) {
	b := newBuffer(tables.Config{})
	q, err := db.Query(ctx, srcfiles.Plain("from :tables"))
	if err != nil {
		return nil, err
	}
	defer q.Close()
	defer q.Pull(true)
	if err := vio.Copy(b, q); err != nil {
		return nil, err
	}
	var pls []*tables.Config
	for _, r := range b.results {
		pls = append(pls, r.(*tables.Config))
	}
	return pls, nil
}

func LookupTableByID(ctx context.Context, db Conn, id uuid.UUID) (*tables.Config, error) {
	b := newBuffer(tables.Config{})
	query := fmt.Sprintf("from :tables | id == %s", id)
	q, err := db.Query(ctx, srcfiles.Plain(query))
	if err != nil {
		return nil, err
	}
	defer q.Close()
	defer q.Pull(true)
	if err := vio.Copy(b, q); err != nil {
		return nil, err
	}
	switch len(b.results) {
	case 0:
		return nil, fmt.Errorf("%s: table not found", id)
	case 1:
		table, ok := b.results[0].(*tables.Config)
		if !ok {
			return nil, fmt.Errorf("internal error: table record has wrong type: %T", b.results[0])
		}
		return table, nil
	default:
		return nil, fmt.Errorf("internal error: multiple tabled found with same id: %s", id)
	}
}

func LookupBranchByID(ctx context.Context, api Conn, id uuid.UUID) (*db.BranchMeta, error) {
	b := newBuffer(db.BranchMeta{})
	query := fmt.Sprintf("from :branches | branch.id == %s", id)
	q, err := api.Query(ctx, srcfiles.Plain(query))
	if err != nil {
		return nil, err
	}
	defer q.Close()
	defer q.Pull(true)
	if err := vio.Copy(b, q); err != nil {
		return nil, err
	}
	switch len(b.results) {
	case 0:
		return nil, fmt.Errorf("%s: branch not found", id)
	case 1:
		branch, ok := b.results[0].(*db.BranchMeta)
		if !ok {
			return nil, fmt.Errorf("internal error: branch record has wrong type: %T", b.results[0])
		}
		return branch, nil
	default:
		return nil, fmt.Errorf("internal error: multiple branches found with same id: %s", id)
	}
}

func GetCommit(ctx context.Context, db Conn, tableName, revision string) (*commits.Commit, error) {
	tableID, err := db.TableID(ctx, tableName)
	if err != nil {
		return nil, err
	}
	commit, err := dbid.ParseID(revision)
	if err != nil {
		// LookupBranchByName(ctx. XXX?
		if commit, err = db.CommitObject(ctx, tableID, revision); err != nil {
			return nil, err
		}
	}
	b := newBuffer(commits.Commit{})
	query := fmt.Sprintf("from %q:log | where id == %s", tableID, commit)
	q, err := db.Query(ctx, srcfiles.Plain(query))
	if err != nil {
		return nil, err
	}
	defer q.Close()
	defer q.Pull(true)
	if err := vio.Copy(b, q); err != nil {
		return nil, err
	}
	switch len(b.results) {
	case 0:
		return nil, fmt.Errorf("%s: commit not found", commit)
	case 1:
		commit, ok := b.results[0].(*commits.Commit)
		if !ok {
			return nil, fmt.Errorf("internal error: branch record has wrong type: %T", b.results[0])
		}
		return commit, nil
	default:
		return nil, fmt.Errorf("internal error: multiple commits found with same id: %s", commit)
	}
}

type buffer struct {
	unmarshaler *super.Unmarshaler
	results     []any
}

var _ sio.Writer = (*buffer)(nil)

func newBuffer(types ...any) *buffer {
	u := super.NewUnmarshaler()
	u.Bind(types...)
	u.SetContext(super.NewContext())
	return &buffer{unmarshaler: u}
}

func (b *buffer) Push(vec vector.Any) error {
	return sbuf.WriteVec(b, vec)
}

func (b *buffer) Write(val super.Value) error {
	var v any
	if err := b.unmarshaler.Unmarshal(val, &v); err != nil {
		return err
	}
	b.results = append(b.results, v)
	return nil
}
