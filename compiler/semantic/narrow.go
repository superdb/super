package semantic

import (
	"strings"

	"github.com/superdb/super"
	"github.com/superdb/super/compiler/semantic/sem"
	"github.com/superdb/super/pkg/field"
	"github.com/superdb/super/sup"
)

// narrow returns typ, the type of the input of a filter with predicate e,
// narrowed to the types of the values for which e may be true, so the
// operators downstream of the filter are checked with a more precise type,
// e.g., after "where is_ok(a)", a field a that may be missing in typ is not
// missing.  Narrowing is like type narrowing in TypeScript and handles
// predicates of these forms, where p is a field path (including a SQL
// column reference):
//
//   - A and B, which narrows by A and then by B,
//   - A or B, which fuses the types narrowed by A and by B,
//   - is_ok(p), which removes the error, missing, none, and null types of p,
//   - is_error(p), which narrows the type of p to its error and missing types,
//   - not is_error(p), which removes the error and missing types of p,
//   - is(p, <T>), which narrows the type of p to T,
//   - kind(p) == "<kind>", which removes the types of p whose kind
//     (see the kind function) isn't <kind>,
//   - p IS NOT NULL, which removes the error, missing, and null types of p,
//   - a comparison of p (e.g., p > 1), which removes the error, missing, and
//     null types of p since a comparison with such a value is an error
//     or null and a filter drops values for which its predicate isn't true,
//     and
//   - p IN <expr>, which removes the error, missing, none, and null types of
//     p for the same reason.
//
// A missing field is an error at runtime (or null for a SQL column
// reference), so missing is treated as an error type.
//
// When the type of p is a union, these remove the union's member types.
// When p is a field of a record that's a member of a union, a member for
// which the predicate can't be true (e.g., a record without the field) is
// removed from the union.  Other forms of predicates don't narrow typ.
//
// Negations of predicates generally don't narrow since a union value's type
// is its union type in kind() and is() but the type checker can't tell a
// union type from the union of the types of fused values (e.g., the union
// in a fused record type), whose kind() and is() are their actual types.
// is_error(), however, applies to the value inside a union, so its negation
// is safe.
func (c *checker) narrow(typ super.Type, e sem.Expr) super.Type {
	return c.narrowBy(typ, e, false)
}

// narrowAnd returns typ narrowed for the right-hand side of lhs and rhs.
// When the and is a value rather than a filter, the right-hand side matters
// when lhs is true or null (since null and error is an error), so a
// predicate that's null for a null operand (a comparison or IN) can't remove
// null.
func (c *checker) narrowAnd(typ super.Type, lhs sem.Expr) super.Type {
	return c.narrowBy(typ, lhs, true)
}

// narrowBy is narrow where orNull means e may be true or null.
func (c *checker) narrowBy(typ super.Type, e sem.Expr, orNull bool) super.Type {
	if typ == nil || hasUnknown(typ) {
		return typ
	}
	cmpKeep, inKeep := isComparable, isPresent
	if orNull {
		cmpKeep, inKeep = notErrorLike, notErrorLike
	}
	switch e := e.(type) {
	case *sem.BinaryExpr:
		switch op := strings.ToLower(e.Op); op {
		case "and":
			return c.narrowBy(c.narrowBy(typ, e.LHS, orNull), e.RHS, orNull)
		case "or":
			lhs := defuse(c.narrowBy(typ, e.LHS, orNull))
			rhs := defuse(c.narrowBy(typ, e.RHS, orNull))
			if lhs == defuse(typ) || rhs == defuse(typ) {
				// One side doesn't narrow.
				return typ
			}
			if lhs == rhs {
				return lhs
			}
			return c.fuse([]super.Type{lhs, rhs})
		case "in":
			if ref, ok := plainPath(e.LHS); ok {
				return c.narrowPath(typ, ref, inKeep)
			}
		case "==":
			if kind, ref, ok := kindTest(e); ok {
				return c.narrowPath(typ, ref, func(t super.Type) bool {
					return hasKind(t, kind)
				})
			}
			fallthrough
		case "!=", "<", "<=", ">", ">=":
			if ref, ok := isOkTest(e); ok {
				return c.narrowPath(typ, ref, isPresent)
			}
			for _, operand := range []sem.Expr{e.LHS, e.RHS} {
				if ref, ok := plainPath(operand); ok {
					typ = c.narrowPath(typ, ref, cmpKeep)
				}
			}
		}
	case *sem.CallExpr:
		switch {
		case e.Tag == "is" && len(e.Args) == 2:
			ref, ok := plainPath(e.Args[0])
			target := c.typeOfTypeLiteral(e.Args[1])
			if ok && target != nil {
				return c.narrowIs(typ, ref, target)
			}
		case e.Tag == "is_error" && len(e.Args) == 1:
			if ref, ok := plainPath(e.Args[0]); ok {
				return c.narrowPath(typ, ref, isErrorLike)
			}
		}
	case *sem.UnaryExpr:
		if e.Op != "!" {
			break
		}
		switch operand := e.Operand.(type) {
		case *sem.IsNullExpr:
			// IS NULL of an error is the error, so NOT of it is too.
			if ref, ok := plainPath(operand.Expr); ok {
				return c.narrowPath(typ, ref, func(t super.Type) bool {
					return notErrorLike(t) && under(t) != super.TypeNull
				})
			}
		case *sem.CallExpr:
			if operand.Tag == "is_error" && len(operand.Args) == 1 {
				if ref, ok := plainPath(operand.Args[0]); ok {
					return c.narrowPath(typ, ref, notErrorLike)
				}
			}
		}
	}
	return typ
}

