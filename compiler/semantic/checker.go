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
	case *sem.HTTPScan,
		*sem.PoolScan,
		*sem.RobotScan,
		*sem.DBMetaScan,
		*sem.PoolMetaScan,
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
		fields := c.assignments(typ, op.Args)
		return c.putPaths(typ, fields)
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
	typ := c.t.sctx.MustLookupTypeRecord([]super.Field{
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
		return c.t.sctx.MustLookupTypeRecord(fields), true
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
		c.expr(typ, e.Expr)
		c.expr(typ, e.Filter)
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
		return c.t.sctx.LookupTypeArray(c.arrayElems(typ, e.Elems))
	case *sem.BadExpr:
		return c.unknown
	case *sem.BinaryExpr:
		lhs := c.expr(typ, e.LHS)
		rhs := c.expr(typ, e.RHS)
		return c.binary(e.Op, e, e.LHS, e.RHS, lhs, rhs)
	case *sem.CallExpr:
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
		typ, _ := c.deref(e.Node, c.expr(typ, e.LHS), e.RHS)
		return typ
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

func defuse(typ super.Type) super.Type {
	if typ, ok := typ.(*super.TypeFusion); ok {
		return typ.Type
	}
	return typ
}

func (c *checker) binary(op string, loc, lloc, rloc ast.Node, lhs, rhs super.Type) super.Type {
	lhs = defuse(lhs)
	rhs = defuse(rhs)
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
		return c.fuse([]super.Type{lhs, rhs})
	default:
		panic(op)
	}
}

func (c *checker) this(loc ast.Node, this *sem.ThisExpr, typ super.Type) super.Type {
	for _, comp := range this.Chain {
		//XXX type check should use comp.Nullish too
		typ, _ = c.deref(loc, typ, comp.ID)
	}
	return typ
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
			fuser.fuse(c.expr(typ, elem.Expr))
		case *sem.ExprElem:
			fuser.fuse(c.expr(typ, elem.Expr))
		default:
			panic(elem)
		}
	}
	return defuse(fuser.Type())
}

func (c *checker) recordElems(typ super.Type, elems []sem.RecordElem) super.Type {
	fuser := c.newFuser()
	for _, elem := range elems {
		switch elem := elem.(type) {
		case *sem.SpreadElem:
			elemType := c.expr(typ, elem.Expr)
			if hasUnknown(elemType) {
				// If we're spreading an unknown type into this record, we don't
				// know the result at all.  Return unknown for the whole thing.
				return c.unknown
			}
			fuser.fuse(c.expr(typ, elem.Expr))
		case *sem.FieldElem:
			column := super.NewField(elem.Name, c.expr(typ, elem.Value))
			fuser.fuse(c.t.sctx.MustLookupTypeRecord([]super.Field{column}))
		default:
			panic(elem)
		}
	}
	return defuse(fuser.Type())
}

func (c *checker) fuseRecordElems(elems []sem.RecordElem, types []super.Type) super.Type {
	fuser := c.newFuser()
	for k, elem := range elems {
		typ := types[k]
		switch elem := elem.(type) {
		case *sem.SpreadElem:
			if hasUnknown(typ) {
				// If we're spreading an unknown type into this record, we don't
				// know the result at all.  Return unknown for the whole thing.
				return c.unknown
			}
			fuser.fuse(typ)
		case *sem.FieldElem:
			fuser.fuse(c.t.sctx.MustLookupTypeRecord([]super.Field{super.NewField(elem.Name, typ)}))
		default:
			panic(elem)
		}
	}
	return defuse(fuser.Type())
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
	fuser := c.newFuser()
	for _, path := range paths {
		fuser.fuse(c.pathToRec(path.typ, path.elems))
	}
	return defuse(fuser.Type())
}

func (c *checker) pathToRec(typ super.Type, elems []string) super.Type {
	for _, elem := range slices.Backward(elems) {
		typ = c.t.sctx.MustLookupTypeRecord([]super.Field{{Name: elem, Type: typ}})
	}
	return typ
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
	types[pick] = c.t.sctx.MustLookupTypeRecord(fields)
	if len(types) > 1 {
		return c.t.sctx.MustLookupTypeUnion(super.Flatten(types))
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

func (c *checker) putPaths(typ super.Type, puts []pathType) super.Type {
	// Fuse each path as a single-record path into the input type.
	fuser := c.newFuser()
	fuser.fuse(typ)
	for _, put := range puts {
		fuser.fuse(c.pathToRec(put.typ, put.elems))
	}
	return fuser.Type()
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

func (c *checker) deref(loc ast.Node, typ super.Type, field string) (super.Type, bool) {
	switch typ := defuse(super.TypeUnder(typ)).(type) {
	case *super.TypeError:
		if isUnknown(typ) {
			return typ, true
		}
	case *super.TypeMap:
		return c.indexMap(loc, typ, super.TypeString)
	case *super.TypeRecord:
		which, ok := typ.IndexOfField(field)
		if !ok {
			if !hasUnknown(typ) {
				c.error(loc, fmt.Errorf("no such field %q", field))
			}
			return c.unknown, false
		}
		return typ.Fields[which].Type, true
	case *super.TypeUnion:
		// Push the error stack and if we find some valid deref,
		// we'll discard the errors.  Otherwise, we'll keep them.
		c.pushErrs()
		var types []super.Type
		var valid bool
		for _, t := range typ.Types {
			typ, ok := c.deref(loc, t, field)
			if ok {
				types = append(types, typ)
				valid = true
			}
		}
		errs := c.popErrs()
		if !valid {
			c.keepErrs(errs[:1])
		}
		return c.fuse(types), valid
	case *super.TypeOption:
		return c.deref(loc, typ.Type, field)
	}
	c.error(loc, fmt.Errorf("no such field %q", field))
	return c.unknown, false
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
	c.boolean(filter, filterType)
	//XXX TBD... need to type check args and get return value
	return c.unknown
}
