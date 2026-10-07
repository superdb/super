package semantic

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/araddon/dateparse"
	"github.com/shellyln/go-sql-like-expr/likeexpr"
	"github.com/superdb/super"
	"github.com/superdb/super/compiler/ast"
	"github.com/superdb/super/compiler/semantic/sem"
	"github.com/superdb/super/compiler/sfmt"
	"github.com/superdb/super/pkg/field"
	"github.com/superdb/super/pkg/nano"
	"github.com/superdb/super/pkg/reglob"
	"github.com/superdb/super/runtime/expr/agg"
	"github.com/superdb/super/runtime/expr/function"
	"github.com/superdb/super/runtime/sam/expr"
	"github.com/superdb/super/sup"
)

func (t *translator) expr(e ast.Expr, inType super.Type) (sem.Expr, super.Type) {
	switch e := e.(type) {
	case *ast.AggFuncExpr:
		expr, _ := t.exprNullable(e.Expr, inType)
		nameLower := strings.ToLower(e.Name)
		if expr == nil && nameLower != "count" {
			t.error(e, fmt.Errorf("aggregator '%s' requires argument", e.Name))
			return badExpr, badType
		}
		return t.aggFunc(e, nameLower, e.Expr, e.Filter, e.Distinct, inType)
	case *ast.ArrayExpr:
		elems, elemsType := t.arrayElems(e.Elems, inType)
		if subquery := t.arraySubquery(elems); subquery != nil {
			return subquery, t.checker.unknown //XXX TBD
		}
		return &sem.ArrayExpr{
			Node:  e,
			Elems: elems,
		}, t.sctx.LookupTypeArray(elemsType)
	case *ast.BinaryExpr:
		return t.binaryExpr(e, inType)
	case *ast.BetweenExpr:
		val, valType := t.expr(e.Expr, inType)
		lower, lowerType := t.expr(e.Lower, inType)
		upper, upperType := t.expr(e.Upper, inType)
		// Copy val so an optimizer change to one instance doesn't affect the other.
		expr := &sem.BinaryExpr{
			Node: e,
			Op:   "and",
			LHS: &sem.BinaryExpr{
				Node: e.Lower,
				Op:   ">=",
				LHS:  val,
				RHS:  lower,
			},
			RHS: &sem.BinaryExpr{
				Node: e.Upper,
				Op:   "<=",
				LHS:  sem.CopyExpr(val),
				RHS:  upper,
			},
		}
		t.checker.comparison(lowerType, valType)
		t.checker.comparison(valType, upperType)
		if e.Not {
			return sem.NewUnaryExpr(e, "!", expr), super.TypeBool
		}
		return expr, super.TypeBool
	case *ast.CaseExpr:
		return t.semCaseExpr(e, inType)
	case *ast.CondExpr:
		var errlocs [][]errloc
		cond, condType := t.expr(e.Cond, inType)
		t.checker.boolean(e.Cond, condType)
		t.checker.pushErrs()
		thenExpr, thenType := t.expr(e.Then, inType)
		if errs := t.checker.popErrs(); len(errs) != 0 {
			errlocs = append(errlocs, errs)
		}
		var elseExpr sem.Expr
		var elseType super.Type
		if e.Else != nil {
			t.checker.pushErrs()
			elseExpr, elseType = t.expr(e.Else, inType)
			if errs := t.checker.popErrs(); len(errs) != 0 {
				errlocs = append(errlocs, errs)
			}
		} else {
			elseExpr = sem.NewStringError(e, "missing")
			elseType = t.checker.unknown
		}
		if len(errlocs) == 2 {
			for _, errs := range errlocs {
				t.checker.keepErrs(errs)
			}
		}
		return &sem.CondExpr{
			Node: e,
			Cond: cond,
			Then: thenExpr,
			Else: elseExpr,
		}, t.checker.fuse([]super.Type{thenType, elseType})
	case *ast.CallExpr:
		return t.semCall(e, inType)
	case *ast.CastExpr:
		expr, _ := t.expr(e.Expr, inType)
		if _, ok := e.Type.(*ast.DateTypeHack); ok {
			// cast to time then bucket by 1d as a workaround for not currently
			// supporting a "date" type.
			cast := sem.NewCast(e.Expr, expr, super.TypeTime, t.defs)
			return sem.NewCall(e, "bucket", []sem.Expr{cast, &sem.PrimitiveExpr{Node: e, Value: "1d"}}), super.TypeTime
		}
		typeVal, _ := t.semType(e.Type)
		// In a future PR, we will add support to see if the expr type is
		// cast-able and return the appropriate cast type.  For now, we just
		// return unknown and we don't do futher type checking on this.
		return sem.NewCall(e, "cast", []sem.Expr{expr, typeVal}), t.checker.unknown
	case *ast.ExtractExpr:
		return t.semExtractExpr(e, e.Part, e.Expr, inType)
	case *ast.DoubleQuoteExpr:
		return t.doubleQuoteExpr(e, inType)
	case *ast.ExistsExpr:
		return t.existsExpr(e, inType)
	case *ast.FStringExpr:
		return t.fstringExpr(e, inType)
	case *ast.FuncNameExpr:
		// We get here for &refs that are in a call expression. e.g.,
		// an arg to another function.  It could be a built-in (as in &upper),
		// or a user function (as in fn foo():... &foo)...
		id := e.Name
		if boundID, _ := t.scope.lookupFuncDeclOrParam(id); boundID != "" {
			id = boundID
		}
		return &sem.FuncRef{
			Node: e,
			ID:   id,
		}, t.checker.unknown
	case *ast.GlobExpr:
		return &sem.RegexpSearchExpr{
			Node:    e,
			Pattern: reglob.Reglob(e.Pattern),
			Expr:    sem.NewThis(e, nil),
		}, super.TypeBool
	case *ast.IDExpr:
		return t.idExpr(e, false, inType)
	case *ast.IndexExpr:
		container, containerType := t.expr(e.Expr, inType)
		index, indexType := t.expr(e.Index, inType)
		// If expr is a path and index is a string, then just extend the path.
		if path := t.isIndexOfThis(container, index); path != nil {
			return path, t.checker.this(e, path, inType)
		}
		outType, _ := t.checker.indexOf(e.Expr, e.Index, containerType, indexType)
		return &sem.IndexExpr{
			Node:  e,
			Expr:  container,
			Index: index,
			Base1: t.scope.indexBase() == 1,
		}, outType
	case *ast.IsNullExpr:
		expr, _ := t.expr(e.Expr, inType)
		var out sem.Expr = &sem.IsNullExpr{Node: e, Expr: expr}
		if e.Not {
			out = sem.NewUnaryExpr(e, "!", out)
		}
		return out, super.TypeBool
	case *ast.LambdaExpr:
		funcDecl := t.resolver.newFuncDecl("lambda", e, t.scope)
		return &sem.FuncRef{
			Node: e,
			ID:   funcDecl.id,
		}, t.checker.unknown
	case *ast.MapExpr:
		var entries []sem.Entry
		var keyTypes []super.Type
		var valTypes []super.Type
		for _, entry := range e.Entries {
			key, keyType := t.expr(entry.Key, inType)
			val, valType := t.expr(entry.Value, inType)
			entries = append(entries, sem.Entry{Key: key, Value: val})
			keyTypes = append(keyTypes, keyType)
			valTypes = append(valTypes, valType)
		}
		return &sem.MapExpr{
			Node:    e,
			Entries: entries,
		}, t.sctx.LookupTypeMap(t.checker.fuse(keyTypes), t.checker.fuse(valTypes))
	case *ast.Primitive:
		val, err := sup.ParsePrimitive(e.Type, e.Text)
		if err != nil {
			t.error(e, err)
			return badExpr, t.checker.unknown
		}
		return &sem.PrimitiveExpr{
			Node:  e,
			Value: sup.FormatValue(val),
		}, val.Type()
	case *ast.SubqueryExpr:
		return t.subqueryExpr(e, e.Array, e.Body, inType)
	case *ast.RecordExpr:
		fields := map[string]struct{}{}
		var out []sem.RecordElem
		var types []super.Type
		for _, elem := range e.Elems {
			switch elem := elem.(type) {
			case *ast.FieldElem:
				name := elem.Name.Text
				if _, ok := fields[name]; ok {
					t.error(elem, fmt.Errorf("record expression: %w", &super.DuplicateFieldError{Name: name}))
					continue
				}
				fields[name] = struct{}{}
				e, typ := t.expr(elem.Value, inType)
				if elem.Opt {
					e = &sem.CallExpr{Node: elem.Value, Tag: "some", Args: []sem.Expr{e}}
				}
				out = append(out, &sem.FieldElem{
					Node:  elem,
					Name:  elem.Name.Text,
					Value: e,
				})
				types = append(types, typ)
			case *ast.SpreadElem:
				e, typ := t.expr(elem.Expr, inType)
				out = append(out, &sem.SpreadElem{
					Node: elem,
					Expr: e,
				})
				types = append(types, typ)
			case *ast.ExprElem:
				e, typ := t.expr(elem.Expr, inType)
				if elem.Opt {
					e = &sem.CallExpr{Node: elem.Expr, Tag: "some", Args: []sem.Expr{e}}
				}
				name := deriveNameFromExpr(elem.Expr)
				if _, ok := fields[name]; ok {
					t.error(elem, fmt.Errorf("record expression: %w", &super.DuplicateFieldError{Name: name}))
					continue
				}
				fields[name] = struct{}{}
				out = append(out, &sem.FieldElem{
					Name:  name,
					Value: e,
				})
				types = append(types, typ)
			default:
				panic(e)
			}
		}
		return &sem.RecordExpr{
			Node:  e,
			Elems: out,
		}, t.checker.fuseRecordElems(out, types)
	case *ast.RegexpExpr:
		return &sem.RegexpSearchExpr{
			Node:    e,
			Pattern: e.Pattern,
			Expr:    sem.NewThis(e, nil),
		}, super.TypeBool
	case *ast.SetExpr:
		elems, elemsType := t.arrayElems(e.Elems, inType)
		return &sem.SetExpr{
			Node:  e,
			Elems: elems,
		}, t.sctx.LookupTypeSet(elemsType)
	case *ast.SliceExpr:
		container, containerType := t.expr(e.Expr, inType)
		from, fromType := t.exprNullable(e.From, inType)
		to, toType := t.exprNullable(e.To, inType)
		if e.From != nil {
			t.checker.integer(e.From, fromType)
		}
		if e.To != nil {
			t.checker.integer(e.To, toType)
		}
		t.checker.sliceable(e.Expr, containerType)
		return &sem.SliceExpr{
			Node:  e,
			Expr:  container,
			From:  from,
			To:    to,
			Base1: t.scope.indexBase() == 1,
		}, containerType
	case *ast.SQLTimeExpr:
		if e.Value.Type != "string" {
			t.error(e.Value, errors.New("value must be a string literal"))
			return badExpr, t.checker.unknown
		}
		tm, err := dateparse.ParseAny(e.Value.Text)
		if err != nil {
			t.error(e.Value, err)
			return badExpr, t.checker.unknown
		}
		ts := nano.TimeToTs(tm)
		if e.Type == "date" {
			ts = ts.Trunc(nano.Day)
		}
		return sem.NewLiteral(e, super.NewTime(ts), t.defs), super.TypeTime
	case *ast.SearchTermExpr:
		var val string
		switch term := e.Value.(type) {
		case *ast.Primitive:
			v, err := sup.ParsePrimitive(term.Type, term.Text)
			if err != nil {
				t.error(e, err)
				return badExpr, t.checker.unknown
			}
			val = sup.FormatValue(v)
		case *ast.DoubleQuoteExpr:
			v, err := sup.ParsePrimitive("string", term.Text)
			if err != nil {
				t.error(e, err)
				return badExpr, t.checker.unknown
			}
			val = sup.FormatValue(v)
		case *ast.TypeValue:
			e, typ := t.semType(term.Value)
			typeExpr, ok := e.(*sem.TypeExpr)
			if !ok {
				return e, typ
			}
			typ, err := t.defs.LookupType(typeExpr.ID)
			if err != nil {
				panic(err)
			}
			val = "<" + sup.FormatType(typ) + ">"
		default:
			panic(fmt.Errorf("unexpected term value: %s (%T)", e.Kind, e))
		}
		return &sem.SearchTermExpr{
			Node:  e,
			Text:  e.Text,
			Value: val,
			Expr:  sem.NewThis(e, nil),
		}, super.TypeBool
	case *ast.SubstringExpr:
		expr, exprType := t.expr(e.Expr, inType)
		if e.From == nil && e.For == nil {
			t.error(e, errors.New("FROM or FOR must be set"))
			return badExpr, t.checker.unknown
		}
		if !hasString(exprType) {
			t.error(e.Expr, errors.New("expected type string"))
		}
		//XXX TBD do what this comment says:
		// XXX type checker should remove this check when it finds it redundant
		under := sem.NewCall(e, "under", []sem.Expr{expr})
		is := sem.NewCall(e, "is", []sem.Expr{under, &sem.PrimitiveExpr{Node: e.Expr, Value: "<string>"}})
		indexBase := t.scope.indexBase()
		from, fromType := t.exprNullable(e.From, inType)
		if e.From != nil {
			t.checker.integer(e.From, fromType)
		}
		slice := &sem.SliceExpr{
			Node:  e,
			Expr:  expr,
			From:  from,
			Base1: indexBase == 1,
		}
		if e.For != nil {
			to, toType := t.expr(e.For, inType)
			t.checker.integer(e.From, toType)
			if slice.From != nil {
				slice.To = sem.NewBinaryExpr(e, "+", slice.From, to)
			} else {
				slice.To = sem.NewBinaryExpr(e, "+", to, sem.NewLiteral(e, super.NewInt64(int64(indexBase)), t.defs))
			}
		}
		serr := sem.NewStructuredError(e, "SUBSTRING: string value required", expr)
		return &sem.CondExpr{
			Node: e,
			Cond: is,
			Then: slice,
			Else: serr,
		}, super.TypeString
	case *ast.TupleExpr:
		elems := make([]sem.RecordElem, 0, len(e.Elems))
		fields := make([]super.Field, 0, len(e.Elems))
		for colno, elem := range e.Elems {
			e, typ := t.expr(elem, inType)
			name := fmt.Sprintf("c%d", colno)
			elems = append(elems, &sem.FieldElem{
				Node:  elem,
				Name:  name,
				Value: e,
			})
			fields = append(fields, super.NewField(name, typ))
		}
		return &sem.RecordExpr{
			Node:  e,
			Elems: elems,
		}, t.sctx.MustLookupTypeRecord(fields)
	case *ast.TypeValue:
		return t.semType(e.Value)
	case *ast.UnaryExpr:
		operand, typ := t.expr(e.Operand, inType)
		if e.Op == "+" {
			return operand, typ
		}
		return sem.NewUnaryExpr(e, e.Op, operand), typ
	case nil:
		panic("semantic analysis: illegal null value encountered in AST")
	}
	panic(e)
}

