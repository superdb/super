package vector

import (
	"fmt"
	"slices"
	"sync"

	"github.com/superdb/super"
	"github.com/superdb/super/scode"
	"github.com/superdb/super/sup"
)

type Union struct {
	dynamic *Dynamic
	mu      sync.Mutex
	rle     []uint32
	Typ     *super.TypeUnion
}

var _ Any = (*Union)(nil)

func NewUnion(typ *super.TypeUnion, tags []uint32, vals []Any) *Union {
	verifyUnion(typ, vals)
	return &Union{dynamic: NewDynamic(tags, vals), Typ: typ}
}

func NewUnionOfOne(typ *super.TypeUnion, vec Any) *Union {
	tag := typ.TagOf(vec.Type())
	if tag < 0 {
		panic(fmt.Sprintf("trying to put %s inside %s", sup.String(vec.Type()), sup.String(typ)))
	}
	tags := make([]uint32, vec.Len())
	for k := range tags {
		tags[k] = uint32(tag)
	}
	vecs := make([]Any, len(typ.Types))
	for k, typ := range typ.Types {
		if k == tag {
			vecs[k] = vec
		} else {
			vecs[k] = NewEmpty(typ)
		}
	}
	return NewUnion(typ, tags, vecs)
}

func NewUnionFromDynamic(sctx *super.Context, d *Dynamic) *Union {
	types := make([]super.Type, 0, len(d.Values))
	for _, vec := range d.Values {
		types = append(types, vec.Type())
	}
	return &Union{dynamic: d, Typ: sctx.MustLookupTypeUnion(types)}
}

func NewUnionFromRLE(typ *super.TypeUnion, rle []uint32, vecs []Any) *Union {
	verifyUnion(typ, vecs)
	return &Union{dynamic: NewDynamic(nil, vecs), rle: rle, Typ: typ}
}

func NewUnionNoneRLE(sctx *super.Context, vec Any, length uint32, runlens []uint32) *Union {
	typ := vec.Type()
	unionMissing := sctx.UnionMissing(typ)
	if union, ok := vec.(*Union); ok {
		// If it's a union, let's make it an option type by adding type none at the end.
		// We don't (yet) bother trying to run-length encode these since there are more
		// than two vectors.
		types := slices.Clone(union.Typ.Types)
		types = append(types, super.TypeMissing)
		vecs := slices.Clone(union.Values())
		noneTag := uint32(len(vecs))
		// buildTags assumes a single value at tag 0 and a none at tag 1.
		// we'll build that then convert it from this unions tags, where the
		// union tags are preserved and the none at tag 1 goes to the last tag (noneTag).
		tags, missingLen := buildTags(runlens, length)
		vecs = append(vecs, NewMissing(missingLen))
		var from int
		fromTags := union.Tags()
		for k := range tags {
			if tags[k] == 0 {
				tags[k] = fromTags[from]
				from++
			} else {
				tags[k] = noneTag
			}
		}
		return NewUnion(unionMissing, tags, vecs)
	}
	//XXX we should use the RLEs only when substantially smaller than tags
	vecs := []Any{vec, NewMissing(missingLength(runlens))}
	return NewUnionFromRLE(unionMissing, runlens, vecs)
}

// verifyUnion verifies that a created union:
// 1. Has a vector for every type in the union.
// 2. There are not multiple vectors with the same type.
func verifyUnion(utyp *super.TypeUnion, vecs []Any) {
	if len(utyp.Types) != len(vecs) {
		panic("NewUnion: must have one vector for each type")
	}
	seen := make(map[super.Type]struct{})
	for _, vec := range vecs {
		typ := vec.Type()
		if utyp.TagOf(typ) == -1 {
			panic(fmt.Sprintf("NewUnion: type %s not a member of union %s", sup.FormatType(vec.Type()), sup.FormatType(utyp)))
		}
		if _, ok := seen[typ]; ok {
			panic(fmt.Sprintf("NewUnion: multiple vectors with the same type %s", sup.FormatType(typ)))
		}
		seen[typ] = struct{}{}
	}

}

func (*Union) Kind() Kind {
	return KindUnion
}

func (u *Union) Type() super.Type {
	return u.Typ
}

func (u *Union) Len() uint32 {
	return u.dynamic.Len()
}

func (u *Union) Serialize(b *scode.Builder, slot uint32) {
	u.load()
	tag := u.Typ.TagOf(u.dynamic.TypeOf(slot))
	super.BeginUnion(b, tag)
	u.dynamic.Serialize(b, slot)
	b.EndContainer()
}

func (u *Union) Values() []Any {
	return u.dynamic.Values
}

func (u *Union) Tags() []uint32 {
	u.load()
	return u.dynamic.Tags
}

