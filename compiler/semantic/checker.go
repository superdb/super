package semantic

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/superdb/super"
	"github.com/superdb/super/compiler/ast"
	"github.com/superdb/super/compiler/semantic/sem"
	"github.com/superdb/super/sup"
)

type checker struct {
	t       *translator
	checked map[super.Type]super.Type
	unknown *super.TypeError
	estack  []errlist
}

func newChecker(t *translator) *checker {
	return &checker{
		t:       t,
		unknown: t.sctx.LookupTypeError(t.sctx.MustLookupTypeRecord(nil)),
		checked: make(map[super.Type]super.Type),
	}
}

func (c *checker) seq(typ super.Type, seq sem.Seq) super.Type {
	for len(seq) > 0 {
		typ = defuse(typ)
		if fork, ok := seq[0].(*sem.ForkOp); ok && len(seq) >= 2 {
			if join, ok := seq[1].(*sem.JoinOp); ok {
				typ = c.join(c.fork(typ, fork), join)
				seq = seq[2:]
				continue
			}
		}
		if swtch, ok := seq[0].(*sem.SwitchOp); ok && len(seq) >= 2 {
			if join, ok := seq[1].(*sem.JoinOp); ok {
				typ = c.join(c.swtch(typ, swtch), join)
				seq = seq[2:]
				continue
			}
		}
		typ = c.op(typ, seq[0])
		seq = seq[1:]
	}
	return typ
}

func (c *checker) op(typ super.Type, op sem.Op) super.Type {
	switch op := op.(type) {
	//
	// Scanners first
	//
	case *sem.FileScan:
		if op.Type == nil {
			return c.unknown
		}
		return op.Type
	case *sem.TableScan:
		if op.Type == nil {
			return c.unknown
		}
		return op.Type
	case *sem.HTTPScan,
		*sem.RobotScan,
		*sem.DBMetaScan,
		*sem.TableMetaScan,
		*sem.CommitMetaScan,
		*sem.DeleteScan:
		return c.unknown
	case *sem.NullScan:
		return super.TypeNull
	//
	// Ops in alphabetical oder
	//
	case *sem.AggregateOp:
		aggPaths := c.assignments(typ, op.Aggs)
		keyPaths := c.assignments(typ, op.Keys)
		for k, path := range keyPaths {
			c.groupKey(op.Keys[k].RHS, path.typ)
		}
		return c.pathsToType(append(keyPaths, aggPaths...))
	case *sem.BadOp:
		return c.unknown
	case *sem.CountOp:
		var elems []sem.RecordElem
		if op.Expr != nil {
			elems = append(elems, op.Expr.(*sem.RecordExpr).Elems...)
		}
		elems = append(elems, &sem.FieldElem{
			Name:  op.Alias,
			Value: sem.NewLiteral(op, super.NewUint64(0), c.t.defs),
		})
		return c.recordElems(typ, elems)
	case *sem.CutOp:
		return c.pathsToType(c.assignments(typ, op.Args))
	case *sem.DebugOp:
		c.expr(typ, op.Expr)
		return typ
	case *sem.DistinctOp:
		c.expr(typ, op.Expr)
		return typ
	case *sem.DropOp:
		drops := c.lvalsToPaths(op.Args)
		if drops == nil {
			return c.unknown
		}
		return c.dropPaths(typ, drops)
	case *sem.FilterOp:
		c.boolean(op.Expr, c.expr(typ, op.Expr))
		return typ
	case *sem.ForkOp:
		return c.fuse(c.fork(typ, op))
	case *sem.FuseOp:
		return typ
	case *sem.HeadOp:
		return typ
	case *sem.LoadOp:
		return c.unknown
	case *sem.MergeOp:
		c.sortExprs(typ, op.Exprs)
		return typ
	case *sem.JoinOp:
		c.error(op, errors.New("join requires two inputs"))
		return c.unknown
	case *sem.OutputOp:
		return typ
	case *sem.PassOp:
		return typ
	case *sem.PutOp:
		return c.putPaths(typ, c.assignments(typ, op.Args))
	case *sem.RenameOp:
		// TBD
		return c.unknown
	case *sem.SkipOp:
		return typ
	case *sem.SortOp:
		return typ
	case *sem.SwitchOp:
		var types []super.Type
		exprType := c.expr(typ, op.Expr)
		for _, cs := range op.Cases {
			c.expr(exprType, cs.Expr)
			types = append(types, c.seq(typ, cs.Path))
		}
		return c.fuse(types)
	case *sem.TailOp:
		return typ
	case *sem.TopOp:
		c.sortExprs(typ, op.Exprs)
		return typ
	case *sem.UniqOp:
		return typ
	case *sem.UnnestOp:
		return c.seq(c.unnest(op.Expr, c.expr(typ, op.Expr)), op.Body)
	case *sem.ValuesOp:
		return c.fuse(c.exprs(typ, op.Exprs))
	default:
		panic(op)
	}
}

func (c *checker) fork(typ super.Type, fork *sem.ForkOp) []super.Type {
	var types []super.Type
	for _, seq := range fork.Paths {
		types = append(types, c.seq(typ, seq))
	}
	return types
}

func (c *checker) swtch(typ super.Type, op *sem.SwitchOp) []super.Type {
	var types []super.Type
	exprType := c.expr(typ, op.Expr)
	for _, cs := range op.Cases {
		c.expr(exprType, cs.Expr)
		types = append(types, c.seq(typ, cs.Path))
	}
	return types
}

func (c *checker) join(types []super.Type, op *sem.JoinOp) super.Type {
	if len(types) != 2 {
		c.error(op, errors.New("join requires two query inputs"))
	}
	typ := c.record([]super.Field{
		super.NewField(op.LeftAlias, types[0]),
		super.NewField(op.RightAlias, types[1]),
	})
	c.expr(typ, op.Cond)
	return typ
}

func (c *checker) unnest(loc ast.Node, typ super.Type) super.Type {
	c.pushErrs()
	typ, ok := c.unnestCheck(loc, typ)
	errs := c.popErrs()
	if !ok {
		c.keepErrs(errs)
	}
	return typ
}

