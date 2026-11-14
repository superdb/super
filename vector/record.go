package vector

import (
	"slices"

	"github.com/superdb/super"
	"github.com/superdb/super/scode"
)

type Record struct {
	Typ    *super.TypeRecord
	Fields []Any
	len    uint32
}

var _ Any = (*Record)(nil)

func NewRecord(typ *super.TypeRecord, fields []Any, length uint32) *Record {
	return &Record{typ, fields, length}
}

func (*Record) Kind() Kind {
	return KindRecord
}

func (r *Record) Type() super.Type {
	return r.Typ
}

func (r *Record) Len() uint32 {
	return r.len
}

func (r *Record) ChangeType(typ *super.TypeRecord) *Record {
	fields := slices.Clone(r.Fields)
	for i, f := range typ.Fields {
		if rtyp, ok := f.Type.(*super.TypeRecord); ok {
			fields[i] = PushView(Under(r.Fields[i])).(*Record).ChangeType(rtyp)
		}
	}
	return &Record{typ, fields, r.len}
}

func (r *Record) Serialize(b *scode.Builder, slot uint32) {
	b.BeginContainer()
	for _, f := range r.Fields {
		f.Serialize(b, slot)
	}
	b.EndContainer()
}