func (t *translator) dot(e *ast.BinaryExpr, nullish bool, inType super.Type) (sem.Expr, super.Type) {
	id, ok := e.RHS.(*ast.IDExpr)
	if !ok {
		t.error(e, errors.New("RHS of dot operator is not an identifier"))
		return badExpr, badType
	}
	if lhs, ok := e.LHS.(*ast.IDExpr); ok {
		// Check for plain-ID this (not double quoted) and resolve accordingly.
		if lhs.Name == "this" && t.scope.sql != nil {
			this, typ := t.scope.resolveThis(t, lhs, inType)
			return t.deref(e, this, id, nullish, typ)
		}
		return t.dottedBaseCase(e, lhs, id, nullish, inType)
	}
	// Handle SQL double-quoted IDs without checking for this.
	if lhs, ok := e.LHS.(*ast.DoubleQuoteExpr); ok && t.scope.sql != nil {
		lhsID := &ast.IDExpr{ID: ast.ID{Name: lhs.Text, Loc: lhs.Loc}}
		return t.dottedBaseCase(e, lhsID, id, nullish, inType)
	}
	lhs, typ := t.expr(e.LHS, inType)
	return t.deref(e, lhs, id, nullish, typ)
}

func (t *translator) dottedBaseCase(loc ast.Node, lhs *ast.IDExpr, rhs *ast.IDExpr, nullish bool, inType super.Type) (sem.Expr, super.Type) {
	if e, typ := t.idExpand(lhs, false, inType); e != nil {
		return t.deref(loc, e, rhs, nullish, typ)
	}
	if t.scope.sql != nil {
		return t.scope.resolve(t, loc, []string{lhs.Name, rhs.Name}, inType)
	}
	id, typ := t.idExpr(lhs, false, inType)
	return t.deref(loc, id, rhs, nullish, typ)
}

