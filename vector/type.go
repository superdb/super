package vector

import (
	"fmt"
	"sync"

	"github.com/superdb/super"
	"github.com/superdb/super/scode"
)

// TypeValue is a vector of type values.  A TypeValue is either local, where
// each type value is its type ID in the query context sctx, or foreign, where
// each type value is a type ID in a foreign TypeDefs table (e.g., from BSUP)
// that's mapped to a type ID in sctx on demand (see ID), so only the types
// that are used are created in sctx.  A foreign TypeValue created with a
// TypesLoader loads its table and IDs only when needed.
type TypeValue struct {
	mu   sync.Mutex
	sctx *super.Context
	// loader provides the foreign typedefs and IDs of a foreign TypeValue,
	// which are loaded into tmap and ids when needed.
	loader TypesLoader
	// tmap is the mapping of a foreign TypeValue's table (and is nil for a
	// local TypeValue).
	tmap *super.TypeIDMap
	// ids are the type IDs of the type values, which are foreign IDs if tmap
	// isn't nil and otherwise type IDs in sctx.
	ids []uint32
	// local are the type IDs in sctx of a foreign TypeValue (see IDs).
	local []uint32
}

// TypesLoader is an interface to load types as IDs in a TypeDefs table, along
// with the mapping of the table's IDs to the query context, so they do not
// pollute the query type context and are converted only when needed.  For
// example, the BSUP reader reads the typedefs table only when there is a
// runtime call to do so, and the BSUP writer loads types as TypeDefs for
// merging and writing to metadata without ever needing to create any
// super.Types.  The users of a table (e.g., the type vectors of a BSUP frame)
// should share its TypeIDMap so each type is mapped once.
type TypesLoader interface {
	Load() (*super.TypeIDMap, []uint32)
}

var _ Any = (*TypeValue)(nil)

// NewTypeValue returns a local TypeValue of types, which are translated into
// sctx if they're from another context.
func NewTypeValue(sctx *super.Context, types []super.Type) *TypeValue {
	t := &TypeValue{sctx: sctx, ids: make([]uint32, 0, len(types))}
	for _, typ := range types {
		t.Append(typ)
	}
	return t
}

// NewTypeValueFromIDs returns a local TypeValue of the types in sctx with
// type IDs ids.
func NewTypeValueFromIDs(sctx *super.Context, ids []uint32) *TypeValue {
	return &TypeValue{sctx: sctx, ids: ids}
}

// NewTypeValueWithLoader returns a foreign TypeValue whose table and IDs are
// provided by loader.
func NewTypeValueWithLoader(sctx *super.Context, loader TypesLoader) *TypeValue {
	return &TypeValue{sctx: sctx, loader: loader}
}

func NewTypeValueEmpty(sctx *super.Context) *TypeValue {
	return &TypeValue{sctx: sctx, ids: []uint32{}}
}

// Append appends typ to a local TypeValue, translating typ into sctx if it's
// from another context.
func (t *TypeValue) Append(typ super.Type) {
	if t.loader != nil || t.tmap != nil {
		panic("vector.TypeValue: Append on a foreign TypeValue")
	}
	id, err := t.sctx.LocalTypeID(typ)
	if err != nil {
		panic(err)
	}
	t.ids = append(t.ids, id)
}

func (*TypeValue) Kind() Kind {
	return KindType
}

func (t *TypeValue) Type() super.Type {
	return super.TypeType
}

func (t *TypeValue) Len() uint32 {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.load()
	return uint32(len(t.ids))
}

// load loads a foreign TypeValue's table and IDs from its loader, if any.
// It must be called with t.mu held.
func (t *TypeValue) load() {
	if t.loader != nil {
		t.tmap, t.ids = t.loader.Load()
		t.loader = nil
	}
}

// IsForeign reports whether t's type values are IDs in a foreign TypeDefs
// table (see TypesLoader) rather than type IDs in sctx.
func (t *TypeValue) IsForeign() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.loader != nil || t.tmap != nil
}

