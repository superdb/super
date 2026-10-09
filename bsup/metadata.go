package bsup

import (
	"net/netip"
	"slices"

	"github.com/superdb/super"
	"github.com/superdb/super/order"
	"github.com/superdb/super/pkg/field"
	"github.com/superdb/super/runtime/sam/expr"
	"github.com/superdb/super/scode"
)

type Metadata interface {
	Len(*Context) uint32
}

type Empty struct {
	Type super.Type
}

func (*Empty) Len(*Context) uint32 {
	return 0
}

type Record struct {
	Length uint32
	Fields []Field
}

func (r *Record) Len(*Context) uint32 {
	return r.Length
}

func (r *Record) LookupField(name string) *Field {
	for k, field := range r.Fields {
		if field.Name == name {
			return &r.Fields[k]
		}
	}
	return nil
}

func under(cctx *Context, meta Metadata) Metadata {
	for {
		switch inner := meta.(type) {
		case *Named:
			meta = cctx.Lookup(inner.Values)
		default:
			return meta
		}
	}
}

type Field struct {
	Name   string
	Values ID
}

type Array struct {
	Length  uint32
	Lengths Segment
	Values  ID
}

func (a *Array) Len(*Context) uint32 {
	return a.Length
}

type Set Array

func (s *Set) Len(*Context) uint32 {
	return s.Length
}

type Map struct {
	Length  uint32
	Lengths Segment
	Keys    ID
	Values  ID
}

func (m *Map) Len(*Context) uint32 {
	return m.Length
}

type Union struct {
	Length uint32
	Tags   Segment
	Values []ID
}

func (u *Union) Len(*Context) uint32 {
	return u.Length
}

type Enum struct {
	Symbols []string
	Values  ID
}

func (e *Enum) Len(cctx *Context) uint32 {
	return cctx.Lookup(e.Values).Len(cctx)
}

type Named struct {
	Name   string
	Values ID
}

func (n *Named) Len(cctx *Context) uint32 {
	return cctx.Lookup(n.Values).Len(cctx)
}

type Error struct {
	Values ID
}

func (e *Error) Len(cctx *Context) uint32 {
	return cctx.Lookup(e.Values).Len(cctx)
}

type Fusion struct {
	Values   ID
	Subtypes ID
}

func (f *Fusion) Len(cctx *Context) uint32 {
	return cctx.Lookup(f.Values).Len(cctx)
}

type Option struct {
	Type   super.Type `super:"Type"`
	Length uint32
	Tags   Segment
	Values ID
}

func (o *Option) Len(*Context) uint32 {
	return o.Length
}

type Any struct {
	Values   ID
	Subtypes ID
}

func (a *Any) Len(cctx *Context) uint32 {
	return cctx.Lookup(a.Values).Len(cctx)
}

type Int struct {
	Typ      super.Type `super:"Type"`
	Location Segment
	Min      int64
	Max      int64
	Count    uint32
}

func (i *Int) Type(*Context, *super.Context) super.Type {
	return i.Typ
}

func (i *Int) Len(*Context) uint32 {
	return i.Count
}

type Uint struct {
	Typ      super.Type `super:"Type"`
	Location Segment
	Min      uint64
	Max      uint64
	Count    uint32
}

func (u *Uint) Type(*Context, *super.Context) super.Type {
	return u.Typ
}

func (u *Uint) Len(*Context) uint32 {
	return u.Count
}

type Float struct {
	Typ      super.Type `super:"Type"`
	Location Segment
	Min      float64
	Max      float64
	Count    uint32
}

func (f *Float) Type(*Context, *super.Context) super.Type {
	return f.Typ
}

func (f *Float) Len(*Context) uint32 {
	return f.Count
}

type Bool struct {
	Location Segment
	Count    uint32
}

func (b *Bool) Len(*Context) uint32 {
	return b.Count
}

type Bytes struct {
	Typ     super.Type `super:"Type"`
	Bytes   Segment
	Offsets Segment
	Min     []byte
	Max     []byte
	Count   uint32
}

func (b *Bytes) Type(*Context, *super.Context) super.Type {
	return b.Typ
}

func (b *Bytes) Len(*Context) uint32 {
	return b.Count
}

type TypeValue struct {
	Location Segment
	Length   uint32
}

func (t *TypeValue) Len(*Context) uint32 {
	return t.Length
}

type IP struct {
	Bytes   Segment
	Offsets Segment
	Min     netip.Addr
	Max     netip.Addr
	Count   uint32
}

func (n *IP) Len(*Context) uint32 {
	return n.Count
}

type Net struct {
	Bytes   Segment
	Offsets Segment
	Min     netip.Prefix
	Max     netip.Prefix
	Count   uint32
}

func (n *Net) Len(*Context) uint32 {
	return n.Count
}

type Null struct {
	Count uint32
}

func (n *Null) Len(*Context) uint32 {
	return n.Count
}

type None struct {
	Count uint32
}

func (n *None) Len(*Context) uint32 {
	return n.Count
}

type Const struct {
	Value super.Value // this value lives in local context and needs to be translated by shadow
	Count uint32
}

