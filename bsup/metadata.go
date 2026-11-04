package bsup

import (
	"github.com/superdb/super"
)

type Metadata interface {
	Len(*Context) uint32
}

type Empty struct {
	TypeID uint32
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

func under(bctx *Context, meta Metadata) Metadata {
	for {
		switch inner := meta.(type) {
		case *Named:
			meta = bctx.Lookup(inner.Values)
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

func (e *Enum) Len(bctx *Context) uint32 {
	return bctx.Lookup(e.Values).Len(bctx)
}

type Named struct {
	Name   string
	Values ID
}

func (n *Named) Len(bctx *Context) uint32 {
	return bctx.Lookup(n.Values).Len(bctx)
}

type Error struct {
	Values ID
}

func (e *Error) Len(bctx *Context) uint32 {
	return bctx.Lookup(e.Values).Len(bctx)
}

type Fusion struct {
	Values   ID
	Subtypes ID
}

func (f *Fusion) Len(bctx *Context) uint32 {
	return bctx.Lookup(f.Values).Len(bctx)
}

type Option struct {
	TypeID uint32
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

func (a *Any) Len(bctx *Context) uint32 {
	return bctx.Lookup(a.Values).Len(bctx)
}

type Int struct {
	Typ      super.Type `super:"Type"`
	Location Segment
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
	Count   uint32
}

func (n *IP) Len(*Context) uint32 {
	return n.Count
}

type Net struct {
	Bytes   Segment
	Offsets Segment
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

type Missing struct {
	Count uint32
}

func (m *Missing) Len(*Context) uint32 {
	return m.Count
}

// None is the metadata for a vector of singleton none values.
type None struct {
	Count uint32
}

func (n *None) Len(*Context) uint32 {
	return n.Count
}

type Const struct {
	TypeID uint32
	Bytes  []byte
	Count  uint32
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
	Missing{},
}
