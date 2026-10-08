package data

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"regexp"
	"uuid"

	"github.com/superdb/super"
	"github.com/superdb/super/order"
	"github.com/superdb/super/pkg/storage"
	"github.com/superdb/super/runtime/sam/expr/extent"
)

const (
	DefaultThreshold = 500 * 1024 * 1024
)

// A FileKind is the first part of a file name, used to differentiate files
// when they are listed from the archive's backing store.
type FileKind string

const (
	FileKindUnknown  FileKind = ""
	FileKindData     FileKind = "data"
	FileKindMetadata FileKind = "meta"
)

func (k FileKind) Description() string {
	switch k {
	case FileKindData:
		return "data"
	case FileKindMetadata:
		return "metadata"
	default:
		return "unknown"
	}
}

var fileRegex = regexp.MustCompile(`([0-9A-Za-z]{27}-(data|meta)).bsuprows$`)

// XXX this won't work right until we integrate segID
func FileMatch(s string) (kind FileKind, id uuid.UUID, ok bool) {
	match := fileRegex.FindStringSubmatch(s)
	if match == nil {
		return
	}
	k := FileKind(match[1])
	switch k {
	case FileKindData:
	case FileKindMetadata:
	default:
		return
	}
	id, err := uuid.Parse(match[2])
	if err != nil {
		return
	}
	return k, id, true
}

// An Object represents a cloud object or file that holds an ordered sequence
// of values sorted according to the pool's data order where From is the
// the first value in the sequence and To is the last value.  Count is the number
// of values in the sequence and Size is total size in bytes of the Object as
// persisted to storage (i.e., its compressed size).
type Object struct {
	ID    uuid.UUID   `super:"id"`
	Min   super.Value `super:"min"`
	Max   super.Value `super:"max"`
	Count uint64      `super:"count"`
	Size  int64       `super:"size"`
}

func (o Object) IsZero() bool {
	return o.ID == uuid.UUID{}
}

func (o Object) String() string {
	return fmt.Sprintf("%s %d value%s in %d data bytes", o.ID, o.Count, plural(int(o.Count)), o.Size)
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

func (o *Object) Equal(to *Object) bool {
	return o.ID == to.ID
}

func NewObject() Object {
	return Object{ID: uuid.NewV7()}
}

func (o Object) Span(order order.Which) *extent.Generic {
	return extent.NewGenericFromOrder(o.Min, o.Max, order)
}

// ObjectPrefix returns a prefix for the various objects that comprise
// a data object so they can all be deleted with the storage engine's
// DeleteByPrefix method.
func (o Object) ObjectPrefix(path *storage.URI) *storage.URI {
	return path.JoinPath(o.ID.String())
}

func (o Object) URI(path *storage.URI) *storage.URI {
	return URI(path, o.ID)
}

func URI(path *storage.URI, id uuid.UUID) *storage.URI {
	return path.JoinPath(fmt.Sprintf("%s.bsup", id))
}

// Remove deletes the object.
// Any 'not found' errors are ignored.
func (o Object) Remove(ctx context.Context, engine storage.Engine, path *storage.URI) error {
	if err := engine.DeleteByPrefix(ctx, o.ObjectPrefix(path)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}