func (t *translator) deref(loc ast.Node, lhs sem.Expr, id *ast.IDExpr, noneish bool, inType super.Type) (sem.Expr, super.Type) {
	typ, _ := t.checker.deref(id, inType, id.Name)
	if lhs, ok := lhs.(*sem.ThisExpr); ok {
		lhs.Chain = lhs.Chain.Append(id.Name, noneish)
		lhs.Node = loc
		return lhs, typ
	}
	return &sem.DotExpr{
		Node:    loc,
		LHS:     lhs,
		RHS:     id.Name,
		Noneish: noneish,
	}, typ
}

func (t *translator) idExpr(id *ast.IDExpr, lval bool, inType super.Type) (sem.Expr, super.Type) {
	if e, typ := t.idExpand(id, lval, inType); e != nil {
		return e, typ
	}
	if t.scope.sql != nil {
		if id.Name == "this" {
			return t.scope.resolveThis(t, id, inType)
		}
		return t.scope.resolve(t, id, []string{id.Name}, inType)
	}
	var chain field.Chain
	if id.Name != "this" {
		chain = field.NewChain(id.Name)
	}
	this := sem.NewThis(id, chain)
	return this, t.checker.this(id, this, inType)
}

// idExpand checks if an identifier binds to something in the symbol table.
func (t *translator) idExpand(id *ast.IDExpr, lval bool, inType super.Type) (sem.Expr, super.Type) {
	if subquery, typ := t.maybeSubquery(id, id.Name, inType); subquery != nil {
		return subquery, typ
	}
	// Check if there's a user function in scope with this name and report
	// an error to avoid a rake when such a function is mistakenly passed
	// without "&" and otherwise turns into a field reference.
	if entry := t.scope.lookupEntry(id.Name); entry != nil {
		if _, ok := entry.ref.(*funcDef); ok && !lval {
			t.error(id, fmt.Errorf("function %q referenced but not called (consider &%s to create a function value)", id.Name, id.Name))
			return badExpr, t.checker.unknown
		}
	}
	// Check for user op params which are in the symbol table as sem.Expr.
	return t.scope.lookupExpr(t, id, id.Name, inType)
}