// ID returns the type ID in sctx of the type value at slot, mapping it from
// its foreign ID if t is foreign.
func (t *TypeValue) ID(slot uint32) uint32 {
	t.mu.Lock()
	t.load()
	id, tmap := t.ids[slot], t.tmap
	t.mu.Unlock()
	if tmap != nil {
		return mapID(tmap, id)
	}
	return id
}

// IDs returns the type IDs in sctx of the type values, mapping them from
// their foreign IDs if t is foreign.  Each distinct foreign ID is mapped
// once.
func (t *TypeValue) IDs() []uint32 {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.load()
	if t.tmap == nil {
		return t.ids
	}
	if t.local == nil {
		local := make([]uint32, len(t.ids))
		for k, id := range t.ids {
			local[k] = mapID(t.tmap, id)
		}
		t.local = local
	}
	return t.local
}

// Value returns the type at slot.
func (t *TypeValue) Value(slot uint32) super.Type {
	return t.lookup(t.ID(slot))
}

func (t *TypeValue) lookup(id uint32) super.Type {
	typ, err := t.sctx.LookupType(int(id))
	if err != nil {
		panic(err)
	}
	return typ
}

func (t *TypeValue) Sctx() *super.Context {
	return t.sctx
}

// Types returns the types of the type values.
func (t *TypeValue) Types() []super.Type {
	ids := t.IDs()
	types := make([]super.Type, len(ids))
	for k, id := range ids {
		types[k] = t.lookup(id)
	}
	return types
}

// TypeIDs returns the type values as IDs of types in a TypeDefs table, which
// is the foreign table if t is foreign and otherwise a snapshot of sctx's
// table, in which the IDs are type IDs.  The IDs are not mapped into sctx.
func (t *TypeValue) TypeIDs() (*super.TypeDefs, []uint32) {
	t.mu.Lock()
	t.load()
	tmap, ids := t.tmap, t.ids
	t.mu.Unlock()
	if tmap != nil {
		return tmap.TypeDefs(), ids
	}
	if t.sctx == nil {
		// An empty TypeValue built from no vectors has no context.
		return new(super.TypeDefs), ids
	}
	return t.sctx.TypeDefsSnapshot(), ids
}

// RawIDs returns the IDs of the type values in t's table (see TypeIDs)
// without getting the table, which for a local TypeValue would take a
// snapshot of sctx's table.  The IDs are not mapped into sctx.
func (t *TypeValue) RawIDs() []uint32 {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.load()
	return t.ids
}

// WithTypeIDs returns a TypeValue of the type values with IDs ids in the
// table of t (see TypeIDs), e.g., a subset of t's IDs.  A foreign TypeValue's
// result shares its table and mapping.
func (t *TypeValue) WithTypeIDs(ids []uint32) *TypeValue {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.load()
	return &TypeValue{sctx: t.sctx, tmap: t.tmap, ids: ids}
}

// Pick returns a TypeValue of the type values of t at the slots in index.
func (t *TypeValue) Pick(index []uint32) *TypeValue {
	ids := t.RawIDs()
	out := make([]uint32, len(index))
	for k, slot := range index {
		out[k] = ids[slot]
	}
	return t.WithTypeIDs(out)
}

func (t *TypeValue) Serialize(b *scode.Builder, slot uint32) {
	b.Append(super.EncodeTypeValue(t.Value(slot)))
}

func TypeValueValue(val Any, slot uint32) super.Type {
	switch val := val.(type) {
	case *TypeValue:
		return val.Value(slot)
	case *Const:
		return TypeValueValue(val.Any, 0)
	case *Dict:
		slot = uint32(val.Index[slot])
		return val.Any.(*TypeValue).Value(slot)
	case *View:
		slot = val.Index[slot]
		return TypeValueValue(val.Any, slot)
	}
	panic(val)
}

// mapID returns the type ID in the query context of the type with foreign ID
// id in tmap's table.
func mapID(tmap *super.TypeIDMap, id uint32) uint32 {
	local, ok := tmap.LookupID(id)
	if !ok {
		// Panic here, not downstream, if there's a type problem.
		panic(fmt.Sprintf("type ID %d not in typedefs table", id))
	}
	return local
}
