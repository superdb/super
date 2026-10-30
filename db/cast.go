package db

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/superdb/super"
	"github.com/superdb/super/runtime/expr"
	"github.com/superdb/super/runtime/expr/cast"
	"github.com/superdb/super/scode"
	"github.com/superdb/super/sup"
	"github.com/superdb/super/vector"
	"github.com/superdb/super/vector/vio"
)

var ErrCastFailed = errors.New("cannot be cast to table type")

// castPuller casts the vectors of its parent to the type of a table and
// fails with ErrCastFailed upon encountering a value that cannot be cast.
// Values are cast strictly (i.e., a record with a field not in the table type
// fails) and, for a SQL table, a missing column of a nullable type is null.
type castPuller struct {
	sctx   *super.Context
	parent vio.Puller
	typ    super.Type
	opts   cast.Options
	// typHasError is true if typ has an error type so values of typ
	// may themselves contain errors.
	typHasError bool
	defuse      *expr.SuperDefuse
}

func newCastPuller(sctx *super.Context, parent vio.Puller, typ super.Type, sql bool) *castPuller {
	return &castPuller{
		sctx:        sctx,
		parent:      parent,
		typ:         typ,
		opts:        cast.Options{MissingToNull: sql, Strict: true},
		typHasError: cast.HasError(typ),
		defuse:      expr.NewSuperDefuse(sctx),
	}
}

func (c *castPuller) Pull(done bool) (vector.Any, error) {
	vec, err := c.parent.Pull(done)
	if vec == nil || err != nil {
		return vec, err
	}
	if _, ok := vec.(*vector.Control); ok {
		return vec, nil
	}
	in, label := vector.Unlabel(vec)
	if in == nil {
		// An end-of-stream marker.
		return vec, nil
	}
	out, err := c.cast(in)
	if err != nil {
		return nil, err
	}
	if label != "" {
		return &vector.Labeled{Any: out, Label: label}, nil
	}
	return out, nil
}

func (c *castPuller) cast(vec vector.Any) (vector.Any, error) {
	out := vector.Apply(vector.ApplyRipUnions, func(vecs ...vector.Any) vector.Any {
		if vecs[0].Kind() == vector.KindError {
			// An error value isn't cast so it's reported as is below.
			return vecs[0]
		}
		return cast.CastWithOptions(c.sctx, vecs[0], c.typ, c.opts)
	}, c.defuse.Eval(vec))
	// A value whose cast succeeded has the table type.  A value whose cast
	// failed (in whole or in part) contains an error or, if the value has
	// a null where the table type doesn't admit null (e.g., a NOT NULL
	// column), has a different type since a cast passes null through.
	var err error
	vector.Apply(vector.ApplyNone, func(vecs ...vector.Any) vector.Any {
		vec := vecs[0]
		if err == nil && vec.Len() > 0 && vec.Type() != c.typ {
			var val super.Value
			var reason string
			if cast.HasError(vec.Type()) {
				val, reason = castInput(vector.ValueAt(scode.NewBuilder(), vec, 0))
			} else {
				// Show the value without the unions introduced by the cast.
				val = vector.ValueAt(scode.NewBuilder(), expr.Defuse(c.sctx, vector.Pick(vec, []uint32{0})), 0)
				if hasDisallowedNull(vec.Type(), c.typ) {
					reason = "null not allowed"
				}
			}
			err = fmt.Errorf("value %s %w %s", sup.FormatValue(val), ErrCastFailed, sup.FormatType(c.typ))
			if reason != "" {
				err = fmt.Errorf("%w: %s", err, reason)
			}
		}
		return vec
	}, out)
	if err != nil {
		return nil, err
	}
	if c.opts.MissingToNull {
		// The values of a SQL table's nullable columns are stored as
		// their values (i.e., a value or null) rather than as unions
		// with null.
		out = vector.Apply(vector.ApplyNone, func(vecs ...vector.Any) vector.Any {
			return denullColumns(c.sctx, vecs[0])
		}, out)
	}
	return out, nil
}

