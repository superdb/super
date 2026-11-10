package jsonvec

import "github.com/superdb/super/vector"

var _ Value = (*Union)(nil)

type Union struct {
	Tags   []uint32
	lut    map[vector.Kind]uint32
	None   *None
	Null   *Null
	Bool   *Bool
	Int    *Int
	Float  *Float
	String *String
	Record *Record
	Array  *Array
}

func NewUnion() *Union {
	return &Union{
		lut:    make(map[vector.Kind]uint32),
		Null:   new(Null),
		Bool:   new(Bool),
		Int:    NewInt(),
		Float:  NewFloat(),
		String: NewString(),
		Record: NewRecord(),
		Array:  NewArray(),
	}
}

func ToUnion(val Value) *Union {
	u := NewUnion()
	if val, ok := val.(*None); ok {
		u.None = val
		return u
	}
	u.lut[val.Kind()] = 0
	u.Tags = make([]uint32, val.Len())
	switch val := val.(type) {
	case *None:
		u.None = val
	case *Null:
		u.Null = val
	case *Bool:
		u.Bool = val
	case *Int:
		u.Int = val
	case *Float:
		u.Float = val
	case *String:
		u.String = val
	case *Record:
		u.Record = val
	case *Array:
		u.Array = val
	default:
		panic(val)
	}
	return u
}

func (u *Union) OnNone() Value {
	if u.None == nil {
		u.None = new(None)
	}
	u.None.OnNone()
	return u
}

func (u *Union) OnNull() Value {
	u.Null.OnNull()
	u.touch(vector.KindNull)
	return u
}

func (u *Union) OnBool(v bool) Value {
	u.Bool.OnBool(v)
	u.touch(vector.KindBool)
	return u
}

func (u *Union) OnInt(v int64) Value {
	u.Int.OnInt(v)
	u.touch(vector.KindInt)
	return u
}

func (u *Union) OnFloat(v float64) Value {
	u.Float.OnFloat(v)
	u.touch(vector.KindFloat)
	return u
}

func (u *Union) OnString(v string) Value {
	u.String.OnString(v)
	u.touch(vector.KindString)
	return u
}

func (u *Union) BeginRecord() Value {
	u.Record.BeginRecord()
	u.touch(vector.KindRecord)
	return u
}

func (u *Union) Field(name string) Value {
	return u.Record.Field(name)
}

func (u *Union) EndRecord() {
	u.Record.EndRecord()
}

func (u *Union) BeginArray() Value {
	u.Array.BeginArray()
	u.touch(vector.KindArray)
	return u
}

func (u *Union) EnterArray() Value {
	return u.Array.EnterArray()
}

func (u *Union) EndArray(v Value) {
	u.Array.EndArray(v)
}

func (u *Union) touch(kind vector.Kind) {
	idx, ok := u.lut[kind]
	if !ok {
		idx = uint32(len(u.lut))
		u.lut[kind] = idx
	}
	u.Tags = append(u.Tags, idx)
}

func (u *Union) Values() []Value {
	values := make([]Value, len(u.lut))
	for kind, idx := range u.lut {
		switch kind {
		case vector.KindNone:
			values[idx] = u.None
		case vector.KindNull:
			values[idx] = u.Null
		case vector.KindBool:
			values[idx] = u.Bool
		case vector.KindInt:
			values[idx] = u.Int
		case vector.KindFloat:
			values[idx] = u.Float
		case vector.KindString:
			values[idx] = u.String
		case vector.KindRecord:
			values[idx] = u.Record
		case vector.KindArray:
			values[idx] = u.Array
		default:
			panic(kind)
		}
	}
	return values
}

func (u *Union) Kind() vector.Kind { return vector.KindUnion }
func (u *Union) Len() uint32       { return uint32(len(u.Tags)) }
func (*Union) Error() error        { return nil }
