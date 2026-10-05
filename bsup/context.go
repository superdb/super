package bsup

import (
	"errors"
	"fmt"
	"io"
	"sync"

	"github.com/superdb/super"
)

type Context struct {
	mu     sync.Mutex
	local  *super.Context // holds the types for the Metadata values
	metas  []Metadata     // id to Metadata
	values []super.Value  // id to unmarshaled Metadata
	uctx   *super.Unmarshaler
	// The typedefs table is a merge of all the fusion vector subtypes.
	// Only the typedefs needed are recorded in this table and different vectors
	// are merged into this shared table by mapping each vector's IDs to the
	// shared IDs.  This is used by both the read path and write path.
	smu      sync.Mutex
	typedefs *super.TypeDefs
	// The subtypesReader holds a pointer to a reader to load the typedefs
	// bytes if they are ever needed.  If we do read them, we read them once
	// into the subtypes table under lock smu and clear this reader value to
	// mark the table loaded.
	subtypesReader io.Reader
	subtypesSize   int64
}

type ID uint32

func NewContext() *Context {
	return &Context{local: super.NewContext()}
}

func (c *Context) enter(meta Metadata) ID {
	id := ID(len(c.metas))
	c.metas = append(c.metas, meta)
	return id
}

func (c *Context) TypeDefs() *super.TypeDefs {
	c.smu.Lock()
	defer c.smu.Unlock()
	if c.typedefs == nil {
		c.typedefs = super.NewTypeDefs()
	}
	return c.typedefs
}

func (c *Context) Lookup(id ID) Metadata {
	if id >= ID(len(c.metas)) {
		panic(fmt.Sprintf("bsup.Context ID (%d) out of range (len %d)", id, len(c.values)))
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.metas[id] == nil {
		if err := c.unmarshal(id); err != nil {
			panic(err) //XXX
		}
	}
	return c.metas[id]
}

func (c *Context) unmarshal(id ID) error {
	if c.uctx == nil {
		c.uctx = super.NewUnmarshaler()
		c.uctx.SetContext(c.local)
		c.uctx.Bind(Template...)
	}
	if c.metas[id] != nil {
		return nil
	}
	return c.uctx.Unmarshal(c.values[id], &c.metas[id])
}

func (c *Context) readMeta(r io.ReaderAt) error {
	fit := NewSeekable(c.local, r)
	for {
		frame, err := fit.Next()
		if err != nil {
			return err
		}
		if frame == nil {
			// initialize meta slots with empty values so they are
			// unmarshaled on demand from c.values.
			c.metas = make([]Metadata, len(c.values))
			return nil
		}
		// metas always stored as rows (else infinite recursion)
		rowframe, ok := frame.(*RowFrame)
		if !ok {
			return errors.New("encountered non-row data in column metadata")
		}
		vals, err := rowframe.DeserializeValues()
		if err != nil {
			return err
		}
		// Need not copy the value bytes as the rowframe buffer will never
		// be overwritten and is instead GC'd when we're done.
		c.values = append(c.values, vals...)
	}
}

// LoadSubtypes is called to load the subtypes table on demand,
// only when needed.  It must be called before calling LookupTypeVal.
func (c *Context) LoadSubtypes() *super.TypeDefs {
	c.smu.Lock()
	defer c.smu.Unlock()
	if c.subtypesReader != nil {
		if err := c.readSubTypes(c.subtypesReader); err != nil {
			// Panic for now but we should handle this more gracefully
			// when an IO error causes failure of a running query.
			panic(err)
		}
		c.subtypesReader = nil
	}
	return c.typedefs
}

func (c *Context) readSubTypes(r io.Reader) error {
	bytes := make([]byte, c.subtypesSize)
	if _, err := io.ReadFull(r, bytes); err != nil {
		return fmt.Errorf("load subtypes failed: %w", err)
	}
	defs, ok := super.NewTypeDefsFromBytes(bytes)
	if !ok {
		return errors.New("metadata typedefs has invalid format")
	}
	c.typedefs = defs
	return nil
}