func (t *translator) doubleQuoteExpr(d *ast.DoubleQuoteExpr, inType super.Type) (sem.Expr, super.Type) {
	// Check if there's a SQL scope and treat a double-quoted string
	// as an identifier.  XXX we'll need to do something a bit more
	// sophisticated to handle pipes inside SQL subqueries.
	if t.scope.sql != nil {
		if d.Text == "this" {
			// Resolve directly here as column so it's not interpreted as this.
			return t.scope.resolve(t, d, []string{"this"}, inType)
		}
		return t.expr(&ast.IDExpr{Kind: "IDExpr", ID: ast.ID{Name: d.Text, Loc: d.Loc}}, inType)
	}
	return t.expr(&ast.Primitive{
		Kind: "Primitive",
		Type: "string",
		Text: d.Text,
		Loc:  d.Loc,
	}, inType)
}

func (t *translator) existsExpr(e *ast.ExistsExpr, inType super.Type) (sem.Expr, super.Type) {
	q, _ := t.subqueryExpr(e, true, e.Body, inType)
	return sem.NewBinaryExpr(e, ">",
		sem.NewCall(e, "len", []sem.Expr{q}),
		sem.NewLiteral(e, super.NewInt64(0), t.defs)), super.TypeBool
}

func (t *translator) regexp(b *ast.BinaryExpr, inType super.Type) (sem.Expr, super.Type) {
	if b.Op != "~" {
		return nil, nil
	}
	rhs, _ := t.expr(b.RHS, inType)
	pattern, ok := t.mustEvalString(rhs)
	if !ok {
		t.error(b.RHS, errors.New(`right-hand side of ~ expression must be a string literal`))
		return badExpr, t.checker.unknown
	}
	if _, err := expr.CompileRegexp(pattern); err != nil {
		t.error(b.RHS, err)
		return badExpr, t.checker.unknown
	}
	lhs, lhsType := t.expr(b.LHS, inType)
	if !hasString(lhsType) {
		t.error(b.LHS, errors.New(`left-hand side of ~ expression must be string type`))
	}
	return &sem.RegexpMatchExpr{
		Node:    b,
		Pattern: pattern,
		Expr:    lhs,
	}, super.TypeBool
}

func (t *translator) binaryExpr(e *ast.BinaryExpr, inType super.Type) (sem.Expr, super.Type) {
	if e, typ := t.regexp(e, inType); e != nil {
		return e, typ
	}
	op := strings.ToLower(e.Op)
	if op == "." || op == "?." {
		return t.dot(e, op == "?.", inType)
	}
	lhs, lhsType := t.expr(e.LHS, inType)
	rhs, rhsType := t.expr(e.RHS, inType)
	if op == "like" || op == "not like" {
		s, ok := t.mustEvalString(rhs)
		if !ok {
			t.error(e.RHS, errors.New("non-constant pattern for LIKE not supported"))
			return badExpr, t.checker.unknown
		}
		pattern := likeexpr.ToRegexp(s, '\\', false)
		expr := &sem.RegexpSearchExpr{
			Node:    e,
			Pattern: "(?s)" + pattern,
			Expr:    lhs,
		}
		if op == "not like" {
			return &sem.UnaryExpr{
				Node:    e,
				Op:      "!",
				Operand: expr,
			}, super.TypeBool
		}
		return expr, super.TypeBool
	}
	if op == "in" || op == "not in" {
		if q, ok := rhs.(*sem.SubqueryExpr); ok {
			q.Array = true
		}
	}
	switch op {
	case "=":
		op = "=="
	case "<>":
		op = "!="
	case "||":
		t.stringy(e.LHS, lhsType)
		t.stringy(e.RHS, rhsType)
		return &sem.CallExpr{
			Node: e,
			Tag:  "concat",
			Args: []sem.Expr{lhs, rhs},
		}, super.TypeString
	case "not in":
		t.checker.in(e, e.LHS, e.RHS, lhsType, rhsType)
		return sem.NewUnaryExpr(e, "!", sem.NewBinaryExpr(e, "in", lhs, rhs)), super.TypeBool
	case "::":
		return &sem.CallExpr{
			Node: e,
			Tag:  "cast",
			Args: []sem.Expr{lhs, rhs},
		}, t.checker.unknown //XXX need cast logic in checker
	}
	return &sem.BinaryExpr{
		Node: e,
		Op:   op,
		LHS:  lhs,
		RHS:  rhs,
	}, t.checker.binary(e.Op, e, e.LHS, e.RHS, lhsType, rhsType)
}

func (t *translator) stringy(loc ast.Node, typ super.Type) {
	if !hasString(typ) {
		t.error(loc, errors.New("expected type string"))
	}
}

func (t *translator) isIndexOfThis(lhs, rhs sem.Expr) *sem.ThisExpr {
	if this, ok := lhs.(*sem.ThisExpr); ok {
		if s, ok := t.maybeEvalString(rhs); ok {
			this.Chain = this.Chain.Append(s, false)
			return this
		}
	}
	return nil
}

func (t *translator) exprNullable(e ast.Expr, inType super.Type) (sem.Expr, super.Type) {
	if e == nil {
		return nil, super.TypeNull
	}
	return t.expr(e, inType)
}

func (t *translator) semCaseExpr(c *ast.CaseExpr, inType super.Type) (sem.Expr, super.Type) {
	e, exprType := t.exprNullable(c.Expr, inType)
	var out sem.Expr
	var elseType super.Type
	if c.Else != nil {
		out, elseType = t.expr(c.Else, inType)
	} else if t.scope.sql != nil {
		out = &sem.PrimitiveExpr{Node: c, Value: "null"}
		elseType = super.TypeNull
	} else if e != nil {
		out = sem.NewStructuredError(c, "case: no clause matched and no else provided", e)
		elseType = t.checker.unknown
	} else {
		out = sem.NewStringError(c, "case: no clause matched and no else provided")
		elseType = t.checker.unknown
	}
	types := []super.Type{elseType}
	var thenErrs [][]errloc
	for _, when := range slices.Backward(c.Whens) {
		cond, condType := t.expr(when.Cond, inType)
		if e != nil {
			cond = sem.NewBinaryExpr(c, "==", e, cond)
			t.checker.comparison(exprType, condType)
		} else {
			t.checker.boolean(when.Cond, condType)
		}
		t.checker.pushErrs()
		then, thenType := t.expr(when.Then, inType)
		out = &sem.CondExpr{
			Node: c,
			Cond: cond,
			Then: then,
			Else: out,
		}
		if errs := t.checker.popErrs(); len(errs) != 0 {
			thenErrs = append(thenErrs, errs)
		}
		types = append(types, thenType)
	}
	if len(thenErrs) == len(c.Whens) {
		// All the paths have errors so report them.
		// If at least one path is ok, then we deem the overall expression valid
		// an do not report any errors.
		for _, errs := range thenErrs {
			t.checker.keepErrs(errs)
		}
	}
	return out, t.checker.fuse(types)
}