func (u *Union) TagsRLE() []uint32 {
	if u.dynamic.Tags != nil && u.rle == nil {
		if len(u.Typ.Types) != 2 {
			panic("union tags RLEs can have only two types")
		}
		rle := NewRLE()
		tags := u.dynamic.Tags
		for k := range tags {
			if tags[k] == 0 {
				rle.Touch(uint32(k))
			}
		}
		return rle.End(uint32(len(tags)))
	}
	return u.rle
}

func (u *Union) Dynamic() *Dynamic {
	u.load()
	return u.dynamic
}

func (u *Union) ForwardTagMap() []uint32 {
	u.load()
	return u.dynamic.ForwardTagMap()
}

func (u *Union) load() {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.dynamic.Tags == nil {
		u.dynamic.Tags, _ = buildTags(u.rle, u.Len())
	}
}

func (u *Union) IsNormalizedOption() bool {
	var cnt int
	for _, vec := range u.Values() {
		if vec.Len() != 0 {
			cnt++
		}
	}
	return cnt <= 1
}

func (u *Union) Normalized() Any {
	for _, vec := range u.Values() {
		if vec.Len() != 0 {
			return vec
		}
	}
	panic(u)
}

func Deunion(vec Any) Any {
	if u, ok := vec.(*Union); ok {
		return u.Dynamic()
	}
	return vec
}

// RLE emits a sequence of runs of the length of alternating sequences
// of nones and values, beginning with nones.  Every run is non-zero except for
// the first, which may be zero when the first value is non-none.
type RLE struct {
	runs       []uint32
	prediction uint32
	last       uint32
}

func NewRLE() *RLE {
	return &RLE{}
}

// Touch is called for each offset at which a value occurs.
// From this, we derive the runs of values and nones interleaved beginning
// with the first run of nones (which may be 0).
// Whenever there is a gap in values, we record the gap size as a run.
// When touch is called consecutively, we wait for for a gap before
// recording the none run immediately followed by the gap.
func (r *RLE) Touch(off uint32) {
	if r.prediction == r.last {
		// This happens only on first call.
		// Emit length of none run.
		r.emit(off)
		r.last = off
	} else if r.prediction != off {
		// emit length of value run
		r.emit(r.prediction - r.last)
		// emit length of none run
		r.emit(off - r.prediction)
		r.last = off
	}
	r.prediction = off + 1
}

func (r *RLE) End(off uint32) []uint32 {
	if r.prediction == r.last {
		// all nones
		r.emit(off)
	} else if r.prediction == off {
		if len(r.runs) == 1 && r.runs[0] == 0 {
			// all values
			return nil
		}
		// write the last run of values
		r.emit(off - r.last)
	} else {
		// write the last run of values and the last run of nones
		r.Touch(off)
	}
	return r.runs
}

func (r *RLE) emit(run uint32) {
	r.runs = append(r.runs, run)
}

// buildTags returns tags for some/none runs presuming missing tag is 1 and
// some tag 0 (as is consistent with super.OptionSomeTag and super.OptionNoneTag)
func buildTags(runlens []uint32, n uint32) ([]uint32, uint32) {
	tags := make([]uint32, n)
	off := 0
	var missingLen uint32
	for in := 0; in < len(runlens); {
		noneRun := runlens[in]
		in++
		for k := range int(noneRun) {
			tags[off+k] = 1
		}
		off += int(noneRun)
		missingLen += noneRun
		if in >= len(runlens) {
			break
		}
		// skip over values (leaving tags 0)
		off += int(runlens[in])
		in++
	}
	return tags, missingLen
}

func missingLength(runlens []uint32) uint32 {
	var missingLen uint32
	for in := 0; in < len(runlens); {
		missingLen += runlens[in]
		in++
		if in >= len(runlens) {
			break
		}
		// skip values run
		in++
	}
	return missingLen
}

// FlattenUnions takes a Dynamic and recursively replaces any Union values
// with their inner values, rewriting tags so that each slot points directly
// to the leaf value vector.
func FlattenUnions(d *Dynamic) *Dynamic {
	hasUnion := slices.ContainsFunc(d.Values, func(vec Any) bool {
		_, ok := vec.(*Union)
		return ok
	})
	if !hasUnion {
		return d
	}
	bases := make([]uint32, len(d.Values))
	unions := make([]*Dynamic, len(d.Values))
	var newValues []Any
	for i, val := range d.Values {
		bases[i] = uint32(len(newValues))
		if u, ok := val.(*Union); ok {
			flat := FlattenUnions(u.Dynamic())
			unions[i] = flat
			newValues = append(newValues, flat.Values...)
		} else {
			newValues = append(newValues, val)
		}
	}
	forward := d.ForwardTagMap()
	newTags := make([]uint32, len(d.Tags))
	for slot, oldTag := range d.Tags {
		base := bases[oldTag]
		if d := unions[oldTag]; d != nil {
			innerSlot := forward[slot]
			newTags[slot] = base + d.Tags[innerSlot]
		} else {
			newTags[slot] = base
		}
	}
	return NewDynamic(newTags, newValues)
}
