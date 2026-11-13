package vhash

import (
	"github.com/superdb/super"
	"github.com/superdb/super/vector"
)

// emptyRecordHash is the hash of every value of a record type with no
// fields.  Any nonzero constant works since a key position holds values of a
// single type.
const emptyRecordHash = 0x9e3779b97f4a7c15

// recordKey is a key of a record type.  Each field is stored by its own key,
// and the rows of the fields line up with the rows of the record.
type recordKey struct {
	typ    *super.TypeRecord
	fields []key
	n      uint32 // number of stored keys
}

func newRecordKey(typ *super.TypeRecord) *recordKey {
	k := &recordKey{typ: typ}
	for _, f := range typ.Fields {
		k.fields = append(k.fields, newKey(f.Type))
	}
	return k
}

func (k *recordKey) bind(vec vector.Any) {
	rec := vector.PushView(vector.Under(vec)).(*vector.Record)
	for i, f := range k.fields {
		f.bind(rec.Fields[i])
	}
}

func (k *recordKey) hash(hashes []uint64) {
	if len(k.fields) == 0 {
		for i := range hashes {
			hashes[i] = emptyRecordHash
		}
		return
	}
	k.fields[0].hash(hashes)
	for _, f := range k.fields[1:] {
		f.combineHash(hashes)
	}
}

func (k *recordKey) combineHash(hashes []uint64) {
	if len(k.fields) == 0 {
		for i, h := range hashes {
			hashes[i] = combine(h, emptyRecordHash)
		}
		return
	}
	for _, f := range k.fields {
		f.combineHash(hashes)
	}
}

func (k *recordKey) equal(rows, keyIDs []uint32, eq []bool) {
	for _, f := range k.fields {
		f.equal(rows, keyIDs, eq)
	}
}

func (k *recordKey) append(rows []uint32) {
	for _, f := range k.fields {
		f.append(rows)
	}
	k.n += uint32(len(rows))
}

func (k *recordKey) vector(sctx *super.Context) vector.Any {
	var fields []vector.Any
	for _, f := range k.fields {
		fields = append(fields, f.vector(sctx))
	}
	return vector.NewRecord(k.typ, fields, k.n)
}