func (t *translator) semCall(call *ast.CallExpr, inType super.Type) (sem.Expr, super.Type) {
	if e, typ := t.maybeConvertAgg(call, inType); e != nil {
		return e, typ
	}
	args, argTypes := t.exprs(call.Args, inType)
	switch f := call.Func.(type) {
	case *ast.FuncNameExpr:
		return t.semCallByName(call, f.Name, args, argTypes, inType)
	case *ast.LambdaExpr:
		return t.semCallLambda(f, args, argTypes)
	default:
		panic(f)
	}
}

func (t *translator) maybeSubquery(n ast.Node, name string, inType super.Type) (*sem.SubqueryExpr, super.Type) {
	if seq, typ := t.scope.lookupQuery(t, name); seq != nil {
		return &sem.SubqueryExpr{
			Node:       n,
			Correlated: isCorrelated(seq),
			Body:       seq,
		}, typ
	}
	return nil, nil
}

func (t *translator) semCallLambda(lambda *ast.LambdaExpr, args []sem.Expr, argTypes []super.Type) (sem.Expr, super.Type) {
	funcDecl := t.resolver.newFuncDecl("lambda", lambda, t.scope)
	return t.resolver.mustResolveCall(lambda, funcDecl.id, args, argTypes)
}

func (t *translator) semCallByName(call *ast.CallExpr, name string, args []sem.Expr, argTypes []super.Type, inType super.Type) (sem.Expr, super.Type) {
	if subquery, typ := t.maybeSubqueryCall(call, name, inType); subquery != nil {
		return subquery, typ
	}
	// Check if the name resolves to a symbol in scope.
	if entry := t.scope.lookupEntry(name); entry != nil {
		switch ref := entry.ref.(type) {
		case funcParamValue:
			t.error(call, fmt.Errorf("function called via parameter %q is bound to a non-function", name))
			return badExpr, t.checker.unknown
		case *funcParamLambda:
			// Called name is a parameter inside of a function.   We only end up here
			// when actual values have been bound to the parameter (i.e., we're compiling
			// a lambda-variant function each time it is called to create each variant),
			// so we call the resolver here to create a new instance of the function being
			// called. In the case of recursion, all the lambdas that are further passed
			// as args are known (in terms of their decl IDs), so the resolver can
			// look this up in the variants of the decl and stop the recursion even if the body
			// of the called entity is not completed yet.  We won't know the type but we
			// can't know the type without function type signatures so when we integrate
			// type checking here, we will use unknown for this corner case.
			if isBuiltin(ref.id) {
				// Check argument count here for builtin functions.
				if _, err := function.New(super.NewContext(), ref.id, len(args)); err != nil {
					t.error(call, fmt.Errorf("function %q called via parameter %q: %w", ref.id, ref.param, err))
					return badExpr, t.checker.unknown
				}
				return sem.NewCall(call, ref.id, args), t.checker.unknown //XXX type check call
			}
			return t.resolver.mustResolveCall(call, ref.id, args, argTypes)
		case *opDecl:
			t.error(call, fmt.Errorf("cannot call user operator %q in an expression (consider subquery syntax)", name))
			return badExpr, t.checker.unknown
		case *sem.FuncRef:
			// FuncRefs are put in the symbol table when passing stuff to user ops, e.g.,
			// a lambda as a parameter, a &func, or a builtin like &upper.
			return t.resolver.mustResolveCall(ref, ref.ID, args, argTypes)
		case *funcDecl:
			return t.resolver.mustResolveCall(call, ref.id, args, argTypes)
		case *constDecl, *queryDecl:
			t.error(call, fmt.Errorf("%q is not a function", name))
			return badExpr, t.checker.unknown
		case thunk:
			// We're calling a function that is a user-operator parameter.
			// It must be bound to a function.
			f, _ := t.resolveThunk(ref, inType)
			funcRef, ok := f.(*sem.FuncRef)
			if !ok {
				t.error(call, fmt.Errorf("function called via parameter %q is not a function", name))
				return badExpr, t.checker.unknown
			}
			return t.resolver.mustResolveCall(call, funcRef.ID, args, argTypes)

		}
		if _, ok := entry.ref.(sem.Expr); ok {
			t.error(call, fmt.Errorf("%q is not a function", name))
			return badExpr, t.checker.unknown
		}
		panic(entry.ref)
	}
	nargs := len(args)
	nameLower := strings.ToLower(name)
	switch {
	case nameLower == "is_ok":
		if err := function.CheckArgCount(nargs, 1, 1); err != nil {
			t.error(call, err)
			return badExpr, t.checker.unknown
		}
		return &sem.BinaryExpr{
			Node: call,
			Op:   "!=",
			// Comparing none result of ok requires a noneish operator to turn
			// option nones into untyped nones since comparing nones is type sensitive.
			LHS: &sem.BinaryExpr{
				Node: call,
				Op:   "??",
				LHS:  sem.NewCall(call, "ok", args),
				RHS:  sem.NewLiteral(call, super.NewValue(super.TypeNone, nil), t.defs),
			},
			RHS: sem.NewLiteral(call, super.NewValue(super.TypeNone, nil), t.defs),
		}, super.TypeBool
	case nameLower == "map":
		return t.semMapCall(call, args, argTypes)
	case nameLower == "grep":
		if err := function.CheckArgCount(nargs, 2, 2); err != nil {
			t.error(call, err)
			return badExpr, t.checker.unknown
		}
		pattern, ok := t.maybeEvalString(args[0])
		if !ok {
			return sem.NewCall(call, "grep", args), super.TypeBool
		}
		re, err := expr.CompileRegexp(pattern)
		if err != nil {
			t.error(call.Args[0], err)
			return badExpr, t.checker.unknown
		}
		if s, ok := re.LiteralPrefix(); ok {
			return &sem.SearchTermExpr{
				Node:  call,
				Text:  s,
				Value: sup.QuotedString(s),
				Expr:  args[1],
			}, super.TypeBool
		}
		return &sem.RegexpSearchExpr{
			Node:    call,
			Pattern: pattern,
			Expr:    args[1],
		}, super.TypeBool
	case nameLower == "position" && nargs == 1:
		b, ok := args[0].(*sem.BinaryExpr)
		if ok && strings.ToLower(b.Op) == "in" {
			// Support for SQL style position function call.
			args = []sem.Expr{b.RHS, b.LHS}
			nargs = 2
		}
		fallthrough
	default:
		if _, err := function.New(t.sctx, nameLower, nargs); err != nil {
			t.error(call, err)
			return badExpr, t.checker.unknown
		}
	}
	return sem.NewCall(call, nameLower, args), t.checker.unknown
}