func (c *checker) unnestCheck(loc ast.Node, typ super.Type) (super.Type, bool) {
	switch typ := super.TypeUnder(typ).(type) {
	case *super.TypeError:
		if isUnknown(typ) {
			return c.unknown, true
		}
		c.error(loc, errors.New("unnested record cannot be an error"))
		return c.unknown, false
	case *super.TypeUnion:
		var types []super.Type
		var ok bool
		for _, t := range typ.Types {
			typ, tok := c.unnestCheck(loc, t)
			if tok {
				types = append(types, typ)
				ok = true
			}
		}
		return c.fuse(types), ok
	case *super.TypeOption:
		return c.unnestCheck(loc, typ.Type)
	case *super.TypeArray:
		return typ.Type, true
	case *super.TypeRecord:
		if len(typ.Fields) != 2 {
			c.error(loc, errors.New("unnested record must have two fields"))
			return c.unknown, false
		}
		arrayField := typ.Fields[1]
		if isUnknown(arrayField.Type) {
			return typ, true
		}
		arrayElemType, ok := c.hasArray(arrayField.Type)
		if !ok {
			c.error(loc, errors.New("unnested record must have array for second field"))
			return c.unknown, false
		}
		fields := []super.Field{typ.Fields[0], {Name: arrayField.Name, Type: arrayElemType}}
		return c.record(fields), true
	default:
		c.error(loc, errors.New("unnest value must be array or record"))
		return c.unknown, false
	}
}

// assignments returns a set of paths where the LHS is a dotted path. If LHS is more
// complex than a dotted path (e.g., depends on the input data, e.g., "put this[fld]:=10"),
// then the elems of that path slot is null.
func (c *checker) assignments(in super.Type, assignments []sem.Assignment) []pathType {
	var paths []pathType
	for _, a := range assignments {
		var path []string
		if this, ok := a.LHS.(*sem.ThisExpr); ok {
			path = this.Chain.Path()
		}
		typ := c.expr(in, a.RHS)
		paths = append(paths, pathType{path, typ})
	}
	return paths
}

func (c *checker) sortExprs(typ super.Type, exprs []sem.SortExpr) {
	for _, se := range exprs {
		c.expr(typ, se.Expr)
	}
}

func (c *checker) exprs(typ super.Type, exprs []sem.Expr) []super.Type {
	var types []super.Type
	for _, e := range exprs {
		types = append(types, c.expr(typ, e))
	}
	return types
}

func (c *checker) expr(typ super.Type, e sem.Expr) super.Type {
	//XXX for now, just deref any super types.  A project is to make
	// a mixed-static/super type checker that understands supers
	// are summary types and regular types are exact types.
	// question is to make mixed-type branches (e.g., case branches)
	// a super or a union.  Latter would impact the design of type-safety
	// semantics of dereferencing such values and should case allow None?
	typ = defuse(typ)
	switch e := e.(type) {
	case nil:
		return c.unknown
	case *sem.AggFunc:
		if e.Expr != nil {
			c.notMissing(e.Expr, c.expr(typ, e.Expr))
		}
		if e.Filter != nil {
			c.notMissing(e.Filter, c.expr(typ, e.Filter))
		}
		// XXX This will be handled in a subsequent PR where we add type signatures
		// to the package containing the agg func implementatons.
		return c.unknown
	case *sem.AggRef:
		// AggRef's should never appear here but they can show up when
		// we are traversing the demand exprs in a SQL SELECT where we
		// do not care about type checking (because ORDER BY will re-do
		// the type checks with the output scope available).
		return c.unknown
	case *sem.ArrayExpr:
		return c.array(c.arrayElems(typ, e.Elems))
	case *sem.BadExpr:
		return c.unknown
	case *sem.BinaryExpr:
		lhs := c.expr(typ, e.LHS)
		rhs := c.expr(typ, e.RHS)
		return c.binary(e.Op, e, e.LHS, e.RHS, lhs, rhs)
	case *sem.CallExpr:
		if e.Tag == "ok" {
			return c.ok(typ, e)
		}
		var types []super.Type
		for _, e := range e.Args {
			types = append(types, c.expr(typ, e))
		}
		if isBuiltin(e.Tag) {
			return c.callBuiltin(e, types)
		}
		return c.callFunc(e, types)
	case *sem.CondExpr:
		c.boolean(e.Cond, c.expr(typ, e.Cond))
		c.pushErrs()
		thenType := c.expr(typ, e.Then)
		thenErrs := c.popErrs()
		c.pushErrs()
		elseType := c.expr(typ, e.Else)
		elseErrs := c.popErrs()
		if len(thenErrs) != 0 && len(elseErrs) != 0 {
			c.error(thenErrs[0].loc, fmt.Errorf("no valid conditional branch found: %w", thenErrs[0].err))
			c.error(elseErrs[0].loc, fmt.Errorf("no valid conditional branch found: %w", elseErrs[0].err))
		}
		return c.fuse([]super.Type{thenType, elseType})
	case *sem.DotExpr:
		return c.deref(e.Node, c.expr(typ, e.LHS), e.RHS, e.Noneish)
	case *sem.IndexExpr:
		typ, _ := c.indexOf(e.Expr, e.Index, c.expr(typ, e.Expr), c.expr(typ, e.Index))
		return typ
	case *sem.IsNullExpr:
		c.expr(typ, e.Expr)
		return super.TypeBool
	case *sem.MapCallExpr:
		containerType := c.expr(typ, e.Expr)
		elemType, ok := c.isContainer(containerType)
		if !ok {
			c.error(e.Expr, errors.New("map entity must be an array or set"))
			return c.unknown
		}
		c.pushErrs()
		lambdaType := c.expr(elemType, e.Lambda)
		errs := c.popErrs()
		if len(errs) != 0 {
			c.error(errs[0].loc, fmt.Errorf("in function called from map: %w", errs[0].err))
		}
		return lambdaType
	case *sem.MapExpr:
		// fuser could take type at a time instead of array
		var keyTypes, valTypes []super.Type
		for _, entry := range e.Entries {
			keyTypes = append(keyTypes, c.expr(typ, entry.Key))
			valTypes = append(valTypes, c.expr(typ, entry.Value))
		}
		return c.t.sctx.LookupTypeMap(c.fuse(keyTypes), c.fuse(valTypes))
	case *sem.PrimitiveExpr:
		val, err := sup.ParseValue(c.t.sctx, e.Value)
		if err != nil {
			return c.unknown
		}
		if !super.IsPrimitiveType(val.Type()) {
			panic(e)
		}
		return val.Type()
	case *sem.RecordExpr:
		return c.recordElems(typ, e.Elems)
	case *sem.RegexpMatchExpr:
		if !hasString(c.expr(typ, e.Expr)) {
			c.error(e.Expr, errors.New("string match must apply to type string"))
		}
		return super.TypeBool
	case *sem.RegexpSearchExpr:
		if !hasString(c.expr(typ, e.Expr)) {
			c.error(e.Expr, errors.New("string match must apply to type string"))
		}
		return super.TypeBool
	case *sem.SearchTermExpr:
		c.expr(typ, e.Expr)
		return super.TypeBool
	case *sem.SetExpr:
		return c.t.sctx.LookupTypeArray(c.arrayElems(typ, e.Elems))
	case *sem.SliceExpr:
		if e.From != nil {
			c.integer(e.From, c.expr(typ, e.From))
		}
		if e.To != nil {
			c.integer(e.To, c.expr(typ, e.To))
		}
		container := c.expr(typ, e.Expr)
		c.sliceable(e.Expr, container)
		return container
	case *sem.SubqueryExpr:
		typ = c.seq(typ, e.Body)
		if e.Array {
			typ = c.t.sctx.LookupTypeArray(typ)
		}
		return typ
	case *sem.ThisExpr:
		return c.this(e.Node, e, typ)
	case *sem.TypeExpr:
		return super.TypeType
	case *sem.UnaryExpr:
		typ = c.expr(typ, e.Operand)
		switch e.Op {
		case "-":
			c.number(e.Operand, typ)
			return typ
		case "!":
			c.boolean(e, typ)
			return super.TypeBool
		default:
			panic(e.Op)
		}
	default:
		panic(e)
	}
}

