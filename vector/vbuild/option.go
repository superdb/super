package vbuild

import (
	"github.com/superdb/super"
	"github.com/superdb/super/vector"
)

type optionBuilder struct {
	typ    *super.TypeOption
	values Builder
	tags   []uint32
	nones  uint32
}

func newOptionBuilder(typ *super.TypeOption) Builder {
	return &optionBuilder{typ: typ, values: New(typ.Type)}
}

func (o *optionBuilder) Write(vec vector.Any) {
	if vec.Len() == 0 {
		return
	}
	option := vector.PushView(vec).(*vector.Option)
	option.Apply(func(tags []uint32, some vector.Any, none *vector.Missing) vector.Any {
		if some == nil {
			n := none.Len()
			for range n {
				o.tags = append(o.tags, super.OptionNoneTag)
			}
			o.nones += n
			return nil
		}
		if none == nil {
			o.values.Write(some)
			for range some.Len() {
				o.tags = append(o.tags, super.OptionSomeTag)
			}
			return nil
		}
		// both style
		o.tags = append(o.tags, tags...)
		o.nones += none.Len()
		o.values.Write(some)
		return nil
	})
}

func (o *optionBuilder) Build() vector.Any {
	some := o.values.Build()
	if some.Len() == 0 {
		return vector.NewNone(o.typ, o.nones)
	}
	if o.nones == 0 {
		return vector.NewSome(o.typ, some)
	}
	return vector.NewOption(o.typ, o.tags, some, vector.NewMissing(o.nones))
}
