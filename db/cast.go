package db

import (
	"errors"
	"fmt"
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
	// A value whose cast failed (in whole or in part) contains an error
	// (except where the table type has errors, in which case the value's
	// type must be the table type).
	var err error
	vector.Apply(vector.ApplyRipUnions, func(vecs ...vector.Any) vector.Any {
		vec := vecs[0]
		if err == nil && vec.Len() > 0 && cast.HasError(vec.Type()) && (!c.typHasError || vec.Type() != c.typ) {
			val, reason := castInput(vector.ValueAt(scode.NewBuilder(), vec, 0))
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
		// their values (i.e., a value or null) rather than as unions.
		out = vector.Apply(vector.ApplyNone, func(vecs ...vector.Any) vector.Any {
			return expr.Defuse(c.sctx, vecs[0])
		}, out)
	}
	return out, nil
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
