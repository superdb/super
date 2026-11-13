package vector

import (
	"github.com/superdb/super"
	"github.com/superdb/super/scode"
)

// An option value is stored as any vector with its option type.
// The Any field is a None vector for none option values and any
// other vector for some option values.  When somes or nones are
// mixed into a single Option vector, then Any is a two-element
// Dynamic with the first value a some and the second value a none.
// This design relies upon super.OptionSomeTag=0 and super.OptionNoneTag=1.
// Since none cannot be the type of an option's some value, the flavor of
// option (pure some / pure none / both) is determined by the vector type
// of Any.  The only way to access the noneness of an option type is by
// expressing it through coalesce which provides the subtitute value for
// the nones.
type Option struct {
	Typ *super.TypeOption
	Any
}

var _ Any = (*Option)(nil)

func NewOptionSome(typ *super.TypeOption, vec Any) *Option {
	return &Option{Typ: typ, Any: vec}
}

func NewOptionNone(typ *super.TypeOption, noneLen uint32) *Option {
	return &Option{Typ: typ, Any: NewNone(noneLen)}
}

func NewOption(typ *super.TypeOption, tags []uint32, some Any, none *None) *Option {
	return &Option{typ, NewDynamic(tags, []Any{some, none})}
}

func NewOptionFromRLE(sctx *super.Context, some Any, n uint32, rle []uint32) *Option {
	tags, noneLen := TagsFromRLE(rle, n)
	optionType := sctx.LookupTypeOption(some.Type())
	if noneLen == 0 {
		panic("can't be zero")
	}
	return NewOption(optionType, tags, some, NewNone(noneLen))
}

func (*Option) Kind() Kind {
	return KindOption
}

func (o *Option) Type() super.Type {
	return o.Typ
}

func (o *Option) SomeLen() uint32 {
	switch vec := o.Any.(type) {
	case *None:
		return 0
	case *Dynamic:
		return vec.Values[0].Len()
	default:
		return vec.Len()
	}
}

func (o *Option) Serialize(b *scode.Builder, slot uint32) {
	tag := super.OptionNoneTag
	switch vec := o.Any.(type) {
	case *None:
	case *Dynamic:
		if vec.TypeOf(slot) != super.TypeNone {
			tag = super.OptionSomeTag
		}
	default:
		tag = super.OptionSomeTag
	}
	b.BeginContainer()
	b.Append(super.EncodeUint(uint64(tag)))
	if tag == super.OptionSomeTag {
		o.Any.Serialize(b, slot)
	}
	b.EndContainer()
}

func (o *Option) Apply(f func([]uint32, Any, *None) Any) Any {
	switch vec := o.Any.(type) {
	case *None:
		return f(nil, nil, vec)
	case *Dynamic:
		return f(vec.Tags, vec.Values[0], vec.Values[1].(*None))
	default:
		return f(nil, vec, nil)
	}
}

func (o *Option) Coaelesce(defaults Any) Any {
	switch vec := o.Any.(type) {
	case *None:
		return defaults
	case *Dynamic:
		defaults = Pick(defaults, noneIndex(vec.Tags))
		return NewDynamic(vec.Tags, []Any{vec.Values[0], defaults})
	default:
		return vec
	}
}

func noneIndex(tags []uint32) []uint32 {
	index := make([]uint32, 0, len(tags))
	for slot, tag := range tags {
		if tag == super.OptionNoneTag {
			index = append(index, uint32(slot))
		}
	}
	return index
}

// Returns the underlying Option for any vector having KindOption.
// To be clear, this does not reach through fusion values.
func UnderOption(vec Any) *Option {
	return PushView(Under(vec)).(*Option)
}
