package jsonvec

import (
	"github.com/superdb/super/vector"
)

type None struct{}

func (n *None) OnNone() Value           { return n }
func (n *None) OnNull() Value           { return ToUnion(n).OnNull() }
func (n *None) OnBool(v bool) Value     { return ToUnion(n).OnBool(v) }
func (n *None) OnInt(v int64) Value     { return ToUnion(n).OnInt(v) }
func (n *None) OnFloat(v float64) Value { return ToUnion(n).OnFloat(v) }
func (n *None) OnString(v string) Value { return ToUnion(n).OnString(v) }
func (n *None) BeginRecord() Value      { return ToUnion(n).BeginRecord() }
func (*None) Field(string) Value        { panic("system error") }
func (*None) EndRecord()                { panic("system error") }
func (n *None) BeginArray() Value       { return ToUnion(n).BeginArray() }
func (*None) EnterArray() Value         { panic("system error") }
func (*None) EndArray(Value)            { panic("system error") }
func (*None) Kind() vector.Kind         { return vector.KindMissing }
func (*None) Len() uint32               { return 0 }
func (*None) Error() error              { return nil }