func (c *checker) ok(typ super.Type, call *sem.CallExpr) super.Type {
	if len(call.Args) != 1 {
		panic(call)
	}
	c.pushErrs()
	typ = c.expr(typ, call.Args[0])
	return c.checkOk(typ, call, c.popErrs())
}

func (c *checker) checkOk(typ super.Type, call *sem.CallExpr, errs []errloc) super.Type {
	others, _ := stripMissing(errs)
	if len(others) != 0 {
		c.keepErrs(others[:1])
		return c.unknown
	}
	// ok() turns a missing value into none, so its value is either one of
	// the types that aren't missing or none.
	typ, ok := c.withoutTypes(defuse(typ), isMissingType)
	if !ok {
		// The value is always missing so ok() is always none.
		return super.TypeNone
	}
	return c.orNone(typ)
}

// orNone returns fusion(typ|none), the type of a value that is either of
// type typ or a bare none, as for ok(e), which the runtime produces as
// plain values of typ or none rather than as options.
func (c *checker) orNone(typ super.Type) super.Type {
	if hasUnknown(typ) {
		return typ
	}
	typ = defuse(typ)
	if super.TypeUnder(typ) == super.TypeNone {
		return typ
	}
	types := super.Flatten([]super.Type{typ, super.TypeNone})
	return c.t.sctx.MustLookupTypeFusion(c.t.sctx.MustLookupTypeUnion(types))
}

// withoutTypes returns typ with the types for which drop is true removed
// from it (from its members if it's a union).  It returns false if nothing
// is left.
func (c *checker) withoutTypes(typ super.Type, drop func(super.Type) bool) (super.Type, bool) {
	u, ok := super.TypeUnder(typ).(*super.TypeUnion)
	if !ok {
		return typ, !drop(typ)
	}
	var keep []super.Type
	for _, t := range u.Types {
		if !drop(t) {
			keep = append(keep, t)
		}
	}
	switch len(keep) {
	case 0:
		return nil, false
	case 1:
		return keep[0], true
	case len(u.Types):
		return typ, true
	}
	return c.union(keep), true
}

func isMissingType(typ super.Type) bool {
	return super.TypeUnder(typ) == super.TypeMissing
}

func isNoneType(typ super.Type) bool {
	return super.TypeUnder(typ) == super.TypeNone
}

// isNoneishType reports whether typ is a type whose values ?? replaces with
// its right-hand side.
func isNoneishType(typ super.Type) bool {
	switch super.TypeUnder(typ) {
	case super.TypeNone, super.TypeNull:
		return true
	}
	return false
}

// coalesce returns the type of lhs ?? rhs: the types of lhs that aren't
// none or null, with any options unwrapped, fused with the type of rhs.
func (c *checker) coalesce(lhs, rhs super.Type) super.Type {
	if hasUnknown(lhs) {
		return c.fuse([]super.Type{lhs, rhs})
	}
	var types []super.Type
	unwrap := func(t super.Type) {
		t = deoption(t)
		if typ, ok := c.withoutTypes(t, isNoneishType); ok {
			types = append(types, typ)
		}
	}
	if u, ok := super.TypeUnder(lhs).(*super.TypeUnion); ok {
		for _, t := range u.Types {
			unwrap(t)
		}
	} else {
		unwrap(lhs)
	}
	return c.fuse(append(types, rhs))
}

func (c *checker) optionize(typ super.Type) super.Type {
	if hasUnknown(typ) {
		return typ
	}
	return c.t.sctx.Optionize(typ)
}

func defuse(typ super.Type) super.Type {
	if typ, ok := typ.(*super.TypeFusion); ok {
		return typ.Type
	}
	return typ
}

var errMissing = errors.New("field may be missing (consider ok())")
var errNone = errors.New("none not handled (consider ?? or ?.)")

