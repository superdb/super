package vector

import (
	"github.com/superdb/super"
	"github.com/superdb/super/scode"
)

type Error struct {
	Typ  *super.TypeError
	Vals Any
}

var _ Any = (*Error)(nil)

// XXX we shouldn't create empty fields... this was the old design, now
// we create the entire vector structure and page in leaves, offsets, etc on demand
func NewError(typ *super.TypeError, vals Any) *Error {
	return &Error{typ, vals}
}

func (*Error) Kind() Kind {
	return KindError
}

func (e *Error) Type() super.Type {
	return e.Typ
}

func (e *Error) Len() uint32 {
	return e.Vals.Len()
}

func (e *Error) Serialize(b *scode.Builder, slot uint32) {
	e.Vals.Serialize(b, slot)
}

func NewStringError(sctx *super.Context, msg string, len uint32) *Error {
	vals := NewConstString(msg, len)
	return &Error{Typ: sctx.LookupTypeError(super.TypeString), Vals: vals}
}

func NewWrappedError(sctx *super.Context, msg string, val Any) *Error {
	msgVec := NewConstString(msg, val.Len())
	return NewVecWrappedError(sctx, msgVec, val)
}

func NewCombinedError(sctx *super.Context, msg string, vec, on Any, index []uint32) Any {
	return Combine(vec, index, NewWrappedError(sctx, msg, Pick(on, index)))
}

func NewVecWrappedError(sctx *super.Context, msg Any, val Any) *Error {
	recType := sctx.MustLookupTypeRecord([]super.Field{
		{Name: "message", Type: msg.Type()},
		{Name: "on", Type: val.Type()},
	})
	rval := NewRecord(recType, []Any{msg, val}, val.Len())
	return &Error{Typ: sctx.LookupTypeError(recType), Vals: rval}
}
