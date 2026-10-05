package vector

import (
	"iter"
	"slices"

	"github.com/superdb/super"
)

type ApplyOpt uint

const (
	ApplyNone      ApplyOpt = 0
	ApplyRipUnions ApplyOpt = 1 << iota
	ApplyRipFusions
	ApplyRipOptions
)

type NoRip struct {
	Any
}

// Apply applies eval to vecs. If any element of vecs is a Dynamic, Apply rips
// vecs accordingly, applies eval to the ripped vectors, and stitches the
// results together into a Dynamic.  For NoRip elements in vecs, Apply does not
// rip the element but unwraps it (i.e, replaces it with NoRip.Any) before
// calling eval.
func Apply(opt ApplyOpt, eval func(...Any) Any, vecs ...Any) Any {
	if opt&ApplyRipFusions != 0 {
		for k, vec := range vecs {
			if _, ok := vec.(*NoRip); !ok {
				if IsTypeAny(vec) {
					vecs[k] = DefuseAny(vec.(*Fusion))
				} else {
					vecs[k] = Super(vec)
				}
			}
		}
	}
	if opt&ApplyRipOptions != 0 {
		// ApplyRipOptions causes any both-style options to be ripped into
		// pure some or pure none options so that the eval callback need not
		// worry about encountering a dynamic inside the option.  The stitching
		// is all done here.
		for k, vec := range vecs {
			if option, ok := Under(vec).(*Option); ok {
				vecs[k] = ripOption(option)
			}
		}
	}
	if opt&ApplyRipUnions != 0 {
		for k := range vecs {
			if vec, ok := Under(vecs[k]).(*Option); ok {
				vecs[k] = vec.Any
			}
			if vec, ok := Under(vecs[k]).(*Union); ok {
				vecs[k] = vec.Dynamic()
			}
		}
	}
	var needApply bool
	for k, vec := range vecs {
		if d, ok := vec.(*Dynamic); ok && len(d.Tags) > 0 && homogenous(d.Tags) {
			// All slots in d come from the same vector so replace d
			// with it.  Then schedule a recursive call to Apply in
			// case the vector needs to be unwrapped.
			vecs[k] = d.Values[d.Tags[0]]
			needApply = true
		}
	}
	if needApply && opt != 0 {
		return Apply(opt, eval, vecs...)
	}
	d, ok := findDynamic(vecs)
	if !ok {
		for k, vec := range vecs {
			if vec, ok := vec.(*NoRip); ok {
				vecs[k] = vec.Any
			}
		}
		return eval(vecs...)
	}
	results := make([]Any, len(d.Values))
	for i, ripped := range rip(vecs, d) {
		results[i] = Apply(opt, eval, ripped...)
	}
	// stitch removes nils and replaces Dynamics with their values.
	return stitch(d.Tags, results)
}

func homogenous(s []uint32) bool {
	s0 := s[0]
	return !slices.ContainsFunc(s[1:], func(v uint32) bool { return v != s0 })
}

func findDynamic(vecs []Any) (*Dynamic, bool) {
	for _, vec := range vecs {
		if d, ok := vec.(*Dynamic); ok {
			return d, true
		}
	}
	return nil, false
}

func rip(vecs []Any, d *Dynamic) iter.Seq2[int, []Any] {
	return func(yield func(int, []Any) bool) {
		for i, rev := range d.ReverseTagMap() {
			if len(rev) == 0 {
				continue
			}
			ripped := make([]Any, len(vecs))
			for j, vec := range vecs {
				if vec == d {
					ripped[j] = d.Values[i]
				} else {
					ripped[j] = Pick(vec, rev)
				}
			}
			if !yield(i, ripped) {
				return
			}
		}
	}
}

// stitch returns a Dynamic for tags and vecs with nil entries removed and
// Dynamic entries replaced by their values (i.e., it flattens one level of
// Dynamic).
func stitch(tags []uint32, vecs []Any) Any {
	var needStitch bool
	var newVecsLen int
	for _, vec := range vecs {
		switch vec := vec.(type) {
		case nil:
			needStitch = true
		case *Dynamic:
			needStitch = true
			newVecsLen += len(vec.Values)
		default:
			newVecsLen++
		}
	}
	if !needStitch {
		return NewDynamic(tags, vecs)
	}
	newVecs := make([]Any, 0, newVecsLen)     // vecs but without nils and with Dynamics replaced by their values
	nestedTags := make([][]uint32, len(vecs)) // tags from nested Dynamics (nil for non-Dynamics)
	shifts := make([]uint32, len(vecs))       // tag + shift[tag] translates tag to newVecs
	var lastShift uint32
	for i, vec := range vecs {
		shifts[i] = lastShift
		switch vec := vec.(type) {
		case nil:
			lastShift--
		case *Dynamic:
			newVecs = append(newVecs, vec.Values...)
			nestedTags[i] = vec.Tags
			lastShift += uint32(len(vec.Values)) - 1
		default:
			newVecs = append(newVecs, vec)
		}
	}
	newTags := make([]uint32, len(tags))
	for i, t := range tags {
		newTag := t + shifts[t]
		if nested := nestedTags[t]; len(nested) > 0 {
			newTag += nested[0]
			nestedTags[t] = nested[1:]
		}
		newTags[i] = newTag
	}
	return NewDynamic(newTags, newVecs)
}

func Stitch(vec Any) Any {
	switch vec := vec.(type) {
	case *Dynamic:
		return stitch(vec.Tags, vec.Values)
	case *View:
		return NewView(Stitch(vec.Any), vec.Index)
	}
	return vec
}

func AddNoRip(vec Any) Any {
	if vec == nil {
		return vec
	}
	if dynamic, ok := vec.(*Dynamic); ok {
		vals := make([]Any, len(dynamic.Values))
		for i, vec := range dynamic.Values {
			vals[i] = AddNoRip(vec)
		}
		return NewDynamic(dynamic.Tags, vals)
	}
	return &NoRip{vec}
}

// When option is style "both", convert it to a dynamic of a pure some option and
// a pure none option.  Otherwise, return the pure option unmodified.
func ripOption(o *Option) Any {
	if d, ok := o.Any.(*Dynamic); ok {
		optionType := super.TypeUnder(o.Type()).(*super.TypeOption)
		some := NewOption(optionType, d.Values[super.OptionSomeTag])
		none := NewOption(optionType, d.Values[super.OptionNoneTag])
		return NewDynamic(d.Tags, []Any{some, none})
	}
	return o
}