func (t *translator) maybeSubqueryCall(call *ast.CallExpr, name string, inType super.Type) (*sem.SubqueryExpr, super.Type) {
	decl, _ := t.scope.lookupOp(name)
	if decl == nil || decl.bad {
		return nil, nil
	}
	userOp, typ := t.userOp(call.Loc, decl, call.Args, inType)
	// When a user op is encountered inside an expression, we turn it into a
	// subquery operating on a single-shot "this" value unless it's uncorrelated
	// (i.e., starts with a from), in which case we put the uncorrelated body
	// in the subquery without the correlating values logic. This is controlled
	// via the Correlated flag.
	return &sem.SubqueryExpr{
		Node:       call,
		Array:      false,
		Correlated: isCorrelated(userOp),
		Body:       userOp,
	}, typ
}

func (t *translator) semMapCall(call *ast.CallExpr, args []sem.Expr, argTypes []super.Type) (sem.Expr, super.Type) {
	if len(args) != 2 {
		t.error(call, errors.New("map requires two arguments"))
		return badExpr, t.checker.unknown
	}
	ref, ok := args[1].(*sem.FuncRef)
	if !ok {
		t.error(call, errors.New("second argument to map must be a function"))
		return badExpr, t.checker.unknown
	}
	elemType, ok := t.checker.hasArray(argTypes[0])
	if !ok {
		t.error(call, errors.New("map expression must be an array"))
		return badExpr, t.checker.unknown
	}
	mapArgs := []sem.Expr{sem.NewThis(call.Args[1], nil)}
	mapTypes := []super.Type{elemType}
	t.checker.pushErrs()
	e, typ := t.resolver.resolveCall(call.Args[1], ref.ID, mapArgs, mapTypes)
	errs := t.checker.popErrs()
	for _, err := range errs {
		t.error(err.loc, fmt.Errorf("in function called from map: %w", err.err))
	}
	if callExpr, ok := e.(*sem.CallExpr); ok {
		return &sem.MapCallExpr{
			Node:   call,
			Expr:   args[0],
			Lambda: callExpr,
		}, t.sctx.LookupTypeArray(typ)
	}
	return e, t.sctx.LookupTypeArray(typ)
}

func (t *translator) semExtractExpr(e, partExpr, argExpr ast.Expr, inType super.Type) (sem.Expr, super.Type) {
	var partstr string
	switch p := partExpr.(type) {
	case *ast.IDExpr:
		partstr = p.Name
	case *ast.Primitive:
		if p.Type != "string" {
			t.error(partExpr, fmt.Errorf("part must be an identifier or string"))
			return badExpr, t.checker.unknown
		} else {
			partstr = p.Text
		}
	default:
		t.error(partExpr, fmt.Errorf("part must be an identifier or string"))
		return badExpr, t.checker.unknown
	}
	argSemExpr, _ := t.expr(argExpr, inType)
	return sem.NewCall(e,
		"date_part",
		[]sem.Expr{
			sem.NewLiteral(partExpr, super.NewString(strings.ToLower(partstr)), t.defs),
			argSemExpr,
		},
	), super.TypeInt64
}

func (t *translator) exprs(in []ast.Expr, inType super.Type) ([]sem.Expr, []super.Type) {
	exprs := make([]sem.Expr, 0, len(in))
	types := make([]super.Type, 0, len(in))
	for _, expr := range in {
		expr, typ := t.expr(expr, inType)
		exprs = append(exprs, expr)
		types = append(types, typ)
	}
	return exprs, types
}

func (t *translator) assignments(assignments []ast.Assignment, inType super.Type) ([]sem.Assignment, []pathType) {
	exprs := make([]sem.Assignment, 0, len(assignments))
	paths := make([]pathType, 0, len(assignments))
	for _, a := range assignments {
		e, path := t.assignment(&a, inType)
		exprs = append(exprs, e)
		paths = append(paths, path)
	}
	return exprs, paths
}

func (t *translator) assignment(assign *ast.Assignment, inType super.Type) (sem.Assignment, pathType) {
	rhs, typ := t.expr(assign.RHS, inType)
	var lhs sem.Expr
	if assign.LHS == nil {
		lhs = sem.NewThis(assign.RHS, field.NewChain(deriveNameFromExpr(assign.RHS)))
	} else {
		lhs = t.lval(assign.LHS)
	}
	path, ok := isLval(lhs)
	if !ok {
		t.error(assign, errors.New("illegal left-hand side of assignment"))
		lhs = badExpr
	}
	if this, ok := lhs.(*sem.ThisExpr); ok && len(this.Chain) == 0 {
		t.error(assign, errors.New("cannot assign to 'this'"))
		lhs = badExpr
	}
	return sem.Assignment{Node: assign, LHS: lhs, RHS: rhs}, pathType{path, typ}
}

func (t *translator) lval(e ast.Expr) sem.Expr {
	if t.scope.sql != nil {
		panic(t)
	}
	var out sem.Expr
	if id, ok := e.(*ast.IDExpr); ok {
		out, _ = t.idExpr(id, true, t.checker.unknown)
	} else {
		out, _ = t.expr(e, t.checker.unknown)
	}
	return out
}

func isLval(e sem.Expr) ([]string, bool) {
	switch e := e.(type) {
	case *sem.IndexExpr:
		_, ok := isLval(e.Expr)
		return nil, ok
	case *sem.DotExpr:
		path, ok := isLval(e.LHS)
		if ok {
			path = append(path, e.RHS)
		}
		return path, ok
	case *sem.ThisExpr:
		return e.Chain.Path(), true
	}
	return nil, false
}

