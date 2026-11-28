package jsonvec

import (
	"encoding/binary"
	"fmt"
	"slices"
	"strings"

	"github.com/superdb/super/vector"
)

var _ Value = (*Record)(nil)

type Record struct {
	Fields []*Element
	RLEs   []vector.RLE
	// LUT maps a field name to a column number in the fused record
	LUT map[string]int
	// tags encodes the permute number and has the same length of the
	// the Record vector, so in a sense, are like union tags.
	// As different permuations of field names are encountered
	// they are tracked in the permutation map and each unique arrangement
	// assigned a unique tag.  Note that there may ultimately be
	// more types that unique tags because each field underneath
	// may have super types but this is handled in materialize.
	tags []uint32
	// perm keeps track of each unique permutation of fields
	// as they appear in the fused record.  We index this map with
	// a uvarint coded string of the field positions that each pattern
	// has with respect to the fused fields.  scratch is used by
	// the visitor calls to build up the array.
	perm    map[string]uint32
	scratch []byte
	seen    []uint32
	rows    uint32
}

func NewRecord() *Record {
	return &Record{
		LUT:  make(map[string]int),
		perm: make(map[string]uint32),
	}
}

func (r *Record) BeginRecord() Value {
	r.rows++
	r.scratch = r.scratch[:0]
	return r
}

func (r *Record) Field(name string) Value {
	idx, ok := r.LUT[name]
	if !ok {
		idx = len(r.Fields)
		// name is from an unsafe string conversion so it needs to be cloned
		// here.
		r.LUT[strings.Clone(name)] = idx
		r.Fields = append(r.Fields, &Element{Value: Unknown{}})
		r.RLEs = append(r.RLEs, vector.RLE{})
		r.seen = slices.Grow(r.seen, idx+1)[:idx+1]
	}
	if r.seen[idx] == r.rows {
		return &Element{err: fmt.Errorf("duplicate field %q", name)}
	}
	r.seen[idx] = r.rows
	r.scratch = binary.AppendUvarint(r.scratch, uint64(idx))
	r.RLEs[idx].Touch(uint32(len(r.tags)))
	return r.Fields[idx]
}

func (r *Record) EndRecord() {
	tag, ok := r.perm[string(r.scratch)]
	if !ok {
		tag = uint32(len(r.perm))
		r.perm[string(r.scratch)] = tag
	}
	r.tags = append(r.tags, tag)
}

func (r *Record) OnNone() Value           { return ToUnion(r).OnNone() }
func (r *Record) OnNull() Value           { return ToUnion(r).OnNull() }
func (r *Record) OnBool(v bool) Value     { return ToUnion(r).OnBool(v) }
func (r *Record) OnInt(v int64) Value     { return ToUnion(r).OnInt(v) }
func (r *Record) OnFloat(v float64) Value { return ToUnion(r).OnFloat(v) }
func (r *Record) OnString(v string) Value { return ToUnion(r).OnString(v) }
func (r *Record) BeginArray() Value       { return ToUnion(r).BeginArray() }
func (r *Record) EnterArray() Value       { panic("system error") }
func (r *Record) EndArray(Value)          {}
func (r *Record) Kind() vector.Kind       { return vector.KindRecord }
func (r *Record) Len() uint32             { return uint32(len(r.tags)) }
func (*Record) Error() error              { return nil }

var _ Value = (*Element)(nil)

type Element struct {
	Value
	err error
}

func (f *Element) OnNull() Value {
	f.Value = f.Value.OnNull()
	return f
}

func (f *Element) OnBool(v bool) Value {
	f.Value = f.Value.OnBool(v)
	return f
}

func (f *Element) OnString(v string) Value {
	f.Value = f.Value.OnString(v)
	return f
}

func (f *Element) OnInt(v int64) Value {
	f.Value = f.Value.OnInt(v)
	return f
}

func (f *Element) OnFloat(v float64) Value {
	f.Value = f.Value.OnFloat(v)
	return f
}

func (f *Element) BeginRecord() Value {
	f.Value = f.Value.BeginRecord()
	return f
}

func (f *Element) BeginArray() Value {
	f.Value = f.Value.BeginArray()
	return f
}

func (f *Element) Error() error {
	return f.err
}