func (c *checker) binary(op string, loc, lloc, rloc ast.Node, lhs, rhs super.Type) super.Type {
	lhs = defuse(lhs)
	rhs = defuse(rhs)
	if op != "==" && op != "!=" && op != "??" {
		if !isUnknown(lhs) {
			if hasMissing(lhs) {
				c.error(lloc, errMissing)
				return c.unknown
			}
			if hasOption(lhs) || hasNone(lhs) {
				c.error(lloc, fmt.Errorf("%s: %w", sup.FormatType(lhs), errNone))
				return c.unknown
			}
		}
		if !isUnknown(rhs) {
			if hasMissing(rhs) {
				c.error(rloc, errMissing)
				return c.unknown
			}
			if hasOption(rhs) || hasNone(rhs) {
				c.error(rloc, fmt.Errorf("%s: %w", sup.FormatType(rhs), errNone))
				return c.unknown
			}
		}
	}
	switch strings.ToLower(op) {
	case "and", "or":
		c.logical(lloc, rloc, lhs, rhs)
		return super.TypeBool
	case "in", "not in":
		c.in(loc, lloc, rloc, lhs, rhs)
		return super.TypeBool
	case "==", "!=", "=", "<>":
		return c.equality(lhs, rhs)
	case "<", "<=", ">", ">=":
		return c.comparison(lhs, rhs)
	case "+":
		return c.plus(loc, lhs, rhs)
	case "-", "*", "/", "%":
		return c.arithmetic(lloc, rloc, lhs, rhs)
	case "??":
		// ?? handles none (and null) but not a field that may be missing,
		// which ok() converts to none.  A value that is only ever missing
		// (e.g., the literal missing) passes through ?? unchanged.
		if !hasUnknown(lhs) && mayBeMissing(lhs) {
			c.error(lloc, errMissing)
			return c.unknown
		}
		return c.coalesce(lhs, rhs)
	default:
		panic(op)
	}
}

func (c *checker) this(loc ast.Node, this *sem.ThisExpr, typ super.Type) super.Type {
	for _, comp := range this.Chain {
		typ = c.deref(loc, typ, comp.ID, comp.Noneish)
		if comp.Nullish {
			// A nullish deref (e.g., a SQL column reference) produces
			// null in place of a missing field.
			typ = c.missingAsNull(typ)
		}
	}
	return typ
}

// missingAsNull returns typ with any missing type replaced by null.
func (c *checker) missingAsNull(typ super.Type) super.Type {
	under := defuse(typ)
	if hasUnknown(under) || !hasMissing(under) {
		return typ
	}
	rest, ok := c.withoutTypes(under, isMissingType)
	if !ok {
		return super.TypeNull
	}
	return c.fuse([]super.Type{rest, super.TypeNull})
}

func (c *checker) isContainer(containerType super.Type) (super.Type, bool) {
	switch typ := super.TypeUnder(containerType).(type) {
	case *super.TypeArray:
		return defuse(typ.Type), true
	case *super.TypeSet:
		return defuse(typ.Type), true
	case *super.TypeError:
		if isUnknown(typ) {
			return c.unknown, true
		}
	}
	return nil, false
}

func (c *checker) arrayElems(typ super.Type, elems []sem.ArrayElem) super.Type {
	fuser := c.newFuser()
	for _, elem := range elems {
		switch elem := elem.(type) {
		case *sem.SpreadElem:
			spreadType := c.expr(typ, elem.Expr)
			if arrayType, ok := spreadType.(*super.TypeArray); ok {
				spreadType = arrayType.Type
			} else {
				spreadType = c.unknown
			}
			fuser.fuse(spreadType)
		case *sem.ExprElem:
			// A none element is dropped from the array at runtime so it
			// doesn't contribute to the element type.
			if elemType := c.expr(typ, elem.Expr); !isNoneType(elemType) {
				fuser.fuse(elemType)
			}
		default:
			panic(elem)
		}
	}
	return defuse(fuser.Type())
}

func (c *checker) recordElems(typ super.Type, elems []sem.RecordElem) super.Type {
	var types []super.Type
	for _, elem := range elems {
		switch elem := elem.(type) {
		case *sem.SpreadElem:
			elemType := c.expr(typ, elem.Expr)
			if hasUnknown(elemType) {
				// If we're spreading an unknown type into this record, we don't
				// know the result at all.  Return unknown for the whole thing.
				return c.unknown
			}
			types = append(types, elemType)
		case *sem.FieldElem:
			types = append(types, c.expr(typ, elem.Value))
		default:
			panic(elem)
		}
	}
	return c.formRecordElems(elems, types)
}

func (c *checker) formRecordElems(elems []sem.RecordElem, types []super.Type) super.Type {
	var fields []super.Field
	order := make(map[string]int)
	for k, elem := range elems {
		typ := types[k]
		switch elem := elem.(type) {
		case *sem.SpreadElem:
			if hasUnknown(typ) {
				// If we're spreading an unknown type into this record, we don't
				// know the result at all.  Return unknown for the whole thing.
				return c.unknown
			}
			// If it's not unknown, it should have passed type checking as a record.
			recType, ok := super.TypeUnder(typ).(*super.TypeRecord)
			if !ok {
				//XXX
				return c.unknown
			}
			for _, f := range recType.Fields {
				if k, ok := order[f.Name]; ok {
					fields[k] = f
				} else {
					order[f.Name] = len(fields)
					fields = append(fields, f)
				}
			}
		case *sem.FieldElem:
			if isNoneType(typ) {
				// A field whose value is none is deleted from the record at
				// runtime, including a field of the same name from a spread.
				if k, ok := order[elem.Name]; ok {
					fields = slices.Delete(fields, k, k+1)
					delete(order, elem.Name)
					for name, j := range order {
						if j > k {
							order[name] = j - 1
						}
					}
				}
				continue
			}
			if k, ok := order[elem.Name]; ok {
				// XXX We need to handle duplicate fields here.  SQL has logic for
				// this but there are some bugs in how the runtime and optimizer handles
				// them in general.  This will be fixed in a subsequent PR.
				fields[k] = super.NewField(elem.Name, typ)
			} else {
				order[elem.Name] = len(fields)
				fields = append(fields, super.NewField(elem.Name, typ))
			}
		default:
			panic(elem)
		}
	}
	return c.record(fields)
}

func (c *checker) record(fields []super.Field) super.Type {
	if slices.ContainsFunc(fields, func(f super.Field) bool {
		return f.Type == c.unknown
	}) {
		return c.unknown
	}
	return c.t.sctx.MustLookupTypeRecord(fields)
}

func (c *checker) union(types []super.Type) super.Type {
	if slices.ContainsFunc(types, func(t super.Type) bool {
		return t == c.unknown
	}) {
		return c.unknown
	}
	return c.t.sctx.MustLookupTypeUnion(types)
}

func (c *checker) array(typ super.Type) super.Type {
	if typ == c.unknown {
		return typ
	}
	return c.t.sctx.LookupTypeArray(typ)
}

func (c *checker) callBuiltin(call *sem.CallExpr, args []super.Type) super.Type {
	// XXX This will be handled in a subsequent PR where we add type signatures
	// to the package containing the built-in function implementatons.
	return c.unknown
}

