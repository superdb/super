package oldbsup

import (
	"errors"
	"fmt"
	"sync"

	"github.com/superdb/super"
)

type Encoder struct {
	defs *super.TypeDefs
	off  int
}

func NewEncoder() *Encoder {
	return &Encoder{
		defs: super.NewTypeDefs(),
	}
}

func (e *Encoder) Reset() {
	e.defs.Reset()
	e.off = 0
}

func (e *Encoder) Flush() {
	e.off = len(e.defs.Bytes())
}

func (e *Encoder) Len() int {
	return len(e.defs.Bytes()) - e.off
}

func (e *Encoder) nextBuffer() []byte {
	bytes := e.defs.Bytes()
	b := bytes[e.off:]
	e.off = len(bytes)
	return b
}

// Encode takes a type from outside this context and constructs a type from
// inside this context and emits BSUP typedefs for any type needed to construct
// the new type into the buffer provided.
func (e *Encoder) Encode(external super.Type) uint32 {
	// In general, a type can have a forward reference to another named type
	// that isn't defined here yet, so we need to recursively bind/patch
	// named types in the local type context, which a mapper can do but it
	// needs the original type context (actually just the original typedefs
	// table from the query sctx)
	return e.defs.LookupType(external)
}

type Decoder struct {
	sctx   *super.Context
	defs   *super.TypeDefs
	mapper *super.TypeDefsMapper
	// Local type IDs are mapped to the shared-context types with the types array.
	// The types slice is protected with mutex as the slice can be expanded while
	// worker threads are scanning earlier batches.
	mu    sync.RWMutex
	types []super.Type
}

var _ super.TypeFetcher = (*Decoder)(nil)

func NewDecoder(sctx *super.Context) *Decoder {
	defs := super.NewTypeDefs()
	return &Decoder{
		sctx:   sctx,
		defs:   defs,
		mapper: super.NewTypeDefsMapper(sctx, defs),
	}
}

func (d *Decoder) decode(b *buffer) error {
	before := d.defs.NTypes()
	if ok := d.defs.AppendBytes(b.Bytes()); !ok {
		return errors.New("corrupt BSUP types frame")
	}
	after := d.defs.NTypes()
	for id := before; id < after; id++ {
		typ := d.mapper.LookupType(uint32(id + super.IDTypeComplex))
		if typ == nil {
			return errors.New("corrupt BSUP types frame")
		}
		// Even though type decoding is single threaded, workers processing a
		// previous batch can be accessing the types map (via LookupType) while
		// the single thread is extending it so these accesses are protected
		// with the mutex.
		d.mu.Lock()
		d.types = append(d.types, typ)
		d.mu.Unlock()
	}
	return nil
}

func (d *Decoder) LookupType(id int) (super.Type, error) {
	if id < super.IDTypeComplex {
		return super.LookupPrimitiveByID(id)
	}
	d.mu.RLock()
	defer d.mu.RUnlock()
	off := id - super.IDTypeComplex
	if off < len(d.types) {
		return d.types[off], nil
	}
	return nil, fmt.Errorf("no type found for type id %d", id)
}
