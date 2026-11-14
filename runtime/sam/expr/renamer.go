package expr

import (
	"errors"
	"fmt"
	"slices"

	"github.com/superdb/super"
	"github.com/superdb/super/pkg/field"
)

// Renamer renames one or more fields in a record. A field can only be
// renamed within its own record. For example id.orig_h can be renamed
// to id.src, but it cannot be renamed to src. Renames are applied
// left to right; each rename observes the effect of all.
type Renamer struct {
	sctx *super.Context
	// For the dst field name, we just store the leaf name since the
	// src path and the dst path are the same and only differ in the leaf name.
	srcs    []*Lval
	dsts    []*Lval
	typeMap map[int]map[string]*super.TypeRecord
	// fieldsStr is used to reduce allocations when computing the fields id.
	fieldsStr []byte
}

func NewRenamer(sctx *super.Context, srcs, dsts []*Lval) *Renamer {
	return &Renamer{sctx, srcs, dsts, make(map[int]map[string]*super.TypeRecord), nil}
}

func (r *Renamer) Eval(this super.Value) super.Value {
	val, err := r.EvalToValAndError(this)
	if err != nil {
		return r.sctx.WrapError(err.Error(), this)
	}
	return val
}

func (r *Renamer) EvalToValAndError(this super.Value) (super.Value, error) {
	if !super.IsRecordType(this.Type()) {
		return this, nil
	}
	srcs, dsts, err := r.evalFields(this)
	if err != nil {
		return super.Null, fmt.Errorf("rename: %w", err)
	}
	id := this.Type().ID()
	m, ok := r.typeMap[id]
	if !ok {
		m = make(map[string]*super.TypeRecord)
		r.typeMap[id] = m
	}
	r.fieldsStr = dsts.AppendTo(srcs.AppendTo(r.fieldsStr[:0]))
	typ, ok := m[string(r.fieldsStr)]
	if !ok {
		var err error
		typ, err = r.computeType(super.TypeRecordOf(this.Type()), srcs, dsts)
		if err != nil {
			return super.Null, fmt.Errorf("rename: %w", err)
		}
		m[string(r.fieldsStr)] = typ
	}
	if typ == super.TypeRecordOf(this.Type()) {
		// Nothing was renamed so keep any named type.
		return this, nil
	}
	return super.NewValue(typ, this.Bytes()), nil
}

func CheckRenameField(src, dst field.Path) error {
	if len(src) != len(dst) {
		return fmt.Errorf("left-hand side and right-hand side must have the same depth (%s vs %s)", src, dst)
	}
	for i := 0; i <= len(src)-2; i++ {
		if src[i] != dst[i] {
			return fmt.Errorf("cannot rename %s to %s (differ in %s vs %s)", src, dst, src[i], dst[i])
		}
	}
	return nil
}

func (r *Renamer) evalFields(this super.Value) (field.List, field.List, error) {
	var srcs, dsts field.List
	for i := range r.srcs {
		src, err := r.srcs[i].Eval(this)
		if err != nil {
			return nil, nil, err
		}
		dst, err := r.dsts[i].Eval(this)
		if err != nil {
			return nil, nil, err
		}
		if err := CheckRenameField(src, dst); err != nil {
			return nil, nil, err
		}
		srcs = append(srcs, src)
		dsts = append(dsts, dst)
	}
	return srcs, dsts, nil
}

func (r *Renamer) computeType(typ *super.TypeRecord, srcs, dsts field.List) (*super.TypeRecord, error) {
	for k, dst := range dsts {
		var err error
		typ, err = r.dstType(typ, srcs[k], dst)
		if err != nil {
			return nil, err
		}
	}
	return typ, nil
}

func (r *Renamer) dstType(typ *super.TypeRecord, src, dst field.Path) (*super.TypeRecord, error) {
	i, ok := typ.IndexOfField(src[0])
	if !ok {
		return typ, nil
	}
	var innerType super.Type
	if len(src) > 1 {
		recType, ok := super.TypeUnder(typ.Fields[i].Type).(*super.TypeRecord)
		if !ok {
			return typ, nil
		}
		inner, err := r.dstType(recType, src[1:], dst[1:])
		if err != nil {
			return nil, err
		}
		if inner == recType {
			// Nothing was renamed so keep any named type.
			return typ, nil
		}
		innerType = inner
	} else {
		innerType = typ.Fields[i].Type
	}
	fields := slices.Clone(typ.Fields)
	fields[i] = super.NewField(dst[0], innerType)
	typ, err := r.sctx.LookupTypeRecord(fields)
	if err != nil {
		if _, ok := errors.AsType[*super.DuplicateFieldError](err); ok {
			return nil, err
		}
		panic(err)
	}
	return typ, nil
}
