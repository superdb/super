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
	DefaultObjectCap = 1024 * 1024
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

var fileRegex = regexp.MustCompile(`([0-9A-Za-z]{27}-(data|meta)).bsup$`)

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
// of values sorted according to the table's sort order where From is the
// the first value in the sequence and To is the last value.  Count is the number
// of values in the sequence and Size is total size in bytes of the Object as
// persisted to storage (i.e., its compressed size).  Type is the fused type
// of all values in the object, or nil if unknown (e.g., the object predates
// this field).  Type may belong to any type context (e.g., the one used to
// unmarshal the object), so use super.Context.TranslateType before mixing it
// with types from another context.
type Object struct {
	ID    uuid.UUID   `super:"id"`
	Type  super.Type  `super:"type"`
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

// FusedType returns the fused type of the values in the objects interpreted
// in sctx.  It returns nil if objects is empty or if any object has no type,
// since in either case the type of the collection cannot be known statically.
func FusedType(sctx *super.Context, objects []*Object) (super.Type, error) {
	if len(objects) == 0 {
		return nil, nil
	}
	fuser := super.NewFuser(sctx)
	// Avoid the type translation across objects.  Probably not a big deal
	// but will help a bit for large object scans.
	seen := make(map[super.Type]struct{})
	for _, o := range objects {
		if o.Type == nil {
			return nil, nil
		}
		if _, ok := seen[o.Type]; ok {
			continue
		}
		seen[o.Type] = struct{}{}
		typ, err := sctx.TranslateType(o.Type)
		if err != nil {
			return nil, fmt.Errorf("data object %s: %w", o.ID, err)
		}
		fuser.Fuse(typ)
	}
	return fuser.FusedType(), nil
}

func NewObject() Object {
	none := super.NewValue(super.TypeMissing, nil)
	return Object{ID: uuid.NewV7(), Min: none, Max: none}
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