func (c *checker) callFunc(call *sem.CallExpr, args []super.Type) super.Type {
	f, ok := c.t.resolver.funcs[call.Tag]
	if !ok {
		return c.unknown
	}
	if len(args) != len(f.params) {
		// The translator has already checked that len(args) is len(params)
		// but when there's an error, mismatches can still show up here so
		// we ignore these here.
		return c.unknown
	}
	fields := make([]super.Field, 0, len(args))
	for k, param := range f.params {
		fields = append(fields, super.Field{Name: param, Type: args[k]})
	}
	argsType := c.t.sctx.MustLookupTypeRecord(fields)
	if typ, ok := c.checked[argsType]; ok {
		return typ
	}
	// For recursive calls, we use unknown for the base type to halt the recursion
	// then fill the actual type computed from the unknown base type.  This has the
	// downside that we now have an error unknown in the sum type of the return value
	// of all recursive functions.  When we add (optional) type signatures to functions,
	// this problem will (partially) go away.
	c.checked[argsType] = c.unknown
	typ := c.expr(argsType, f.body)
	c.checked[argsType] = typ
	return typ
}

type pathType struct {
	elems []string
	typ   super.Type
}

func (c *checker) pathsToType(paths []pathType) super.Type {
	return c.putPaths(c.t.sctx.MustLookupTypeRecord(nil), paths)
}

func (c *checker) dropPaths(typ super.Type, drops []path) super.Type {
	for _, drop := range drops {
		typ = c.dropPath(typ, drop)
	}
	return typ
}

func (c *checker) dropPath(typ super.Type, drop path) super.Type {
	if len(drop.elems) == 0 {
		return nil
	}
	// Drop is a little tricky since it passes through non-record values so
	// we need to preserve any union type presented to its input. pickRec returns
	// a copy of the types slice so we can modify it.
	types, pick := pickRec(typ)
	if types == nil {
		// drop passes through non-records
		return typ
	}
	rec := super.TypeUnder(types[pick]).(*super.TypeRecord)
	off, ok := rec.IndexOfField(drop.elems[0])
	if !ok {
		if !hasUnknown(typ) {
			c.error(drop.loc, errors.New("expression not present in drop input"))
		}
		return c.unknown
	}
	fields := slices.Clone(rec.Fields)
	childType := c.dropPath(fields[off].Type, path{drop.loc, drop.elems[1:]})
	if childType == nil {
		fields = slices.Delete(fields, off, off+1)
	} else {
		fields[off].Type = childType
	}
	types[pick] = c.record(fields)
	if len(types) > 1 {
		return c.union(super.Flatten(types))
	}
	return types[0]
}

func pickRec(typ super.Type) ([]super.Type, int) {
	switch typ := deoption(super.TypeUnder(typ)).(type) {
	case *super.TypeRecord:
		return []super.Type{typ}, 0
	case *super.TypeUnion:
		types := slices.Clone(typ.Types)
		for k := range types {
			if _, ok := super.TypeUnder(types[k]).(*super.TypeRecord); ok {
				return types, k
			}
		}
	}
	return nil, 0
}

func deoption(typ super.Type) super.Type {
	if typ, ok := super.TypeUnder(typ).(*super.TypeOption); ok {
		return typ.Type
	}
	return typ
}

func (c *checker) putPaths(inType super.Type, puts []pathType) super.Type {
	// Fuse each path as a single-record path into the input type.
	//fuser.fuse(typ)
	recType, ok := super.TypeUnder(inType).(*super.TypeRecord)
	if !ok {
		return c.unknown
	}
	for _, put := range puts {
		typ := c.putInto(recType, put.elems, put.typ)
		var ok bool
		recType, ok = typ.(*super.TypeRecord)
		if !ok {
			return c.unknown
		}
	}
	return recType
}

func (c *checker) putInto(recType *super.TypeRecord, path []string, leaf super.Type) super.Type {
	if len(path) == 0 {
		return leaf
	}
	if recType != nil {
		fields := slices.Clone(recType.Fields)
		if index, ok := recType.IndexOfField(path[0]); ok {
			fields[index].Type = c.putInto(nil, path[1:], leaf)
		} else {
			fields = append(fields, super.NewField(path[0], c.putInto(nil, path[1:], leaf)))
		}
		return c.record(fields)
	}
	return c.record([]super.Field{super.NewField(path[0], c.putInto(nil, path[1:], leaf))})
}

type path struct {
	loc   ast.Node
	elems []string
}

func (c *checker) lvalsToPaths(exprs []sem.Expr) []path {
	var paths []path
	for _, e := range exprs {
		this, ok := e.(*sem.ThisExpr)
		if !ok {
			return nil
		}
		paths = append(paths, path{loc: this.Node, elems: this.Chain.Path()})
	}
	return paths
}

func (c *checker) fuse(types []super.Type) super.Type {
	if len(types) == 0 {
		return c.unknown
	}
	if len(types) == 1 {
		return types[0]
	}
	fuser := c.newFuser()
	for _, typ := range types {
		fuser.fuse(typ)
	}
	return defuse(fuser.Type())
}

func (c *checker) boolean(loc ast.Node, typ super.Type) bool {
	ok := typeCheck(typ, func(typ super.Type) bool {
		return typ == super.TypeBool || typ == super.TypeNull
	})
	if !ok {
		c.error(loc, fmt.Errorf("boolean type required, encountered type %q", sup.FormatType(typ)))
	}
	return ok
}

func typeCheck(typ super.Type, check func(super.Type) bool) bool {
	if isUnknown(typ) {
		return true
	}
	if u, ok := super.TypeUnder(typ).(*super.TypeUnion); ok {
		for _, t := range u.Types {
			if typeCheck(t, check) {
				return true
			}
		}
		return false
	}
	if o, ok := super.TypeUnder(typ).(*super.TypeOption); ok {
		return typeCheck(o.Type, check)
	}
	return check(typ)
}

func (c *checker) integer(loc ast.Node, typ super.Type) bool {
	ok := typeCheck(typ, func(typ super.Type) bool {
		return super.IsInteger(typ.ID())
	})
	if !ok {
		c.error(loc, fmt.Errorf("integer type required, encountered %s", sup.FormatType(typ)))
	}
	return ok
}

