package aggregate

import (
	"fmt"

	"github.com/superdb/super"
	"github.com/superdb/super/vector"
)

type countByString struct {
	typ        super.Type
	table      map[string]int64
	builder    *vector.RecordBuilder
	partialsIn bool
}

func newCountByString(typ super.Type, b *vector.RecordBuilder, partialsIn bool) aggTable {
	return &countByString{
		typ:        typ,
		builder:    b,
		table:      make(map[string]int64),
		partialsIn: partialsIn,
	}
}

func (c *countByString) update(keys, vals []vector.Any) {
	if c.partialsIn {
		c.updatePartial(keys[0], vals[0])
		return
	}
	switch val := vector.Under(keys[0]).(type) {
	case *vector.String:
		c.count(val)
	case *vector.Dict:
		c.countDict(val.Any.(*vector.String), val.Counts)
	case *vector.Const:
		c.countFixed(val)
	case *vector.View:
		c.countView(val)
	default:
		panic(fmt.Sprintf("UNKNOWN %T", val))
	}
}

func (c *countByString) updatePartial(keyvec, valvec vector.Any) {
	key, ok1 := vector.Under(keyvec).(*vector.String)
	val, ok2 := valvec.(*vector.Int)
	if !ok1 || !ok2 {
		panic("count by string: invalid partials in")
	}
	for i := range key.Len() {
		c.table[key.Value(i)] += val.Values[i]
	}
}

func (c *countByString) count(vec *vector.String) {
	offs, bytes := vec.Table().Slices()
	for k := range vec.Len() {
		c.table[string(bytes[offs[k]:offs[k+1]])]++
	}
}

func (c *countByString) countDict(vec *vector.String, counts []uint32) {
	offs, bytes := vec.Table().Slices()
	for k := range vec.Len() {
		if counts[k] > 0 {
			c.table[string(bytes[offs[k]:offs[k+1]])] += int64(counts[k])
		}
	}
}

func (c *countByString) countFixed(vec *vector.Const) {
	c.table[vector.StringValue(vec, 0)] += int64(vec.Len())
}

func (c *countByString) countView(vec *vector.View) {
	strVec := vec.Any.(*vector.String)
	for _, slot := range vec.Index {
		c.table[strVec.Value(slot)]++
	}
}

func (c *countByString) materialize() vector.Any {
	length := len(c.table)
	counts := make([]int64, length)
	var bytes []byte
	offs := make([]uint32, length+1)
	var k int
	for key, count := range c.table {
		offs[k] = uint32(len(bytes))
		bytes = append(bytes, key...)
		counts[k] = count
		k++
	}
	offs[k] = uint32(len(bytes))
	keyVec := vector.Any(vector.NewString(vector.NewBytesTable(offs, bytes)))
	if n, ok := c.typ.(*super.TypeNamed); ok {
		keyVec = vector.NewNamed(n, keyVec)
	}
	countVec := vector.NewInt(super.TypeInt64, counts)
	return c.builder.New([]vector.Any{keyVec, countVec})
}
