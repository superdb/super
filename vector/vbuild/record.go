package vbuild

import (
	"github.com/superdb/super"
	"github.com/superdb/super/vector"
)

type recordBuilder struct {
	typ    *super.TypeRecord
	fields []Builder
	len    uint32
}

func newRecordBuilder(typ *super.TypeRecord) Builder {
	var fields []Builder
	for _, f := range typ.Fields {
		// XXX when we re-integrate vector.Option, we could have an option builder
		// here for the fields to compute RLEs, or we can leave it to BSUP to figure
		// out when to create thm.
		fields = append(fields, New(f.Type))
	}
	return &recordBuilder{
		typ:    typ,
		fields: fields,
	}
}

func (r *recordBuilder) Write(vec vector.Any) {
	n := vec.Len()
	if n == 0 {
		return
	}
	rec := vector.PushViewOrConst(vec).(*vector.Record)
	for i, vec := range rec.Fields {
		r.fields[i].Write(vec)
	}
	r.len += n
}

func (r *recordBuilder) Build() vector.Any {
	var fields []vector.Any
	for _, b := range r.fields {
		fields = append(fields, b.Build())
	}
	return vector.NewRecord(r.typ, fields, r.len)
}
