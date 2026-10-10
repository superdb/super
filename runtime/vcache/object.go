package vcache

import (
	"errors"

	"github.com/superdb/super"
	"github.com/superdb/super/bsup"
	"github.com/superdb/super/bsup/loader"
)

// Object is the interface to load a given BSUP object from storage into
// memory and perform projections (or whole value reads) of the in-memory data.
// This is also suitable for one-pass use where the data is read on demand,
// used for processing, then discarded.  Objects maybe be persisted across
// multiple callers of Cache and the super.Context in use is passed in for
// each vector constructed from its in-memory shadow.
type Object struct {
	loaders []*loader.FrameLoader
}

func NewObject(sctx *super.Context, fit bsup.FrameIter) (*Object, error) {
	// XXX this stub just reads all the objects.  With a seekable,
	// we can read just the frame headers and page in when necessary.
	// We need another layer of abstraction which is uuid/slot to
	// model each frame in the object referenced by its uuid.
	var loaders []*loader.FrameLoader
	for {
		frame, err := fit.Next()
		if err != nil {
			return nil, err
		}
		if frame == nil {
			return &Object{loaders}, nil
		}
		colFrame, ok := frame.(*bsup.ColFrame)
		if !ok {
			return nil, errors.New("encountered non-column data")
		}
		loaders = append(loaders, loader.NewFrameLoader(sctx, colFrame))
	}
}

func (o *Object) Loaders() []*loader.FrameLoader {
	return o.loaders
}

func (o *Object) Release() error {
	panic("TBD")
}