func (c *checker) number(loc ast.Node, typ super.Type) bool {
	ok := typeCheck(typ, func(typ super.Type) bool {
		id := typ.ID()
		return super.IsNumber(id) || id == super.IDNull
	})
	if !ok {
		c.error(loc, fmt.Errorf("numeric type required, encountered %s", sup.FormatType(typ)))
	}
	return ok
}

// e ?. f => if e is none, then none otherwise e.f
// so typeof(e?.f) is option(typeof(e.f))
// but only if e is an option type, otherwise the type is error|typeof(e.f)
func (c *checker) deref(loc ast.Node, inType super.Type, field string, noneish bool) super.Type {
	switch typ := defuse(super.TypeUnder(inType)).(type) {
	case *super.TypeError:
		if isUnknown(typ) {
			return typ
		}
	case *super.TypeMap:
		t, _ := c.indexMap(loc, typ, super.TypeString)
		return t
	case *super.TypeRecord:
		which, ok := typ.IndexOfField(field)
		if !ok {
			if !hasUnknown(typ) {
				c.error(loc, newMissing(field))
			}
			return c.unknown
		}
		return typ.Fields[which].Type
	case *super.TypeUnion:
		if hasNone(typ) {
			// The value may be a bare none (e.g., it's from ok()), which
			// has no type to carry, so e?.f is a bare none when e is and
			// e.f doesn't handle none.
			if !noneish {
				c.error(loc, fmt.Errorf("%s: %w", sup.FormatType(typ), errNone))
				return c.unknown
			}
			rest, ok := c.withoutTypes(typ, isNoneType)
			if !ok {
				return super.TypeNone
			}
			return c.orNone(c.deref(loc, rest, field, noneish))
		}
		c.pushErrs()
		var types []super.Type
		for _, t := range typ.Types {
			types = append(types, c.deref(loc, t, field, noneish))
		}
		// XXX COOL IDEA: if we have a case on kind() then we can strip the relevant
		// types out of the union and, e.g., precisely type check records under
		// case type(x)="record" (...), or switch, this is like typecase but is
		// kindcase (which is the same is case kind(e) when the cases are const string)
		//
		errs := c.popErrs()
		_, missings := stripMissing(errs)
		recs := recTypes(typ.Types)
		if len(missings) == len(recs) && len(recs) > 0 {
			var in string
			if len(recs) == 1 {
				in = sup.FormatType(recs[0])
			} else {
				in = sup.FormatType(c.union(recs))
			}
			c.error(missings[0].loc, fmt.Errorf("no such field %s in %s", field, in))
		}
		return c.fuse(types)
	case *super.TypeOption:
		// e?.f is none when e is none and otherwise e.f, so its type is
		// option(typeof(e.f)), whereas e.f on an option doesn't handle none.
		// The none keeps the type of e.f so, e.g., {y:e?.f} has an optional
		// field y rather than y being dropped.
		if !noneish {
			c.error(loc, fmt.Errorf("%s: %w", sup.FormatType(inType), errNone))
			return c.unknown
		}
		return c.optionize(c.deref(loc, typ.Type, field, noneish))
	case *super.TypeOfNone:
		if !noneish {
			c.error(loc, fmt.Errorf("%s: %w", sup.FormatType(inType), errNone))
			return c.unknown
		}
		return typ
	case *super.TypeOfMissing:
		// A field of a missing value is missing too, so leave the missing
		// in the type for whatever uses it to catch.
		return typ
	}
	c.error(loc, fmt.Errorf("no such field %q: . applied to %s", field, sup.FormatType(inType)))
	return c.unknown

}

func recTypes(types []super.Type) []super.Type {
	var out []super.Type
	for _, typ := range types {
		if _, ok := super.TypeUnder(defuse(typ)).(*super.TypeRecord); ok {
			out = append(out, typ)
		}
	}
	return out
}

type missing struct {
	error
}

func newMissing(field string) error {
	return &missing{fmt.Errorf("no such field %q", field)}
}

func stripMissing(errs errlist) (errlist, errlist) {
	var missings, others errlist
	for _, errloc := range errs {
		if _, ok := errloc.err.(*missing); ok {
			missings = append(missings, errloc)
		} else {
			others = append(others, errloc)
		}
	}
	return others, missings
}

func (c *checker) logical(lloc, rloc ast.Node, lhs, rhs super.Type) {
	c.boolean(lloc, lhs)
	c.boolean(rloc, rhs)
}

func (c *checker) in(loc, lloc, rloc ast.Node, lhs, rhs super.Type) bool {
	switch typ := super.TypeUnder(rhs).(type) {
	case *super.TypeOfNull:
	case *super.TypeError:
		return isUnknown(typ)
	case *super.TypeArray:
		if !comparable(lhs, typ.Type) {
			c.error(lloc, errors.New("left-hand side of in operator be compatible with array type"))
			return false
		}
	case *super.TypeSet:
		if !comparable(lhs, typ.Type) {
			c.error(lloc, errors.New("left-hand side of in operator be compatible with set type"))
			return false
		}
	case *super.TypeRecord:
		var types []super.Type
		for _, field := range typ.Fields {
			types = append(types, field.Type)
		}
		if !comparable(lhs, c.fuse(types)) {
			c.error(lloc, errors.New("left-hand side of in operator not compatible with any fields of record on right-hand side"))
			return false
		}
	case *super.TypeMap:
		if !comparable(lhs, typ.ValType) && !comparable(lhs, typ.KeyType) {
			c.error(lloc, errors.New("left-hand side of in operator be compatible with map value or key type"))
			return false
		}
	case *super.TypeOfNet:
		c.error(rloc, errors.New("right-hand side of in operator cannot be type net; consider cidr_match()"))
		return false
	case *super.TypeUnion:
		// Push the error stack and if we find some valid deref,
		// we'll discard the errors.  Otherwise, we'll keep them.
		c.pushErrs()
		var valid bool
		for _, t := range typ.Types {
			if c.in(loc, lloc, rloc, lhs, t) {
				valid = true
			}
		}
		errs := c.popErrs()
		if !valid {
			c.keepErrs(errs)
		}
		return valid
	case *super.TypeOption:
		return c.in(loc, lloc, rloc, lhs, typ.Type)
	default:
		// If the RHS is not a container, see if they are compatible in terms of
		// equality comparison.  The in operator for SuperSQL is broader than SQL
		// and is true for equality of any value as well as equality containment
		// of the LHS in the RHS.
		if !comparable(lhs, rhs) {
			c.error(loc, fmt.Errorf("scalar type mismatch for 'in' operator where right-hand side is not container type: %s", sup.FormatType(typ)))
		}
		return false
	}
	return true
}