func deriveNameFromExpr(e ast.Expr) string {
	switch e := e.(type) {
	case *ast.AggFuncExpr:
		return e.Name
	case *ast.CallExpr:
		if f, ok := e.Func.(*ast.FuncNameExpr); ok {
			name := f.Name
			if s := strings.ToLower(name); (s == "ok" || s == "is_ok") && len(e.Args) > 0 {
				return deriveNameFromExpr(e.Args[0])
			}
			return name
		}
	case *ast.BinaryExpr:
		if name, ok := dottedName(e); ok {
			return name
		}
	case *ast.DoubleQuoteExpr:
		if s, ok := quoteString(e); ok {
			return s
		}
	case *ast.IDExpr:
		if e.Name == "this" {
			return "that"
		}
		return e.Name
	}
	return sfmt.ASTExpr(e)
}

func dottedName(e *ast.BinaryExpr) (string, bool) {
	if e.Op == "??" {
		return deriveNameFromExpr(e.LHS), true
	}
	if e.Op != "." {
		return "", false
	}
	rhs, ok := idString(e.RHS)
	if !ok {
		return "", false
	}
	if _, ok := idString(e.LHS); ok {
		return rhs, true
	}
	if lhs, ok := e.LHS.(*ast.BinaryExpr); ok {
		if _, ok := dottedName(lhs); ok {
			return rhs, true
		}
	}
	return "", false
}

func idString(e ast.Expr) (string, bool) {
	switch e := e.(type) {
	case *ast.IDExpr:
		return e.Name, true
	case *ast.DoubleQuoteExpr:
		return quoteString(e)
	}
	return "", false
}

func quoteString(e *ast.DoubleQuoteExpr) (string, bool) {
	v, err := sup.ParsePrimitive("string", e.Text)
	if err == nil {
		return v.AsString(), true
	}
	return "", false
}

func (t *translator) fields(exprs []ast.Expr, inType super.Type) ([]sem.Expr, []super.Type) {
	fields := make([]sem.Expr, 0, len(exprs))
	types := make([]super.Type, 0, len(exprs))
	for _, expr := range exprs {
		e, typ := t.field(expr, inType)
		fields = append(fields, e)
		types = append(types, typ)
	}
	return fields, types
}

// semField analyzes the expression f and makes sure that it's
// a field reference returning an error if not.
func (t *translator) field(f ast.Expr, inType super.Type) (sem.Expr, super.Type) {
	e, typ := t.expr(f, inType)
	switch e := e.(type) {
	case *sem.ThisExpr:
		if len(e.Chain) == 0 {
			t.error(f, errors.New("cannot use 'this' as a field reference"))
			return badExpr, t.checker.unknown
		}
		return e, typ
	case *sem.BadExpr:
		return e, t.checker.unknown
	default:
		t.error(f, errors.New("invalid expression used as a field"))
		return badExpr, t.checker.unknown
	}
}

func (t *translator) maybeConvertAgg(call *ast.CallExpr, inType super.Type) (sem.Expr, super.Type) {
	name, ok := call.Func.(*ast.FuncNameExpr)
	if !ok {
		return nil, t.checker.unknown
	}
	nameLower := strings.ToLower(name.Name)
	if _, err := agg.NewPattern(t.sctx, nameLower, false, true); err != nil {
		return nil, t.checker.unknown
	}
	if err := function.CheckArgCount(len(call.Args), 0, 1); err != nil {
		if nameLower == "min" || nameLower == "max" {
			// min and max are special cases as they are also functions. If the
			// number of args is greater than 1 they're probably a function so do not
			// return an error.
			return nil, nil
		}
		t.error(call, err)
		return badExpr, t.checker.unknown
	}
	var e ast.Expr
	if len(call.Args) == 1 {
		e = call.Args[0]
	}
	return t.aggFunc(call, nameLower, e, nil, false, inType)
}

func (t *translator) aggFunc(n ast.Node, name string, arg ast.Expr, filter ast.Expr, distinct bool, inType super.Type) (sem.Expr, super.Type) {
	// If we are in the context of a having clause, re-expose the select schema
	// since the agg func's arguments and where clause operate on the input relation
	// not the output.
	scope, ok := t.scope.sql.(*selectScope)
	if ok {
		if !scope.aggOk {
			if scope.groupByLoc != nil {
				t.error(scope.groupByLoc, fmt.Errorf("aggregate function %q cannot appear in GROUP BY via positional reference", name))
			} else {
				t.error(n, fmt.Errorf("aggregate function %q called in non-aggregate context", name))
			}
			return badExpr, t.checker.unknown
		}
		scope.aggOk = false
		save := scope.out
		defer func() {
			scope.aggOk = true
			scope.out = save
		}()
		scope.out = nil
	}
	argExpr, argType := t.exprNullable(arg, inType)
	filterExpr, filterType := t.exprNullable(filter, inType)
	f := &sem.AggFunc{
		Node:     n,
		Name:     name,
		Expr:     argExpr,
		Filter:   filterExpr,
		Distinct: distinct,
	}
	if !ok {
		typ := t.checker.aggFunc(name, arg, argType, filter, filterType)
		return f, typ
	}
	for k, previous := range scope.aggs {
		if f.Name == previous.Name && f.Distinct == previous.Distinct && eqExpr(f.Expr, previous.Expr) && eqExpr(f.Filter, previous.Filter) {
			return &sem.AggRef{Node: n, Index: k}, scope.aggTypes[k]
		}
	}
	typ := t.checker.aggFunc(name, arg, argType, filter, filterType)
	ref := &sem.AggRef{Node: f, Index: len(scope.aggs)}
	scope.aggs = append(scope.aggs, f)
	scope.aggTypes = append(scope.aggTypes, typ)
	return ref, typ
}

func DotExprToFieldPath(e ast.Expr) *sem.ThisExpr {
	switch e := e.(type) {
	case *ast.BinaryExpr:
		if e.Op == "." || e.Op == "?." {
			lhs := DotExprToFieldPath(e.LHS)
			if lhs == nil {
				return nil
			}
			id, ok := e.RHS.(*ast.IDExpr)
			if !ok {
				return nil
			}
			lhs.Chain = lhs.Chain.Append(id.Name, e.Op == "?.")
			return lhs
		}
	case *ast.IndexExpr:
		this := DotExprToFieldPath(e.Expr)
		if this == nil {
			return nil
		}
		id, ok := e.Index.(*ast.Primitive)
		if !ok || id.Type != "string" {
			return nil
		}
		this.Chain = this.Chain.Append(id.Text, false)
		return this
	case *ast.IDExpr:
		return sem.NewThis(e, field.NewChain(e.Name))
	}
	// This includes a null Expr, which can happen if the AST is missing
	// a field or sets it to null.
	return nil
}

