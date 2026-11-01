package vector

import (
	"sync"

	"github.com/superdb/super"
	"github.com/superdb/super/scode"
)

type TypeValue struct {
	mu sync.Mutex
	// sctx is only populated/needed if values are retrieved via the loader.
	sctx   *super.Context
	loader TypesLoader
	types  []super.Type
}

// TypesLoader is an interface to load types as IDs and a TypeDefs table so
// they do not pollute the query type context and are converted only when
// needed.  For example, the BSUP reader reads the typedefs table only when
// there is a runtime call to do so, and the BSUP writer loads types as
// TypeDefs for merging and writing to metadata without ever needing to
// create any super.Types.
type TypesLoader interface {
	Load() (*super.TypeDefs, []uint32)
}

var _ Any = (*TypeValue)(nil)

func NewTypeValue(types []super.Type) *TypeValue {
	return &TypeValue{types: types}
}

func NewTypeValueWithLoader(sctx *super.Context, loader TypesLoader) *TypeValue {
	return &TypeValue{sctx: sctx, loader: loader}
}

func NewTypeValueEmpty() *TypeValue {
	return &TypeValue{}
}

func (t *TypeValue) Append(typ super.Type) {
	t.types = append(t.types, typ)
}

func (*TypeValue) Kind() Kind {
	return KindType
}

func (t *TypeValue) Type() super.Type {
	return super.TypeType
}

func (t *TypeValue) Len() uint32 {
	return uint32(len(t.Types()))
}

func (t *TypeValue) Value(slot uint32) super.Type {
	return t.Types()[slot]
}

func (t *TypeValue) Sctx() *super.Context {
	return t.sctx
}

func (t *TypeValue) TypeIDs() (*super.TypeDefs, []uint32) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.typeIDs()
}

func (t *TypeValue) typeIDs() (*super.TypeDefs, []uint32) {
	if t.loader == nil {
		t.loader = t.buildTypeDefs()
	}
	return t.loader.Load()
}

type loaderShim struct {
	defs *super.TypeDefs
	ids  []uint32
}

var _ TypesLoader = (*loaderShim)(nil)

func (l *loaderShim) Load() (*super.TypeDefs, []uint32) {
	return l.defs, l.ids
}

func (t *TypeValue) buildTypeDefs() *loaderShim {
	defs := super.NewTypeDefs()
	ids := make([]uint32, 0, len(t.types))
	for _, typ := range t.types {
		// This lookup has the side effect of installing each needed typedef
		// in the defs table
		ids = append(ids, defs.LookupType(typ))
	}
	return &loaderShim{defs, ids}
}

func (t *TypeValue) Types() []super.Type {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.types == nil {
		defs, ids := t.typeIDs()
		t.types = make([]super.Type, len(ids))
		mapper := super.NewTypeDefsMapper(t.sctx, defs)
		for i, id := range ids {
			t.types[i] = mapper.LookupType(id)
			if t.types[i] == nil {
				// Panic here, not downstream, if there's a type problem.
				panic(t.types[i])
			}
		}
	}
	return t.types
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
	case *Dynamic:
		return TypeValueValue(val.Values[val.Tags[slot]], val.ForwardTagMap()[slot])
	}
	panic(val)
}
