package commits

import (
	"errors"
	"fmt"

	"github.com/segmentio/ksuid"
	"github.com/superdb/super"
	"github.com/superdb/super/db/data"
	"github.com/superdb/super/order"
	"github.com/superdb/super/runtime/sam/expr/extent"
)

// A Patch represents a difference between a base snapshot and the patched
// snapshot.  Patch implements View so either a patch or a base snapshot
// can be traversed in the same manner.  Furthermore, patches can be easily
// chained to implement a sequence of patches to a base snapshot.
type Patch struct {
	base           View
	diff           *Snapshot
	deletedObjects []ksuid.KSUID
}

var _ View = (*Patch)(nil)
var _ Writeable = (*Patch)(nil)

func NewPatch(base View) *Patch {
	return &Patch{
		base: base,
		diff: NewSnapshot(),
	}
}

func (p *Patch) Lookup(id ksuid.KSUID) (*data.Object, error) {
	if s, err := p.diff.Lookup(id); err == nil {
		return s, nil
	}
	return p.base.Lookup(id)
}

func (p *Patch) Select(span extent.Span, o order.Which) DataObjects {
	objects := p.base.Select(span, o)
	objects.Append(p.diff.Select(span, o))
	return objects
}

func (p *Patch) SelectAll() DataObjects {
	objects := p.base.SelectAll()
	objects.Append(p.diff.SelectAll())
	return objects
}

func (p *Patch) DataObjects() []ksuid.KSUID {
	var ids []ksuid.KSUID
	for _, dataObject := range p.diff.SelectAll() {
		ids = append(ids, dataObject.ID)
	}
	return ids
}

func (p *Patch) AddDataObject(object *data.Object) error {
	if Exists(p.base, object.ID) {
		return ErrExists
	}
	return p.diff.AddDataObject(object)
}

func (p *Patch) DeleteObject(id ksuid.KSUID) error {
	if p.diff.Exists(id) {
		return p.diff.DeleteObject(id)
	}
	if !Exists(p.base, id) {
		return ErrNotFound
	}
	// Keep track of the deletions from the base so we can add the
	// needed delete Actions when building the transaction patch.
	p.deletedObjects = append(p.deletedObjects, id)
	return nil
}

func (p *Patch) NewCommitObject(parent ksuid.KSUID, retries int, author, message string, meta super.Value) *Object {
	o := NewObject(parent, author, message, meta, retries)
	for _, id := range p.deletedObjects {
		o.appendDelete(id)
	}
	for _, s := range p.diff.objects {
		o.appendAdd(s)
	}
	return o
}

func (p *Patch) Revert(tip *Snapshot, commit, parent ksuid.KSUID, retries int, author, message string) (*Object, error) {
	object := NewObject(parent, author, message, super.Null, retries)
	// For each data object that is added in the patch and is also in the tip, we do a delete.
	for _, dataObject := range p.diff.SelectAll() {
		if Exists(tip, dataObject.ID) {
			object.appendDelete(dataObject.ID)
		}
	}
	// For each delete in the patch that is absent in the tip, we do an add.
	for _, id := range p.deletedObjects {
		// Reach back to get the object before it was deleted in this patch.
		dataObject, err := p.base.Lookup(id)
		if err != nil {
			return nil, err
		}
		if dataObject == nil {
			return nil, fmt.Errorf("corrupt snapshot: ID %s is in patch's deleted objects but not in patch's base snapshot", id)
		}
		if !Exists(tip, dataObject.ID) {
			object.appendAdd(dataObject)
		}
	}
	if len(object.Actions) == 1 {
		return nil, errors.New("revert commit is empty")
	}
	return object, nil
}

func Diff(parent, child *Patch) (*Patch, error) {
	var dirty bool
	p := NewPatch(parent)
	deletedObjects := make(map[ksuid.KSUID]struct{})
	for _, id := range child.deletedObjects {
		deletedObjects[id] = struct{}{}
	}
	// For each object in the child patch that isn't in the parent, create an add,
	// unless the parent deletes it, then return an error.
	for _, o := range child.SelectAll() {
		if !Exists(parent, o.ID) {
			if _, ok := deletedObjects[o.ID]; ok {
				return nil, fmt.Errorf("parent branch deletes object that child branch adds: %d", o.ID)
			}
			if err := p.AddDataObject(o); err != nil {
				return nil, err
			}
			dirty = true
		}
	}
	// For each delete in the child patch, create a delete.
	// If the object doesn't exist in the parent, then we have a
	// delete conflict.
	for _, id := range child.deletedObjects {
		if Exists(parent, id) {
			if err := p.DeleteObject(id); err != nil {
				return nil, err
			}
			dirty = true
		} else {
			return nil, fmt.Errorf("delete conflict: %s", id)
		}
	}
	if !dirty {
		return nil, errors.New("difference is empty")
	}
	return p, nil
}