// narrowCase returns typ, the type of the input of a case of a switch
// operator, narrowed by the case.  expr is the expression of the switch (nil
// if each case is a predicate) and val is the value or predicate of the case
// (nil for a default case), e.g., the path of the case "record" of switch
// kind(this) is checked only with the record types of typ.  A switch on an
// expression matches a case by the bytes of the value without its type
// (e.g., null matches case "") so only a switch on kind(p), whose values are
// strings, narrows.  The cases are tested in order so a later case could be
// narrowed by the failure of the earlier ones, but that would take narrowing
// by the negation of a predicate, which generally isn't safe (see narrow).
func (c *checker) narrowCase(typ super.Type, expr, val sem.Expr) super.Type {
	if val == nil {
		return typ
	}
	if expr == nil {
		return c.narrow(typ, val)
	}
	test := sem.NewBinaryExpr(nil, "==", expr, val)
	if _, _, ok := kindTest(test); ok {
		return c.narrow(typ, test)
	}
	return typ
}

// narrowPath returns typ with the type of the value at path narrowed to the
// member types for which keep is true.  When no value of typ can have a
// value at path for which keep is true, it returns typ unchanged since
// the filter drops every value.
func (c *checker) narrowPath(typ super.Type, path field.Chain, keep func(super.Type) bool) super.Type {
	if out, ok := c.narrowAt(defuse(typ), path, keep); ok {
		return out
	}
	return typ
}

// narrowAt is narrowPath without the fallback and returns false if no value
// of typ can satisfy keep at path.
func (c *checker) narrowAt(typ super.Type, path field.Chain, keep func(super.Type) bool) (super.Type, bool) {
	typ = defuse(typ)
	if hasUnknown(typ) {
		return typ, true
	}
	if named, ok := typ.(*super.TypeNamed); ok {
		// A value of a named type keeps its type, so we can't narrow
		// the named type's underlying type but we can tell if no value
		// of it satisfies keep.  Some predicates (e.g., is_error) apply
		// to the value inside a named union, so check its members too.
		if len(path) == 0 {
			if keep(typ) {
				return typ, true
			}
			if _, ok := under(named).(*super.TypeUnion); !ok {
				return typ, false
			}
		}
		_, ok := c.narrowAt(named.Type, path, keep)
		return typ, ok
	}
	if u, ok := under(typ).(*super.TypeUnion); ok {
		// A value of a union type is a value of one of its member types.
		var types []super.Type
		changed := false
		for _, t := range u.Types {
			out, ok := c.narrowAt(t, path, keep)
			if !ok {
				changed = true
				continue
			}
			if out != t {
				changed = true
			}
			types = append(types, out)
		}
		switch {
		case len(types) == 0:
			return nil, false
		case !changed:
			return typ, true
		}
		return c.unionOf(super.Flatten(types)), true
	}
	if len(path) == 0 {
		return typ, keep(typ)
	}
	switch t := under(typ).(type) {
	case *super.TypeRecord:
		if path[0].Nullish {
			// A missing value of a SQL column reference is null.
			keep = missingAsNull(keep)
		}
		k, ok := t.IndexOfField(path[0].ID)
		if !ok {
			_, ok := c.narrowAt(super.TypeMissing, path[1:], keep)
			return typ, ok
		}
		ftype, ok := c.narrowAt(t.Fields[k].Type, path[1:], keep)
		if !ok {
			return nil, false
		}
		if ftype == t.Fields[k].Type {
			return typ, true
		}
		fields := append([]super.Field(nil), t.Fields...)
		fields[k] = super.NewField(fields[k].Name, ftype)
		rec, err := c.t.sctx.LookupTypeRecord(fields)
		if err != nil {
			return typ, true
		}
		return rec, true
	case *super.TypeOfMissing:
		// A field of a missing value is missing.
		return typ, keep(typ)
	}
	// We don't know the type of the value at path (e.g., a field of a map
	// or an error), so leave typ as is.
	return typ, true
}

// narrowIs returns typ with the type of the value at path narrowed by
// is(path, <target>), which is true only for a value whose type is target.
func (c *checker) narrowIs(typ super.Type, path field.Chain, target super.Type) super.Type {
	// If the type at path is target itself (e.g., a union type), the
	// value's type is target, so narrowAt must not split it into members.
	if out, ok := c.narrowWhole(defuse(typ), path, target); ok {
		return out
	}
	if _, ok := target.(*super.TypeUnion); ok {
		// narrowAt splits unions into their members, none of which is
		// target, so we can't narrow.
		return typ
	}
	// A missing value is error("missing") at runtime.
	errMissing := c.t.sctx.LookupTypeError(super.TypeString)
	return c.narrowPath(typ, path, func(t super.Type) bool {
		return t == target || isMissingType(t) && target == errMissing
	})
}