func (c *checker) equality(lhs, rhs super.Type) super.Type {
	comparable(lhs, rhs)
	return super.TypeBool
}

func (c *checker) comparison(lhs, rhs super.Type) super.Type {
	comparable(lhs, rhs)
	return super.TypeBool
}

func comparable(a, b super.Type) bool {
	if isUnknown(a) || isUnknown(b) {
		return true
	}
	if a, ok := super.TypeUnder(a).(*super.TypeOption); ok {
		return comparable(a.Type, b)
	}
	if b, ok := super.TypeUnder(b).(*super.TypeOption); ok {
		return comparable(a, b.Type)
	}
	if u, ok := super.TypeUnder(a).(*super.TypeUnion); ok {
		for _, t := range u.Types {
			if comparable(t, b) {
				return true
			}
		}
		return false
	}
	if u, ok := super.TypeUnder(b).(*super.TypeUnion); ok {
		for _, t := range u.Types {
			if comparable(a, t) {
				return true
			}
		}
		return false
	}
	aid := super.TypeUnder(a).ID()
	bid := super.TypeUnder(b).ID()
	if aid == bid || aid == super.IDNull || bid == super.IDNull {
		return true
	}
	if super.IsNumber(aid) {
		return super.IsNumber(bid)
	}
	switch super.TypeUnder(a).(type) {
	case *super.TypeRecord:
		_, ok := super.TypeUnder(b).(*super.TypeRecord)
		return ok
	case *super.TypeArray:
		if _, ok := super.TypeUnder(b).(*super.TypeArray); ok {
			return ok
		}
		_, ok := super.TypeUnder(b).(*super.TypeSet)
		return ok
	case *super.TypeSet:
		if _, ok := super.TypeUnder(b).(*super.TypeArray); ok {
			return ok
		}
		_, ok := super.TypeUnder(b).(*super.TypeSet)
		return ok
	case *super.TypeMap:
		_, ok := super.TypeUnder(b).(*super.TypeMap)
		return ok
	}
	return false
}

func (c *checker) arithmetic(lloc, rloc ast.Node, lhs, rhs super.Type) super.Type {
	if hasUnknown(lhs) || hasUnknown(rhs) {
		return c.unknown
	}
	c.number(lloc, lhs)
	c.number(rloc, rhs)
	return c.fuse([]super.Type{lhs, rhs})
}

func (c *checker) plus(loc ast.Node, lhs, rhs super.Type) super.Type {
	if hasUnknown(lhs) || hasUnknown(rhs) {
		return c.unknown
	}
	if hasNumber(lhs) && hasNumber(rhs) {
		return c.fuse([]super.Type{lhs, rhs})
	}
	c.error(loc, fmt.Errorf("type mismatch: %s + %s", sup.FormatType(lhs), sup.FormatType(rhs)))
	return c.unknown
}

func hasNumber(typ super.Type) bool {
	id := super.TypeUnder(typ).ID()
	if super.IsNumber(id) || id == super.IDNull {
		return true
	}
	if u, ok := super.TypeUnder(typ).(*super.TypeUnion); ok {
		if slices.ContainsFunc(u.Types, hasNumber) {
			return true
		}
	}
	if o, ok := super.TypeUnder(typ).(*super.TypeOption); ok {
		return hasNumber(o.Type)
	}
	return false
}

func hasString(typ super.Type) bool {
	switch typ := super.TypeUnder(typ).(type) {
	case *super.TypeError:
		return isUnknown(typ)
	case *super.TypeOfString, *super.TypeOfNull:
		return true
	case *super.TypeUnion:
		return slices.ContainsFunc(typ.Types, hasString)
	}
	return false
}

func isUnknown(typ super.Type) bool {
	if err, ok := super.TypeUnder(typ).(*super.TypeError); ok {
		if rec, ok := err.Type.(*super.TypeRecord); ok {
			return len(rec.Fields) == 0
		}
	}
	return false
}

func hasUnknown(typ super.Type) bool {
	if u, ok := super.TypeUnder(typ).(*super.TypeUnion); ok {
		if slices.ContainsFunc(u.Types, hasUnknown) {
			return true
		}
	}
	if o, ok := super.TypeUnder(typ).(*super.TypeOption); ok {
		return hasUnknown(o.Type)
	}
	return isUnknown(typ)
}

func hasMissing(typ super.Type) bool {
	if u, ok := super.TypeUnder(typ).(*super.TypeUnion); ok {
		if slices.ContainsFunc(u.Types, hasMissing) {
			return true
		}
	}
	return super.TypeUnder(typ) == super.TypeMissing
}

// mayBeMissing reports whether typ is a union with missing as one of its
// types, as for a field that's absent from some records.
func mayBeMissing(typ super.Type) bool {
	u, ok := super.TypeUnder(typ).(*super.TypeUnion)
	return ok && slices.ContainsFunc(u.Types, hasMissing)
}

// hasNone reports whether typ is none or a union with none as one of its
// types.
func hasNone(typ super.Type) bool {
	if u, ok := super.TypeUnder(typ).(*super.TypeUnion); ok {
		return slices.ContainsFunc(u.Types, hasNone)
	}
	return super.TypeUnder(typ) == super.TypeNone
}

func hasOption(typ super.Type) bool {
	if u, ok := super.TypeUnder(typ).(*super.TypeUnion); ok {
		if slices.ContainsFunc(u.Types, hasOption) {
			return true
		}
	}
	_, ok := super.TypeUnder(typ).(*super.TypeOption)
	return ok
}

func (c *checker) hasArray(typ super.Type) (super.Type, bool) {
	switch typ := super.TypeUnder(typ).(type) {
	case *super.TypeUnion:
		for _, t := range typ.Types {
			if elemType, ok := c.hasArray(t); ok {
				return elemType, true
			}
		}
	case *super.TypeArray:
		return typ.Type, true
	case *super.TypeOption:
		return c.hasArray(typ.Type)
	}
	if isUnknown(typ) {
		return c.unknown, true
	}
	return nil, false

}