func (c *Const) Type(_ *Context, sctx *super.Context) super.Type {
	typ, err := sctx.TranslateType(c.Value.Type())
	if err != nil {
		panic(err)
	}
	return typ
}

func (c *Const) Len(*Context) uint32 {
	return c.Count
}

type Dict struct {
	Values ID
	Counts Segment
	Index  Segment
	Length uint32
}

func (d *Dict) Len(*Context) uint32 {
	return d.Length
}

type Dynamic struct {
	Tags   Segment
	Values []ID
	Length uint32
}

var _ Metadata = (*Dynamic)(nil)

func (*Dynamic) Type(*Context, *super.Context) super.Type {
	panic("Type should not be called on Dynamic")
}

func (d *Dynamic) Len(*Context) uint32 {
	return d.Length
}

func newMetadataValue(cctx *Context, sctx *super.Context, b *scode.Builder, id ID, p field.Projection) super.Value {
	b.Truncate()
	typ := metadataValue(cctx, sctx, b, id, p)
	return super.NewValue(typ, b.Bytes().Body())
}

// XXX this should live in shadow so we don't load it twice
func metadataValue(cctx *Context, sctx *super.Context, b *scode.Builder, id ID, projection field.Projection) super.Type {
	m := cctx.Lookup(id)
	switch m := under(cctx, m).(type) {
	case *Any:
		// XXX Do not have min/max on all values for now.
		b.Append(nil)
		return super.TypeNull
	case *Fusion:
		return metadataValue(cctx, sctx, b, m.Values, projection)
	case *Option:
		return metadataValue(cctx, sctx, b, m.Values, projection)
	case *Dict:
		return metadataValue(cctx, sctx, b, m.Values, projection)
	case *Record:
		var fields []super.Field
		b.BeginContainer()
		if len(projection) == 0 {
			for _, f := range m.Fields {
				typ := metadataValue(cctx, sctx, b, f.Values, nil)
				fields = append(fields, super.NewField(f.Name, typ))
			}
		} else {
			for _, node := range projection {
				if k := indexOfField(node.Name, m.Fields); k >= 0 {
					typ := metadataValue(cctx, sctx, b, m.Fields[k].Values, node.Proj)
					fields = append(fields, super.NewField(node.Name, typ))
				}
			}
		}
		b.EndContainer()
		return sctx.MustLookupTypeRecord(fields)
	case *Union:
		cmp := expr.NewValueCompareFn(order.Asc, order.NullsLast)
		max, min := super.Null, super.Null
		var bb scode.Builder
		for _, id := range m.Values {
			val := newMetadataValue(cctx, sctx, &bb, id, projection)
			if val.IsNull() {
				continue
			}
			min2, max2 := val.Deref("min"), val.Deref("max")
			if min2 == nil {
				continue
			}
			if min.IsNull() || cmp(min, *min2) > 0 {
				min = min2.Copy()
			}
			if max.IsNull() || cmp(max, *max2) < 0 {
				max = max2.Copy()
			}
		}
		if min.IsNull() {
			b.Append(nil)
			return super.TypeNull
		}
		return metadataLeaf(sctx, b, min, max)
	case *Int:
		return metadataLeaf(sctx, b, super.NewInt(m.Typ, m.Min), super.NewInt(m.Typ, m.Max))
	case *Uint:
		return metadataLeaf(sctx, b, super.NewUint(m.Typ, m.Min), super.NewUint(m.Typ, m.Max))
	case *Float:
		return metadataLeaf(sctx, b, super.NewFloat(m.Typ, m.Min), super.NewFloat(m.Typ, m.Max))
	case *Bytes:
		return metadataLeaf(sctx, b, super.NewValue(m.Typ, m.Min), super.NewValue(m.Typ, m.Max))
	case *IP:
		return metadataLeaf(sctx, b, super.NewIP(m.Min), super.NewIP(m.Max))
	case *Net:
		return metadataLeaf(sctx, b, super.NewNet(m.Min), super.NewNet(m.Max))
	case *Const:
		return metadataLeaf(sctx, b, m.Value, m.Value)
	default:
		b.Append(nil)
		return super.TypeNull
	}
}

func metadataLeaf(sctx *super.Context, b *scode.Builder, min, max super.Value) super.Type {
	b.BeginContainer()
	b.Append(min.Bytes())
	b.Append(max.Bytes())
	b.EndContainer()
	return sctx.MustLookupTypeRecord([]super.Field{
		super.NewField("min", min.Type()),
		super.NewField("max", max.Type()),
	})
}

func indexOfField(name string, fields []Field) int {
	return slices.IndexFunc(fields, func(f Field) bool {
		return f.Name == name
	})
}

var Template = []any{
	Record{},
	Array{},
	Set{},
	Map{},
	Union{},
	Int{},
	Uint{},
	Float{},
	Bytes{},
	TypeValue{},
	Named{},
	Error{},
	Const{},
	Dict{},
	Dynamic{},
	Fusion{},
	Option{},
	Any{},
	Empty{},
	Enum{},
	Bool{},
	IP{},
	Net{},
	Null{},
	None{},
}
