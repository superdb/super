package semantic

import (
	"errors"
	"fmt"

	"github.com/superdb/super"
	"github.com/superdb/super/compiler/ast"
	"github.com/superdb/super/compiler/semantic/sem"
	"github.com/superdb/super/pkg/field"
)

type Scope struct {
	parent  *Scope
	pragmas map[string]any
	symbols map[string]*entry
	ctes    map[string]*sqlCTE
	sql     relScope
}

func NewScope(parent *Scope) *Scope {
	return &Scope{
		parent:  parent,
		pragmas: make(map[string]any),
		symbols: make(map[string]*entry),
		ctes:    make(map[string]*sqlCTE),
	}
}

type entry struct {
	ref   any
	order int
}

func (s *Scope) BindSymbol(name string, e any) error {
	if _, ok := s.symbols[name]; ok {
		return fmt.Errorf("symbol %q redefined", name)
	}
	s.symbols[name] = &entry{ref: e, order: len(s.symbols)}
	return nil
}

func (s *Scope) lookupOp(name string) (*opDecl, error) {
	if entry := s.lookupEntry(name); entry != nil {
		d, ok := entry.ref.(*opDecl)
		if !ok {
			return nil, fmt.Errorf("symbol %q is not bound to an operator", name)
		}
		return d, nil
	}
	return nil, nil
}

func (s *Scope) lookupQuery(t *translator, name string) (sem.Seq, super.Type) {
	if entry := s.lookupEntry(name); entry != nil {
		if decl, ok := entry.ref.(*queryDecl); ok {
			return decl.resolve(t)
		}
	}
	return nil, nil
}

func (s *Scope) lookupEntry(name string) *entry {
	for scope := s; scope != nil; scope = scope.parent {
		if entry, ok := scope.symbols[name]; ok {
			return entry
		}
	}
	return nil
}

func (s *Scope) lookupPragma(name string) any {
	for scope := s; scope != nil; scope = scope.parent {
		if p, ok := scope.pragmas[name]; ok {
			return p
		}
	}
	return nil
}

func (s *Scope) lookupExpr(t *translator, loc ast.Node, name string, inType super.Type) (sem.Expr, super.Type) {
	if entry := s.lookupEntry(name); entry != nil {
		// function parameters hide exteral definitions as you don't
		// want the this.param ref to be overriden by a const etc.
		switch entry := entry.ref.(type) {
		case *funcDecl, *ast.FuncNameExpr, funcParamValue, *opDecl:
			return nil, nil
		case *funcParamLambda:
			// When we're inside a function body and we bind an ID to a
			// function parameter, we can't resolve it here (because the resolver
			// needs to unfurl it using the mix of lamba/actuals).  The only
			// allowed use here is as a call reference (but that will be caught
			// by semCall before ever getting here) or it can be passed as
			// an arg to another function (which does get here).  In this case,
			// we return a sem.FuncRef that the resolver will recognize to
			// properly unfurl the function call.  Otherwise, we could have
			// a random reference to a sem.FuncRef in an expression that otherwise
			// escapes type checking, but dagen will see it and report the error.
			return &sem.FuncRef{Node: loc, ID: entry.id}, t.checker.unknown
		case *constDecl:
			e := entry.resolve(t)
			return e, t.checker.expr(inType, e)
		case thunk:
			return t.resolveThunk(entry, inType)
		}
		e := entry.ref.(sem.Expr)
		return e, t.checker.expr(inType, e)
	}
	return nil, nil
}

// Returns the decl ID of a function declared with the given name in
// this scope or a function value passed as a lambda parameter and bound
// to formal parameter with the given name.
func (s *Scope) lookupFuncDeclOrParam(name string) (string, error) {
	entry := s.lookupEntry(name)
	if entry == nil {
		return "", nil
	}
	switch ref := entry.ref.(type) {
	case *funcDecl:
		return ref.id, nil
	case *funcParamLambda:
		return ref.id, nil
	}
	return "", fmt.Errorf("%q is not bound to a function", name)
}

func (s *Scope) resolveThis(t *translator, n ast.Node, inType super.Type) (sem.Expr, super.Type) {
	if e := s.sql.this(n, nil); e != nil {
		return e, t.checker.expr(inType, e)
	}
	t.error(n, errors.New("cannot reference 'this' in this context"))
	return badExpr, badType
}