func (c *checker) indexOf(cloc, iloc ast.Node, container, index super.Type) (super.Type, bool) {
	if hasUnknown(container) {
		return c.unknown, true
	}
	switch typ := super.TypeUnder(container).(type) {
	case *super.TypeArray:
		c.pushErrs()
		c.integer(iloc, index)
		if errs := c.popErrs(); len(errs) > 0 {
			c.keepErrs(errs)
			return typ.Type, false
		}
		return typ.Type, true
	case *super.TypeSet:
		c.pushErrs()
		c.integer(iloc, index)
		if errs := c.popErrs(); len(errs) > 0 {
			c.keepErrs(errs)
			return typ.Type, false
		}
		return typ.Type, true
	case *super.TypeRecord:
		ok := typeCheck(index, func(typ super.Type) bool {
			id := super.TypeUnder(typ).ID()
			return id == super.IDString || super.IsInteger(id) || id == super.IDNull
		})
		if !ok {
			c.error(iloc, errors.New("string or integer type required to index record"))
		}
		var types []super.Type
		for _, field := range typ.Fields {
			types = append(types, field.Type)
		}
		return c.fuse(types), true
	case *super.TypeMap:
		if !comparable(typ.KeyType, index) {
			c.error(iloc, errors.New("type mismatch indexing map"))
			return typ.ValType, false
		}
		return typ.ValType, true
	case *super.TypeUnion:
		c.pushErrs()
		var types []super.Type
		var valid bool
		for _, t := range typ.Types {
			typ, ok := c.indexOf(cloc, iloc, t, index)
			if ok {
				types = append(types, typ)
				valid = true
			}
		}
		errs := c.popErrs()
		if !valid {
			c.keepErrs(errs)
		}
		return c.fuse(types), valid
	case *super.TypeOption:
		return c.indexOf(cloc, iloc, typ.Type, index)
	default:
		c.error(cloc, fmt.Errorf("indexed entity is not indexable"))
		return c.unknown, false
	}
}

func (c *checker) indexMap(loc ast.Node, m *super.TypeMap, index super.Type) (super.Type, bool) {
	if isUnknown(index) {
		return c.unknown, true
	}
	if !c.coerceable(index, m.KeyType) {
		c.error(loc, errors.New("type mismatch between map key and index"))
		return c.unknown, false
	}
	return m.ValType, true
}

func (c *checker) sliceable(loc ast.Node, typ super.Type) {
	if hasUnknown(typ) {
		return
	}
	switch super.TypeUnder(typ).(type) {
	case *super.TypeArray, *super.TypeSet, *super.TypeRecord, *super.TypeOfString, *super.TypeOfBytes:
	default:
		c.error(loc, fmt.Errorf("sliced entity is not sliceable"))
	}
}

func (c *checker) coerceable(from, to super.Type) bool {
	if isUnknown(from) || isUnknown(to) {
		return true
	}
	if u, ok := super.TypeUnder(from).(*super.TypeUnion); ok {
		for _, t := range u.Types {
			if c.coerceable(t, to) {
				return true
			}
		}
		return false
	}
	if u, ok := super.TypeUnder(to).(*super.TypeUnion); ok {
		for _, t := range u.Types {
			if c.coerceable(from, t) {
				return true
			}
		}
		return false
	}
	fromID := super.TypeUnder(from).ID()
	toID := super.TypeUnder(to).ID()
	if fromID == toID || fromID == super.IDNull || toID == super.IDNull {
		return true
	}
	if super.IsNumber(toID) {
		return super.IsNumber(fromID)
	}
	switch super.TypeUnder(to).(type) {
	case *super.TypeRecord:
		_, ok := super.TypeUnder(from).(*super.TypeRecord)
		return ok
	case *super.TypeArray:
		if _, ok := super.TypeUnder(from).(*super.TypeArray); ok {
			return ok
		}
		_, ok := super.TypeUnder(from).(*super.TypeSet)
		return ok
	case *super.TypeSet:
		if _, ok := super.TypeUnder(from).(*super.TypeArray); ok {
			return ok
		}
		_, ok := super.TypeUnder(from).(*super.TypeSet)
		return ok
	case *super.TypeMap:
		_, ok := super.TypeUnder(from).(*super.TypeMap)
		return ok
	}
	return false
}

func (c *checker) pushErrs() {
	c.estack = append(c.estack, nil)
}

func (c *checker) popErrs() errlist {
	n := len(c.estack) - 1
	errs := c.estack[n]
	c.estack = c.estack[:n]
	return errs
}

func (c *checker) keepErrs(errs errlist) {
	n := len(c.estack) - 1
	c.estack[n] = append(c.estack[n], errs...)
}

func (c *checker) error(loc ast.Node, err error) {
	c.estack[len(c.estack)-1].error(loc, err)
}

func (c *checker) newFuser() *fuser {
	//XXX "complete" option will be true when we have dual support for fusion
	// and static types
	return &fuser{super.NewFuser(c.t.sctx, false), c.unknown}
}

type fuser struct {
	fuser   *super.Fuser
	unknown super.Type
}

func (f *fuser) fuse(typ super.Type) {
	f.fuser.Fuse(typ)
}

func (f *fuser) Type() super.Type {
	typ := f.fuser.Type()
	if typ == nil {
		return f.unknown
	}
	return typ
}

func (c *checker) aggFunc(name string, arg ast.Node, argType super.Type, filter ast.Node, filterType super.Type) super.Type {
	// An aggregate function skips none values but a field that may be
	// absent must be converted to none with ok(), e.g., count(y.ok()).
	if arg != nil {
		c.notMissing(arg, argType)
	}
	if filter == nil || c.notMissing(filter, filterType) {
		c.boolean(filter, filterType)
	}
	//XXX TBD... need to type check args and get return value
	return c.unknown
}

// groupKey checks the type of a grouping expression.  A missing value
// can't be a group key so a field that may be absent must be converted
// to none with ok(), e.g., aggregate count() by y.ok(), in which case
// the values where the field is absent form the none group.
func (c *checker) groupKey(loc ast.Node, typ super.Type) {
	c.notMissing(loc, typ)
}

// notMissing reports an error at loc if typ may be missing.
func (c *checker) notMissing(loc ast.Node, typ super.Type) bool {
	typ = defuse(typ)
	if typ == nil || hasUnknown(typ) || !hasMissing(typ) {
		return true
	}
	c.error(loc, errMissing)
	return false
}