func (t *translator) semType(typ ast.Type) (sem.Expr, super.Type) {
	id, err := t.types.LookupType(typ)
	if err != nil {
		// Try looking in the input for named types which did not resolve,
		// which will be holding the parsed input
		if ref, ok := typ.(*ast.TypeRef); ok {
			if named := t.sctx.LookupByName(ref.Name); named != nil {
				// Found the type in the main context.  Translate it
				// to the typedefs context.
				local, err := t.defs.TranslateType(named)
				if err != nil {
					// There can't be conflict since we just tried looking up
					// this name in the defs context via t.types.
					panic(err)
				}
				return &sem.TypeExpr{
					Node: typ,
					ID:   super.TypeID(local),
				}, super.TypeType
			}
		}
		t.error(typ, err)
		return badExpr, t.checker.unknown
	}
	return &sem.TypeExpr{
		Node: typ,
		ID:   id,
	}, super.TypeType
}

func (t *translator) arrayElems(elems []ast.ArrayElem, inType super.Type) ([]sem.ArrayElem, super.Type) {
	var out []sem.ArrayElem
	var types []super.Type
	for _, elem := range elems {
		switch elem := elem.(type) {
		case *ast.SpreadElem:
			elemExpr, elemType := t.expr(elem.Expr, inType)
			out = append(out, &sem.SpreadElem{Node: elem, Expr: elemExpr})
			types = append(types, elemType)
		case *ast.ExprElem:
			elemExpr, elemType := t.expr(elem.Expr, inType)
			out = append(out, &sem.ExprElem{Node: elem, Expr: elemExpr})
			types = append(types, elemType)
		default:
			panic(elem)
		}
	}
	return out, t.checker.fuse(types)
}

func (t *translator) fstringExpr(f *ast.FStringExpr, inType super.Type) (sem.Expr, super.Type) {
	if len(f.Elems) == 0 {
		return sem.NewLiteral(f, super.NewString(""), t.defs), super.TypeString
	}
	var out sem.Expr
	for _, elem := range f.Elems {
		var e sem.Expr
		switch elem := elem.(type) {
		case *ast.FStringExprElem:
			e, _ = t.expr(elem.Expr, inType)
			e = sem.NewCast(f, e, super.TypeString, t.defs)
		case *ast.FStringTextElem:
			e = sem.NewLiteral(elem, super.NewString(elem.Text), t.defs)
		default:
			panic(elem)
		}
		if out == nil {
			out = e
			continue
		}
		out = sem.NewCall(f, "concat", []sem.Expr{out, e})
	}
	return out, super.TypeString
}

func (t *translator) arraySubquery(elems []sem.ArrayElem) *sem.SubqueryExpr {
	if len(elems) == 1 {
		if elem, ok := elems[0].(*sem.ExprElem); ok {
			if subquery, ok := elem.Expr.(*sem.SubqueryExpr); ok {
				subquery.Array = true
				return subquery
			}
		}
	}
	return nil
}

func (t *translator) subqueryExpr(astExpr ast.Expr, array bool, body ast.Seq, inType super.Type) (*sem.SubqueryExpr, super.Type) {
	// We pass inType in whether or not it's correlated since an uncorrelated
	// subquery will just ignore it.
	seq, outType := t.seq(body, inType)
	correlated := isCorrelated(seq)
	e := &sem.SubqueryExpr{
		Node:       astExpr,
		Array:      array,
		Correlated: correlated,
		Body:       seq,
	}
	// Add a nullscan only for uncorrelated queries that don't have a source.
	if !correlated && !HasSource(e.Body) {
		e.Body.Prepend(&sem.NullScan{})
	}
	if !array && t.scope.sql != nil {
		// SQL expects a record with a single column result so unravel this
		// condition with this complex cleanup...
		e.Body, outType = t.scalarSubqueryCheck(astExpr, e.Body, outType)
	}
	if array {
		outType = t.sctx.LookupTypeArray(outType)
	}
	return e, outType
}

func (t *translator) scalarSubqueryCheck(n ast.Node, seq sem.Seq, thisType super.Type) (sem.Seq, super.Type) {
	// In a SQL expression (except for RHS of EXISTS or IN, which both use array result),
	// a subquery returns a scalar but the result of the subquery is a relation.
	// The runtime already checks if the subquery returns multiple "rows", so
	// here we just check if the relation is a single column and error appropriately
	// or otherwise pull out the value to make the scalar.
	// values is_error(this) ? this : (len(this) == 1 ? this[1] : error(...) )
	lenCall := &sem.CallExpr{
		Node: n,
		Tag:  "len",
		Args: []sem.Expr{sem.NewThis(n, nil)},
	}
	lenCond := &sem.BinaryExpr{
		Node: n,
		Op:   "==",
		LHS:  lenCall,
		RHS:  sem.NewLiteral(n, super.NewInt64(1), t.defs),
	}
	// Note no need to check index_base here since we are directly
	// creating the underlying index expression.
	indexExpr := &sem.IndexExpr{
		Node:  n,
		Expr:  sem.NewThis(n, nil),
		Index: sem.NewLiteral(n, super.NewInt64(0), t.defs),
	}
	innerCond := &sem.CondExpr{
		Node: n,
		Cond: lenCond,
		Then: indexExpr,
		Else: sem.NewStructuredError(n, "subquery expression cannot have multiple columns", sem.NewThis(n, nil)),
	}
	isErrCond := &sem.CallExpr{
		Node: n,
		Tag:  "is_error",
		Args: []sem.Expr{sem.NewThis(n, nil)},
	}
	outerCond := &sem.CondExpr{
		Node: n,
		Cond: isErrCond,
		Then: sem.NewThis(n, nil),
		Else: innerCond,
	}
	return append(seq, &sem.ValuesOp{
		Node:  n,
		Exprs: []sem.Expr{outerCond},
	}), t.checker.expr(thisType, outerCond)
}

func isCorrelated(seq sem.Seq) bool {
	switch op := seq[0].(type) {
	case *sem.CommitMetaScan,
		*sem.DBMetaScan,
		*sem.DeleteScan,
		*sem.FileScan,
		*sem.HTTPScan,
		*sem.NullScan,
		*sem.PoolMetaScan,
		*sem.PoolScan:
		return false
	case *sem.ForkOp:
		return slices.ContainsFunc(op.Paths, isCorrelated)
	case *sem.SwitchOp:
		// Switch output doesn't necessarily depend on input (e.g.,
		// `switch case true ( values 1 )`), but in practice it does so
		// precise analysis doesn't seem worth the trouble.
		return true
	}
	return true
}