// resolve paths based on SQL semantics in order of precedence
// and replace with dag path with schemafied semantics.
// In the case of unqualified col ref, check that it is not ambiguous
// when there are multiple tables (i.e., from joins).
func (s *Scope) resolve(t *translator, n ast.Node, path field.Path, inType super.Type) (sem.Expr, super.Type) {
	// If there's no relational scope, we're not in a SQL context so we just
	// return the path unmodified.  Otherwise, we apply SQL scoping
	// rules to transform the abstract path to the dataflow path
	// implied by the relational scope.
	scope := s.sql
	if scope == nil || len(path) == 0 {
		panic(s)
	}
	var inputFirst bool
	if pg := s.lookupPragma("pg"); pg != nil {
		if b, ok := pg.(bool); ok {
			inputFirst = b
		}
	}
	// If the output projection is in place, then we are outside the
	// SELECT body (e.g., in ORDER BY), so we need to first check
	// the select column names before the inputs.  This is handled by
	// other SQLs by looking in the output table, but we can't do that
	// since we can't mix output columns and input columns
	// in the grouping expression matcher.  So we rely upon this column
	// lookup to give us the input bindings and then match everything to its
	// grouped output.
	if sch, ok := scope.(*selectScope); ok && sch.out != nil && sch.isGrouped() {
		for _, c := range sch.columns {
			if c.name == path[0] {
				e := extend(n, c.semExpr, path[1:])
				return e, t.checker.expr(inType, e)
			}
		}
	}
	// We give priority to lateral column aliases which diverges from
	// PostgreSQL semantics (which checks the input table first) but follows
	// Google SQL semantics.  We favor this approach so we can avoid
	// precedence problems for dynamic inputs or super-structured inputs
	// that evolve, e.g., with a super-structured input, whether a column
	// is present in the input shouldn't control the scoping decision compared
	// to a column aliases otherwise as data evolves (and such a column shows up)
	// the binding of identifiers changes and query results can change.  By
	// resolving lateral aliases first, we avoid this problem.  This can
	// be overridden with "pragma pg" to favor PostgreSQL precedence.
	if scope, ok := scope.(*selectScope); ok && scope.lateral && !inputFirst {
		if e, _ := resolveLateralColumn(t, scope, path[0], inType); e != nil {
			e := extend(n, e, path[1:])
			return e, t.checker.expr(inType, e)
		}
	}
	out, dyn, err := scope.resolveUnqualified(path[0])
	if err != nil {
		t.error(n, err)
		return badExpr, t.checker.unknown
	}
	if out != nil {
		if dyn {
			// Make sure there's not a table with the same name as the column name.
			if _, tdyn, err := scope.resolveTable(n, path[0], nil); err != nil && tdyn {
				t.error(n, fmt.Errorf("cannot use unqualified reference %q when table of same name is in scope (consider qualified reference)", path[0]))
				return badExpr, t.checker.unknown
			}
		}
		this := sem.NewThis(n, field.NewChainNullish(append(out, path[1:]...)...))
		return this, t.checker.this(n, this, inType)
	}
	if scope, ok := scope.(*selectScope); ok && scope.lateral && inputFirst {
		if e, _ := resolveLateralColumn(t, scope, path[0], inType); e != nil {
			e := extend(n, e, path[1:])
			return e, t.checker.expr(inType, e)
		}
	}
	if len(path) == 1 {
		// Single identifier didn't resolve to column so let's see if there's
		// a table with this name and return an expression capturing the
		// rows as single-column records.
		out, _, err := scope.resolveTable(n, path[0], nil)
		if err != nil {
			t.error(n, err)
			return badExpr, t.checker.unknown
		}
		if out != nil {
			return out, t.checker.expr(inType, out)
		}
	} else {
		// Look for match of a qualified reference (table.col).
		out, err := scope.resolveQualified(path[0], path[1])
		if err != nil {
			t.error(n, err)
			return badExpr, t.checker.unknown
		}
		if out != nil {
			this := sem.NewThis(n, field.NewChainNullish(append(out, path[2:]...)...))
			return this, t.checker.this(n, this, inType)
		}
		if p, _, _ := scope.resolveTable(n, path[0], nil); p != nil {
			t.error(n, fmt.Errorf("column %q does not exist in table %q", path[1], path[0]))
			return badExpr, t.checker.unknown
		}
	}
	t.error(n, fmt.Errorf("%q is not a column or table", path[0]))
	return badExpr, t.checker.unknown
}

func extend(n ast.Node, e sem.Expr, rest []string) sem.Expr {
	if len(rest) == 0 {
		return e
	}
	if this, ok := e.(*sem.ThisExpr); ok {
		return sem.NewThis(n, append(this.Chain, field.NewChainNullish(rest...)...))
	}
	out := &sem.DotExpr{
		Node: n,
		LHS:  e,
		RHS:  rest[0],
	}
	for _, f := range rest[1:] {
		out = &sem.DotExpr{
			Node:    n,
			LHS:     out,
			RHS:     f,
			Nullish: true,
		}
	}
	return out
}

func resolveLateralColumn(t *translator, scope *selectScope, col string, inType super.Type) (sem.Expr, super.Type) {
	if !scope.lateral {
		// lateral columns available only inside select bodies
		return nil, t.checker.unknown
	}
	for _, c := range scope.columns {
		if c.lateral && c.name == col {
			defer func() {
				scope.lateral = true
			}()
			scope.lateral = false
			return t.expr(c.astExpr, inType)
		}
	}
	return nil, t.checker.unknown
}

func (s *Scope) indexBase() int {
	if v := s.lookupPragma("index_base"); v != nil {
		return v.(int)
	}
	return 0
}
