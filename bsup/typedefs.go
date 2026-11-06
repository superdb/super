package bsup

import (
	"errors"
	"fmt"
	"io"
	"sync"

	"github.com/superdb/super"
)

// TypeDefs holds the typedefs table of a column frame for the types of type
// values (e.g., the subtypes of fusion vectors and any vector.TypeValue types),
// as opposed to the frame's vector typedefs table, which holds the types of the
// frame's vectors (see ColFrame).
type TypeDefs struct {
	// The typedefs table is a merge of all the fusion vector subtypes and any
	// vector TypeValue types.  These types are loaded and decoded lazily so the
	// only overhead in merging everything is in loading the bytes buffer into
	// memory.  In fact, the advnatage of merging everything is the savings attained
	// by sharing type defs across all these uses.
	mu       sync.Mutex
	encoder  *super.TypeDefsEncoder
	typedefs *super.TypeDefs
	// The reader is non-nil when the typedef bytes haven't been loaded into memory.
	// If we never use them, they are never read. Once the buffer is loaded (under mutex),
	// we clear the reader to indicate the load is done.
	reader       io.Reader
	subtypesSize int64
}

func NewTypeDefs() *TypeDefs {
	return &TypeDefs{}
}

// Encoder returns the typedefs table being built on the write path.
func (t *TypeDefs) Encoder() *super.TypeDefsEncoder {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.encoder == nil {
		t.encoder = super.NewTypeDefsEncoder()
	}
	return t.encoder
}

// LoadSubtypes is called to load the subtypes table on demand,
// only when needed.
func (t *TypeDefs) LoadSubtypes() *super.TypeDefs {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.reader != nil {
		if err := t.readSubTypes(t.reader); err != nil {
			// Panic for now but we should handle this more gracefully
			// when an IO error causes failure of a running query.
			panic(err)
		}
		t.reader = nil
	}
	return t.typedefs
}

func (t *TypeDefs) readSubTypes(r io.Reader) error {
	bytes := make([]byte, t.subtypesSize)
	if _, err := io.ReadFull(r, bytes); err != nil {
		return fmt.Errorf("load subtypes failed: %w", err)
	}
	defs, ok := super.NewTypeDefsFromBytes(bytes)
	if !ok {
		return errors.New("malformed BSUP typedefs section")
	}
	t.typedefs = defs
	return nil
}