// denullColumns returns the record vector vec with the null type removed
// from the union types of its fields, so a field of type T|null is either a
// value of T or null and a field of type T1|T2|null is either a value of
// T1|T2 or null.
func denullColumns(sctx *super.Context, vec vector.Any) vector.Any {
	var index []uint32
	if view, ok := vec.(*vector.View); ok {
		index = view.Index
		vec = view.Any
	}
	rec, ok := vec.(*vector.Record)
	if !ok {
		if index != nil {
			return vector.Pick(vec, index)
		}
		return vec
	}
	fields := slices.Clone(rec.Fields)
	for k, field := range fields {
		if index != nil {
			field = vector.Pick(field, index)
		}
		if union, ok := super.TypeUnder(rec.Typ.Fields[k].Type).(*super.TypeUnion); ok {
			field = denull(sctx, field, union)
		}
		fields[k] = field
	}
	return vector.Apply(vector.ApplyNone, func(vecs ...vector.Any) vector.Any {
		fields := make([]super.Field, len(vecs))
		for k, vec := range vecs {
			fields[k] = super.NewField(rec.Typ.Fields[k].Name, vec.Type())
		}
		return vector.NewRecord(sctx.MustLookupTypeRecord(fields), vecs, vecs[0].Len())
	}, fields...)
}

// denull returns vec, whose type is union, with null removed from union.
func denull(sctx *super.Context, vec vector.Any, union *super.TypeUnion) vector.Any {
	if union.TagOf(super.TypeNull) < 0 {
		return vec
	}
	var rest []super.Type
	for _, typ := range union.Types {
		if typ != super.TypeNull {
			rest = append(rest, typ)
		}
	}
	ripped := vector.Apply(vector.ApplyRipUnions, func(vecs ...vector.Any) vector.Any {
		return vecs[0]
	}, vec)
	if len(rest) == 1 {
		// The values are either of type rest[0] or null.
		return ripped
	}
	restType := sctx.MustLookupTypeUnion(slices.Clone(rest))
	d, ok := ripped.(*vector.Dynamic)
	if !ok {
		// All values have the same type.
		if ripped.Type() == super.TypeNull {
			return ripped
		}
		return vector.NewUnionOfOne(restType, ripped)
	}
	nullTag := -1
	// Map the tags of d to the tags of restType for the non-null values.
	restTags := make([]uint32, len(d.Values))
	restVals := make([]vector.Any, len(restType.Types))
	for k, typ := range restType.Types {
		restVals[k] = vector.NewEmpty(typ)
	}
	for k, val := range d.Values {
		if val.Type() == super.TypeNull {
			nullTag = k
			continue
		}
		tag := restType.TagOf(val.Type())
		restTags[k] = uint32(tag)
		restVals[tag] = val
	}
	tags := make([]uint32, len(d.Tags))
	var unionTags []uint32
	for k, tag := range d.Tags {
		if int(tag) == nullTag {
			continue
		}
		tags[k] = 1
		unionTags = append(unionTags, restTags[tag])
	}
	restUnion := vector.NewUnion(restType, unionTags, restVals)
	if nullTag < 0 {
		return restUnion
	}
	return vector.NewDynamic(tags, []vector.Any{d.Values[nullTag], restUnion})
}

// hasDisallowedNull reports whether a cast to want resulting in type got
// passed through a null where want doesn't admit null.
func hasDisallowedNull(got, want super.Type) bool {
	if got == want {
		return false
	}
	got, want = super.TypeUnder(got), super.TypeUnder(want)
	if u, ok := got.(*super.TypeUnion); ok && u.TagOf(super.TypeNull) >= 0 {
		if w, ok := want.(*super.TypeUnion); !ok || w.TagOf(super.TypeNull) < 0 {
			return true
		}
	}
	if got == super.TypeNull {
		return true
	}
	switch got := got.(type) {
	case *super.TypeRecord:
		if want, ok := want.(*super.TypeRecord); ok {
			for _, f := range got.Fields {
				if w, ok := want.TypeOfField(f.Name); ok && hasDisallowedNull(f.Type, w) {
					return true
				}
			}
		}
	case *super.TypeArray:
		if want, ok := want.(*super.TypeArray); ok {
			return hasDisallowedNull(got.Type, want.Type)
		}
	case *super.TypeSet:
		if want, ok := want.(*super.TypeSet); ok {
			return hasDisallowedNull(got.Type, want.Type)
		}
	}
	return false
}

// castInput returns the value that failed to cast and the reason, if any,
// if val is a cast error of the form error({message:"cannot cast to T[: reason]",
// on:value}) and otherwise returns val.
func castInput(val super.Value) (super.Value, string) {
	errType, ok := super.TypeUnder(val.Type()).(*super.TypeError)
	if !ok {
		return val, ""
	}
	inner := super.NewValue(errType.Type, val.Bytes())
	msg, on := inner.Deref("message"), inner.Deref("on")
	if msg == nil || on == nil || super.TypeUnder(msg.Type()) != super.TypeString {
		return val, ""
	}
	text, ok := strings.CutPrefix(msg.AsString(), "cannot cast to ")
	if !ok {
		return val, ""
	}
	if _, reason, ok := strings.Cut(text, ": "); ok {
		return *on, reason
	}
	return *on, ""
}