// narrowWhole returns typ unchanged and true if the type of the value at path
// in every record type of typ is target.
func (c *checker) narrowWhole(typ super.Type, path field.Chain, target super.Type) (super.Type, bool) {
	at := typ
	for _, elem := range path {
		rec, ok := under(defuse(at)).(*super.TypeRecord)
		if !ok {
			return nil, false
		}
		k, ok := rec.IndexOfField(elem.ID)
		if !ok {
			return nil, false
		}
		at = rec.Fields[k].Type
	}
	if defuse(at) == target {
		return typ, true
	}
	return nil, false
}

// typeOfTypeLiteral returns the type of e if e is a type expression or a
// literal type value and nil otherwise.
func (c *checker) typeOfTypeLiteral(e sem.Expr) super.Type {
	if prim, ok := e.(*sem.PrimitiveExpr); ok {
		val, err := sup.ParseValue(c.t.sctx, prim.Value)
		if err != nil || val.Type() != super.TypeType {
			return nil
		}
		typ, err := c.t.sctx.LookupByValue(val.Bytes())
		if err != nil {
			return nil
		}
		return typ
	}
	return c.typeOfTypeExpr(e)
}

// plainPath returns the chain of e if e is a field reference (or this) whose
// chain has no ?. elements.  (An element of a SQL column reference is
// nullish, i.e., it's null rather than missing when the field is absent.)
func plainPath(e sem.Expr) (field.Chain, bool) {
	this, ok := e.(*sem.ThisExpr)
	if !ok {
		return nil, false
	}
	for _, elem := range this.Chain {
		if elem.Noneish {
			return nil, false
		}
	}
	return this.Chain, true
}

// isOkTest returns the path p if e is the translation of is_ok(p), i.e.,
// (ok(p) ?? none) != none.
func isOkTest(e *sem.BinaryExpr) (field.Chain, bool) {
	if e.Op != "!=" {
		return nil, false
	}
	coalesce, ok := e.LHS.(*sem.BinaryExpr)
	if !ok || coalesce.Op != "??" || !isNoneLiteral(coalesce.RHS) || !isNoneLiteral(e.RHS) {
		return nil, false
	}
	call, ok := coalesce.LHS.(*sem.CallExpr)
	if !ok || call.Tag != "ok" || len(call.Args) != 1 {
		return nil, false
	}
	return plainPath(call.Args[0])
}

func isNoneLiteral(e sem.Expr) bool {
	p, ok := e.(*sem.PrimitiveExpr)
	return ok && p.Value == "none"
}

// kindTest returns the kind and path p if e is kind(p) == "<kind>" or
// "<kind>" == kind(p).
func kindTest(e *sem.BinaryExpr) (string, field.Chain, bool) {
	call, lit := e.LHS, e.RHS
	if _, ok := lit.(*sem.CallExpr); ok {
		call, lit = lit, call
	}
	kindCall, ok := call.(*sem.CallExpr)
	if !ok || kindCall.Tag != "kind" || len(kindCall.Args) != 1 {
		return "", nil, false
	}
	prim, ok := lit.(*sem.PrimitiveExpr)
	if !ok {
		return "", nil, false
	}
	val, err := sup.ParseValue(super.NewContext(), prim.Value)
	if err != nil || !val.IsString() {
		return "", nil, false
	}
	path, ok := plainPath(kindCall.Args[0])
	return val.AsString(), path, ok
}

// hasKind returns true if a value of type typ (but not a union type) may
// have the kind kind according to the kind function.
func hasKind(typ super.Type, kind string) bool {
	if kind == "union" {
		// A fused type's union types are the types of its values but
		// a union value's kind is "union", so we can't tell.
		return true
	}
	if isMissingType(typ) {
		// A missing value is an error at runtime.
		return kind == "error"
	}
	if under(typ) == super.TypeType {
		// The kind of a type value is the kind of its type.
		return true
	}
	return under(typ).Kind().String() == kind
}

// isPresent returns true if a value of type typ may satisfy is_ok(), i.e.,
// it's not an error, missing, none, or null.
func isPresent(typ super.Type) bool {
	return !isErrorType(typ) && !isMissingType(typ) && !isNoneishType(typ)
}

// isErrorLike returns true if a value of type typ may be an error, which
// includes a missing value.
func isErrorLike(typ super.Type) bool {
	return isErrorType(typ) || isMissingType(typ)
}

func notErrorLike(typ super.Type) bool {
	return !isErrorLike(typ)
}

// missingAsNull returns keep for a value that's null when it's missing.
func missingAsNull(keep func(super.Type) bool) func(super.Type) bool {
	return func(t super.Type) bool {
		if isMissingType(t) {
			return keep(super.TypeNull)
		}
		return keep(t)
	}
}

// isComparable returns true if a value of type typ may be true in a comparison,
// i.e., it's not an error, missing, or null.
func isComparable(typ super.Type) bool {
	return !isErrorType(typ) && !isMissingType(typ) && under(typ) != super.TypeNull
}
