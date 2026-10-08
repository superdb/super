package semantic

import (
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"

	"uuid"
	"github.com/superdb/super"
	"github.com/superdb/super/compiler/ast"
	"github.com/superdb/super/compiler/dag"
	"github.com/superdb/super/compiler/semantic/sem"
	"github.com/superdb/super/dbid"
	"github.com/superdb/super/order"
	"github.com/superdb/super/pkg/field"
	"github.com/superdb/super/pkg/plural"
	"github.com/superdb/super/pkg/reglob"
	"github.com/superdb/super/runtime/expr/function"
	"github.com/superdb/super/runtime/sam/expr"
	"github.com/superdb/super/sbuf"
	"github.com/superdb/super/sio"
	"github.com/superdb/super/sio/anyio"
	"github.com/superdb/super/sup"
	"github.com/superdb/super/vector"
)

func (t *translator) seq(in ast.Seq, typ super.Type) (sem.Seq, super.Type) {
	var seq sem.Seq
	for len(in) > 0 {
		if len(in) >= 2 {
			if s, t := t.parentJoin(in, seq, typ); s != nil {
				seq = s
				typ = t
				in = in[2:]
				continue
			}
		}
		seq, typ = t.semOp(in[0], seq, typ)
		in = in[1:]
	}
	return seq, typ
}

func (t *translator) parentJoin(in ast.Seq, seq sem.Seq, inType super.Type) (sem.Seq, super.Type) {
	join, ok := in[1].(*ast.JoinOp)
	if !ok {
		return nil, nil
	}
	var types []super.Type
	switch op := in[0].(type) {
	case *ast.ForkOp:
		seq, types = t.forkOp(op, seq, inType)
	case *ast.SwitchOp:
		seq, types = t.switchOp(op, seq, inType)
	default:
		return nil, nil
	}
	return t.joinOp(join, seq, types)
}

func (t *translator) fromSource(entity ast.FromSource, args []ast.OpArg, seq sem.Seq) (sem.Seq, super.Type, string) {
	switch entity := entity.(type) {
	case *ast.GlobExpr:
		if t.env.IsAttached() {
			// XXX need to get fused type from pool
			return t.fromPoolRegexp(entity, reglob.Reglob(entity.Pattern), entity.Pattern, "glob", args), nil, ""
		}
		// XXX should fuse the types across the glob instead of unknown
		op := t.fromFileGlob(entity, entity.Pattern, args)
		return append(seq, op), t.checker.unknown, ""
	case *ast.RegexpExpr:
		if bad := t.hasFromParent(entity, seq); bad != nil {
			return bad, badType, ""
		}
		if !t.env.IsAttached() {
			t.error(entity, errors.New("cannot use regular expression with from operator on local file system"))
			return seq, badType, ""
		}
		// XXX need to get fused type from pool
		return t.fromPoolRegexp(entity, entity.Pattern, entity.Pattern, "regexp", args), t.checker.unknown, ""
	case *ast.Text:
		if seq, typ := t.scope.lookupQuery(t, entity.Text); seq != nil {
			return seq, typ, entity.Text
		}
		op, def := t.fromName(entity, entity.Text, args)
		if op, ok := op.(*sem.FileScan); ok {
			typ := op.Type
			if typ == nil {
				typ = t.checker.unknown
			}
			return append(seq, op), typ, def
		}
		return sem.Seq{op}, t.checker.unknown, def
	case *ast.FromEval:
		seq, def := t.fromFString(entity, args, seq)
		return seq, t.checker.unknown, def
	case *ast.DBMeta:
		if bad := t.hasFromParent(entity, seq); bad != nil {
			return bad, badType, ""
		}
		return sem.Seq{t.dbMeta(entity)}, t.checker.unknown, ""
	default:
		panic(entity)
	}
}

func (t *translator) sqlTableExpr(e ast.SQLTableExpr, seq sem.Seq) (sem.Seq, relScope) {
	switch e := e.(type) {
	case *ast.SQLFromItem:
		if e.Ordinality != nil {
			t.error(e.Ordinality, errors.New("WITH ORDINALITY clause is not yet supported"))
			return seq, badTable
		}
		alias := e.Alias
		switch input := e.Input.(type) {
		case *ast.FromItem:
			var table relTable
			if c, name := t.maybeCTE(input.Source); c != nil {
				if bad := t.hasFromParent(input, seq); bad != nil {
					return bad, badTable
				}
				if len(input.Args) != 0 {
					t.error(input, fmt.Errorf("CTE cannot use operator arguments"))
					return seq, badTable
				}
				if alias == nil {
					alias = c.Name
				}
				seq, table = t.fromCTE(input, c)
			} else {
				if _, ok := input.Source.(*ast.FromEval); !ok {
					if bad := t.hasFromParent(e, seq); bad != nil {
						return bad, badTable
					}
				}
				var typ super.Type
				seq, typ, name = t.fromSource(input.Source, input.Args, seq)
				table = newTableFromType(typ, t.checker.unknown)
				if _, ok := table.(*dynamicTable); !ok && alias == nil {
					alias = &ast.TableAlias{Name: name, Loc: input.Loc}
				}
			}
			if table == badTable {
				return seq, badTable
			}
			seq, table, err := applyAlias(t.sctx, alias, table, seq)
			if err != nil {
				t.error(alias, err)
			}
			return seq, table
		case *ast.SQLPipe:
			seq, sch := t.sqlFromPipe(input, seq)
			seq, sch, err := applyAlias(t.sctx, alias, sch, seq)
			if err != nil {
				t.error(alias, err)
			}
			return seq, sch
		default:
			panic(input)
		}
	case *ast.SQLJoin:
		return t.sqlJoin(e, seq)
	case *ast.SQLCrossJoin:
		return t.sqlCrossJoin(e, seq)
	default:
		panic(e)
	}
}

func (t *translator) maybeCTE(source ast.FromSource) (*ast.SQLCTE, string) {
	if text, ok := source.(*ast.Text); ok {
		if c, ok := t.scope.ctes[strings.ToLower(text.Text)]; ok {
			return c, text.Text
		}
	}
	return nil, ""
}

func (t *translator) fromCTE(node ast.Node, c *ast.SQLCTE) (sem.Seq, relTable) {
	if slices.Contains(t.cteStack, c) {
		t.error(node, errors.New("recursive WITH relations not currently supported"))
		return sem.Seq{badOp}, badTable
	}
	t.cteStack = append(t.cteStack, c)
	defer func() {
		t.cteStack = t.cteStack[:len(t.cteStack)-1]
	}()
	seq, scope := t.sqlQueryBody(c.Body, nil, nil, nil)
	return scope.endScope(node, seq)
}

func (t *translator) fromFString(entity *ast.FromEval, args []ast.OpArg, seq sem.Seq) (sem.Seq, string) {
	expr, _ := t.fstringExpr(entity.Expr, t.checker.unknown)
	val, ok := t.maybeEval(expr)
	if ok && !t.hasError(val) {
		if bad := t.hasFromParent(entity, seq); bad != nil {
			return bad, ""
		}
		return t.fromConst(val, entity, args)
	}
	// This is an expression so set up a robot scanner that pulls values from
	// parent to decide what to scan.
	return append(seq, &sem.RobotScan{
		Node:   entity,
		Expr:   expr,
		Format: t.asFormatArg(args),
	}), ""
}

func (t *translator) hasError(val super.Value) bool {
	vec := sbuf.Dematerialize(t.sctx, val)
	vec = function.NewHasError(t.sctx).Call(vec)
	val = vector.ValueAt(nil, vec, 0)
	return val.AsBool()
}

func (t *translator) hasFromParent(loc ast.Node, seq sem.Seq) sem.Seq {
	if len(seq) > 0 {
		t.error(loc, errors.New("from operator cannot have parent unless its argument is a non-constant f-string"))
		return append(seq, badOp)
	}
	return nil
}

func (t *translator) fromConst(val super.Value, entity *ast.FromEval, args []ast.OpArg) (sem.Seq, string) {
	if super.TypeUnder(val.Type()) == super.TypeString {
		op, name := t.fromName(entity, val.AsString(), args)
		return sem.Seq{op}, name
	}
	vals, ok := val.Elements()
	if !ok {
		t.error(entity.Expr, fmt.Errorf("from expression requires a string but encountered %s", sup.String(val)))
		return sem.Seq{badOp}, ""
	}
	names := make([]string, 0, len(vals))
	for _, val := range vals {
		if !hasString(val.Type()) {
			t.error(entity.Expr, fmt.Errorf("from expression requires a string but encountered %s", sup.String(val)))
			return sem.Seq{badOp}, ""
		}
		names = append(names, val.AsString())
	}
	if len(names) == 1 {
		op, _ := t.fromName(entity, names[0], args)
		return sem.Seq{op}, names[0]
	}
	var paths []sem.Seq
	for _, name := range names {
		op, _ := t.fromName(entity, name, args)
		paths = append(paths, sem.Seq{op})
	}
	return sem.Seq{
		&sem.ForkOp{
			Paths: paths,
		},
	}, ""
}

func (t *translator) fromName(node ast.Node, name string, args []ast.OpArg) (sem.Op, string) {
	if isURL(name) {
		return t.fromURL(node, name, args), ""
	}
	prefix, _, _ := strings.Cut(filepath.Base(name), ".")
	if t.env.IsAttached() {
		return t.pool(node, name, args), prefix
	}
	return t.file(node, name, args), prefix
}

func (t *translator) asFormatArg(args []ast.OpArg) string {
	opArgs := t.opArgs(args, "format")
	s, _ := t.textArg(opArgs, "format")
	return s
}

func (t *translator) file(n ast.Node, name string, args []ast.OpArg) sem.Op {
	format := t.asFormatArg(args)
	if format == "" {
		format = t.env.ReaderOpts.Format
	}
	if format == "" {
		format = sio.FormatFromPath(name)
	}
	typ, err := t.fileType(name, format)
	if err != nil {
		t.error(n, err)
		typ = badType
	}
	return &sem.FileScan{
		Node:   n,
		Type:   typ,
		Paths:  []string{name},
		Format: format,
	}
}

func (t *translator) fileType(path, format string) (super.Type, error) {
	if t.env.Dynamic {
		return t.checker.unknown, nil
	}
	engine := t.env.Engine()
	if engine == nil {
		return t.checker.unknown, nil
	}
	opts := t.env.ReaderOpts
	opts.Format = format
	typ, err := anyio.FileType(t.ctx, t.sctx, engine, path, opts, t.env.Static)
	if typ == nil {
		typ = t.checker.unknown
	}
	return typ, err
}

func (t *translator) fromFileGlob(globLoc ast.Node, pattern string, args []ast.OpArg) sem.Op {
	names, err := filepath.Glob(pattern)
	if err != nil {
		t.error(globLoc, err)
		return badOp
	}
	if len(names) == 0 {
		t.error(globLoc, errors.New("no file names match glob pattern"))
		return badOp
	}
	if len(names) == 1 {
		return t.file(globLoc, names[0], args)
	}
	paths := make([]sem.Seq, 0, len(names))
	for _, name := range names {
		paths = append(paths, sem.Seq{t.file(globLoc, name, args)})
	}
	return &sem.ForkOp{
		Paths: paths,
	}
}

func (t *translator) fromURL(urlLoc ast.Node, u string, args []ast.OpArg) sem.Op {
	_, err := url.ParseRequestURI(u)
	if err != nil {
		t.error(urlLoc, err)
		return badOp
	}
	opArgs := t.opArgs(args, "format", "method", "body", "headers")
	format, _ := t.textArg(opArgs, "format")
	method, _ := t.textArg(opArgs, "method")
	body, _ := t.textArg(opArgs, "body")
	var headers map[string][]string
	if e, loc := t.exprArg(opArgs, "headers"); e != nil {
		if val, ok := t.mustEval(e); ok {
			headers, err = unmarshalHeaders(val)
			if err != nil {
				t.error(loc, err)
			}
		}
	}
	if format == "" {
		format = sio.FormatFromPath(u)
	}
	return &sem.HTTPScan{
		Node:    urlLoc,
		URL:     u,
		Format:  format,
		Method:  method,
		Headers: headers,
		Body:    body,
	}
}

func unmarshalHeaders(val super.Value) (map[string][]string, error) {
	if !super.IsRecordType(val.Type()) {
		return nil, errors.New("headers value must be a record")
	}
	headers := map[string][]string{}
	for i, f := range val.Fields() {
		fieldVal := val.DerefByColumn(i)
		if fieldVal.IsMissing() {
			continue
		}
		headerStrings, err := decodeStrings(fieldVal)
		if err != nil {
			return nil, err
		}
		headers[f.Name] = append(headers[f.Name], headerStrings...)
	}
	return headers, nil
}

func decodeStrings(val *super.Value) ([]string, error) {
	typ := super.TypeUnder(val.Type())
	if inner := super.InnerType(typ); inner != nil {
		if inner.ID() != super.IDString {
			return nil, errors.New("array elements of header field must be strings")
		}
		var out []string
		for it := val.ContainerIter(); !it.Done(); {
			out = append(out, super.DecodeString(it.Next()))
		}
		return out, nil
	}
	if typ != super.TypeString {
		return nil, errors.New("header field value must be a string or an array or set of strings")
	}
	return []string{val.AsString()}, nil
}

func (t *translator) fromPoolRegexp(node ast.Node, re, orig, which string, args []ast.OpArg) sem.Seq {
	poolNames, err := t.matchPools(re, orig, which)
	if err != nil {
		t.error(node, err)
		return sem.Seq{badOp}
	}
	var paths []sem.Seq
	for _, name := range poolNames {
		paths = append(paths, sem.Seq{t.pool(node, name, args)})
	}
	return sem.Seq{&sem.ForkOp{Paths: paths}}
}

func (t *translator) sortExpr(sch relScope, s ast.SortExpr, reverse bool, inType super.Type) sem.SortExpr {
	var e sem.Expr
	if ts, ok := sch.(tableScope); ok && ts != nil {
		if colno, ok := isOrdinal(s.Expr); ok {
			e = t.resolveOrdinalOuter(ts, s.Expr, "", colno)
		} else if sel, ok := ts.(*selectScope); ok {
			e, _ = t.groupedExpr(sel, s.Expr, inType)
		} else {
			e, _ = t.expr(s.Expr, inType)
		}
	} else {
		e, _ = t.expr(s.Expr, inType)
	}
	o := order.Asc
	if s.Order != nil {
		var err error
		if o, err = order.Parse(s.Order.Name); err != nil {
			t.error(s.Order, err)
		}
	}
	if reverse {
		o = o.Reverse()
	}
	n := order.NullsLast
	if s.Nulls != nil {
		if err := n.UnmarshalText([]byte(s.Nulls.Name)); err != nil {
			t.error(s.Nulls, err)
		}
	}
	return sem.SortExpr{Node: s, Expr: e, Order: o, Nulls: n}
}

func (t *translator) pool(node ast.Node, poolName string, args []ast.OpArg) sem.Op {
	opArgs := t.opArgs(args, "commit", "meta", "tap")
	poolID, err := t.env.PoolID(t.ctx, poolName)
	if err != nil {
		t.error(node, err)
		return badOp
	}
	var commitID uuid.UUID
	commit, commitLoc := t.textArg(opArgs, "commit")
	if commit != "" {
		if commitID, err = dbid.ParseID(commit); err != nil {
			commitID, err = t.env.CommitObject(t.ctx, poolID, commit)
			if err != nil {
				t.error(commitLoc, err)
				return badOp
			}
		}
	}
	meta, metaLoc := t.textArg(opArgs, "meta")
	if meta != "" {
		if _, ok := dag.CommitMetas[meta]; ok {
			if commitID == uuid.Nil() {
				commitID, err = t.env.CommitObject(t.ctx, poolID, "main")
				if err != nil {
					t.error(metaLoc, err)
					return badOp
				}
			}
			tapString, _ := t.textArg(opArgs, "tap")
			tap := tapString != ""
			return &sem.CommitMetaScan{
				Node:   node,
				Meta:   meta,
				Pool:   poolID,
				Commit: commitID,
				Tap:    tap,
			}
		}
		if _, ok := dag.PoolMetas[meta]; ok {
			return &sem.PoolMetaScan{
				Node: node,
				Meta: meta,
				ID:   poolID,
			}
		}
		t.error(metaLoc, fmt.Errorf("unknown metadata type %q", meta))
		return badOp
	}
	if commitID == uuid.Nil() {
		// This trick here allows us to default to the main branch when
		// there is a "from pool" operator with no meta query or commit object.
		commitID, err = t.env.CommitObject(t.ctx, poolID, "main")
		if err != nil {
			t.error(node, err)
			return badOp
		}
	}
	return &sem.PoolScan{
		Node:   node,
		ID:     poolID,
		Commit: commitID,
	}
}

func (t *translator) dbMeta(entity *ast.DBMeta) sem.Op {
	meta := entity.Meta.Text
	if _, ok := dag.DBMetas[meta]; !ok {
		t.error(entity, fmt.Errorf("unknown database metadata type %q in from operator", meta))
		return badOp
	}
	return &sem.DBMetaScan{
		Node: entity,
		Meta: meta,
	}
}

func (t *translator) deleteScan(op *ast.Delete) sem.Op {
	if !t.env.IsAttached() {
		t.error(op, errors.New("deletion requires database"))
		return badOp
	}
	poolID, err := t.env.PoolID(t.ctx, op.Pool)
	if err != nil {
		t.error(op, err)
		return badOp
	}
	var commitID uuid.UUID
	if op.Branch != "" {
		var err error
		if commitID, err = dbid.ParseID(op.Branch); err != nil {
			commitID, err = t.env.CommitObject(t.ctx, poolID, op.Branch)
			if err != nil {
				t.error(op, err)
				return badOp
			}
		}
	}
	return &sem.DeleteScan{
		Node:   op,
		ID:     poolID,
		Commit: commitID,
	}
}

func (t *translator) matchPools(pattern, origPattern, patternDesc string) ([]string, error) {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, err
	}
	pools, err := t.env.DB().ListPools(t.ctx)
	if err != nil {
		return nil, err
	}
	var matches []string
	for _, p := range pools {
		if re.MatchString(p.Name) {
			matches = append(matches, p.Name)
		}
	}
	if len(matches) == 0 {
		return nil, fmt.Errorf("%s: pool matching %s not found", origPattern, patternDesc)
	}
	return matches, nil
}

func (t *translator) scopeOp(op *ast.ScopeOp, inType super.Type) (sem.Seq, super.Type) {
	t.scope = NewScope(t.scope)
	defer t.exitScope()
	t.decls(op.Decls)
	return t.seq(op.Body, inType)
}

// semOp does a semantic analysis on a flowgraph to an
// intermediate representation that can be compiled into the runtime
// object.  Currently, it only replaces the aggregate duration with
// a bucket call on the ts and replaces FunctionCalls in op context
// with either an aggregate or filter op based on the function's name.
func (t *translator) semOp(o ast.Op, seq sem.Seq, inType super.Type) (sem.Seq, super.Type) {
	switch o := o.(type) {
	case *ast.SQLOp:
		seq, sch := t.sqlQueryBody(o.Body, nil, seq, inType)
		seq, scope := sch.endScope(o.Body, seq)
		var typ super.Type
		if scope != badTable {
			typ = scope.typ
		} else {
			typ = t.checker.unknown
		}
		return seq, typ
	case *ast.FileScan:
		format := t.env.ReaderOpts.Format
		fuser := t.checker.newFuser()
		paths := slices.Clone(o.Paths)
		for i, p := range paths {
			if p == "-" {
				p = "stdio:stdin"
				paths[i] = p
			}
			typ, err := t.fileType(p, format)
			if typ == nil || err != nil {
				if err != nil {
					t.reporter.AddError(p+": "+err.Error(), -1, -1)
				}
				fuser = nil
				break
			}
			fuser.fuse(typ)
		}
		typ := super.Type(t.checker.unknown)
		if fuser != nil {
			typ = fuser.Type()
		}
		return append(seq, &sem.FileScan{
			Node:   o,
			Type:   typ,
			Paths:  paths,
			Format: format,
		}), typ
	case *ast.FromOp:
		seq, typ, _ := t.fromSource(o.Item.Source, o.Item.Args, seq)
		return seq, typ
	case *ast.Delete:
		if len(seq) > 0 {
			panic("analyzer.SemOp: delete scan cannot have parent in AST")
		}
		return sem.Seq{t.deleteScan(o)}, t.checker.unknown
	case *ast.AggregateOp:
		keys, keyPaths := t.assignments(o.Keys, inType)
		t.checkStaticAssignment(o.Keys, keys)
		if len(keys) == 0 && len(o.Aggs) == 1 {
			if seq, typ := t.singletonAgg(&o.Aggs[0], seq, inType); seq != nil {
				return seq, typ
			}
		}
		if len(keys) == 1 && len(o.Aggs) == 0 {
			if seq, typ := t.singletonKey(o.Keys[0], seq, inType); seq != nil {
				return seq, typ
			}
		}
		aggs, aggPaths := t.assignments(o.Aggs, inType)
		t.checkStaticAssignment(o.Aggs, aggs)
		return append(seq, &sem.AggregateOp{
			Node:  o,
			Limit: o.Limit,
			Keys:  keys,
			Aggs:  aggs,
		}), t.checker.pathsToType(append(keyPaths, aggPaths...))
	case *ast.ForkOp:
		seq, types := t.forkOp(o, seq, inType)
		return seq, t.checker.fuse(types)
	case *ast.ScopeOp:
		ops, typ := t.scopeOp(o, inType)
		return append(seq, ops...), typ
	case *ast.SwitchOp:
		seq, types := t.switchOp(o, seq, inType)
		return seq, t.checker.fuse(types)
	case *ast.CountOp:
		var alias string
		var expr sem.Expr
		var fields []super.Field
		if o.Expr == nil {
			alias = "count"
			expr = &sem.RecordExpr{
				Elems: []sem.RecordElem{
					&sem.FieldElem{Name: "that", Value: sem.NewThis(nil, nil)},
				},
			}
			fields = []super.Field{super.NewField("that", inType)}
		} else {
			n := len(o.Expr.Elems)
			if n == 0 {
				t.error(o.Expr, errors.New("count record expression must not be empty"))
				return append(seq, badOp), t.checker.unknown
			}
			last := o.Expr.Elems[n-1]
			if exprElem, ok := last.(*ast.ExprElem); ok {
				if id, ok := exprElem.Expr.(*ast.IDExpr); ok {
					alias = id.Name
				}
			}
			if alias == "" {
				t.error(last, errors.New("last element in record expression for count must be an identifier"))
				return append(seq, badOp), t.checker.unknown
			}
			if n > 1 {
				e, typ := t.expr(&ast.RecordExpr{
					Kind:  "RecordExpr",
					Elems: o.Expr.Elems[:n-1],
					Loc:   o.Expr.Loc,
				}, inType)
				var ok bool
				expr, ok = e.(*sem.RecordExpr)
				if !ok {
					return append(seq, badOp), t.checker.unknown
				}
				// Check that typ is a legit TypeRecord and not unknown.
				// If unknown then there are unknowns in the spread so just
				// propagate the unknown.
				recType, ok := typ.(*super.TypeRecord)
				if !ok {
					// We don't know the type of the non-count fields so
					// just propagate unknown.
					return append(seq, &sem.CountOp{
						Node:  o,
						Alias: alias,
						Expr:  expr,
					}), t.checker.unknown
				}
				fields = slices.Clone(recType.Fields)
			}
		}
		fields = append(fields, super.NewField(alias, super.TypeInt64))
		return append(seq, &sem.CountOp{
			Node:  o,
			Alias: alias,
			Expr:  expr,
		}), t.sctx.MustLookupTypeRecord(fields)
	case *ast.CutOp:
		//XXX When cutting an lval with no LHS, promote the lval to the LHS so
		// it is not auto-inferred.  We will change cut to use paths in a future PR.
		// Currently there is work in optimizer and parallelizer to manage changing
		// the tests that use cut to use values instead.  This work needed to be done
		// anyway, but we don't want to change cut until we're ready to do that work.
		for k, arg := range o.Args {
			if arg.LHS == nil {
				// Push and pop to prevent duplicating errors from t.assignments below.
				t.checker.pushErrs()
				rhs, _ := t.expr(arg.RHS, inType)
				t.checker.popErrs()
				if _, ok := isLval(rhs); ok {
					o.Args[k].LHS = arg.RHS
				}
			}
		}
		assignments, paths := t.assignments(o.Args, inType)
		// Collect static paths so we can check on what is available.
		var fields field.List
		for _, a := range assignments {
			if this, ok := a.LHS.(*sem.ThisExpr); ok {
				fields = append(fields, this.Chain.Path())
			}
		}
		if _, err := super.NewRecordBuilder(t.sctx, fields); err != nil {
			t.error(o.Args, err)
			return append(seq, badOp), t.checker.unknown
		}
		return append(seq, &sem.CutOp{
			Node: o,
			Args: assignments,
		}), t.checker.pathsToType(paths)
	case *ast.DebugOp:
		e, _ := t.exprNullable(o.Expr, inType)
		if e == nil {
			e = sem.NewThis(o.Expr, nil)
		}
		var f sem.Expr
		if o.Filter != nil {
			var typ super.Type
			f, typ = t.exprNullable(o.Filter, inType)
			t.checker.boolean(o.Filter, typ)
		}
		return append(seq, &sem.DebugOp{
			Node:   o,
			Expr:   e,
			Filter: f,
		}), inType
	case *ast.DistinctOp:
		e, _ := t.expr(o.Expr, inType)
		return append(seq, &sem.DistinctOp{
			Node: o,
			Expr: e,
		}), inType
	case *ast.DropOp:
		args, _ := t.fields(o.Args, inType)
		if len(args) == 0 {
			t.error(o, errors.New("no fields given"))
			return append(seq, badOp), t.checker.unknown
		}
		drops := t.checker.lvalsToPaths(args)
		if drops == nil {
			panic(drops)
		}
		// Ignore errors in dropPaths since t.fields will get them.
		t.checker.pushErrs()
		typ := t.checker.dropPaths(inType, drops)
		t.checker.popErrs()
		return append(seq, &sem.DropOp{
			Node: o,
			Args: args,
		}), typ
	case *ast.SortOp:
		var sortExprs []sem.SortExpr
		for _, e := range o.Exprs {
			sortExprs = append(sortExprs, t.sortExpr(nil, e, o.Reverse, inType))
		}
		return append(seq, &sem.SortOp{
			Node:    o,
			Exprs:   sortExprs,
			Reverse: o.Reverse && len(sortExprs) == 0,
		}), inType
	case *ast.HeadOp:
		count := 1
		if o.Count != nil {
			count = t.mustEvalPositiveInteger(o.Count)
		}
		return append(seq, &sem.HeadOp{
			Node:  o,
			Count: count,
		}), inType
	case *ast.InferOp:
		limit := 100
		if o.Limit != nil {
			limit = t.mustEvalPositiveInteger(o.Limit)
		}
		return append(seq, &sem.InferOp{
			Node:  o,
			Limit: limit,
		}), inType
	case *ast.TailOp:
		count := 1
		if o.Count != nil {
			count = t.mustEvalPositiveInteger(o.Count)
		}
		return append(seq, &sem.TailOp{
			Node:  o,
			Count: count,
		}), inType
	case *ast.SkipOp:
		return append(seq, &sem.SkipOp{
			Node:  o,
			Count: t.mustEvalPositiveInteger(o.Count),
		}), inType
	case *ast.UniqOp:
		typ := inType
		if o.Cflag {
			// Don't bother type checking this.  We can fix this later
			// if we want.
			typ = t.checker.unknown
		}
		return append(seq, &sem.UniqOp{
			Node:  o,
			Cflag: o.Cflag,
		}), typ
	case *ast.PassOp:
		return append(seq, &sem.PassOp{Node: o}), inType
	case *ast.ExprOp:
		return t.exprOp(o.Expr, seq, inType)
	case *ast.CallOp:
		return t.callOp(o, seq, inType)
	case *ast.SearchOp:
		e, typ := t.expr(o.Expr, inType)
		t.checker.boolean(o.Expr, typ)
		return append(seq, &sem.FilterOp{Node: o, Expr: e}), inType
	case *ast.WhereOp:
		e, typ := t.expr(o.Expr, inType)
		t.checker.boolean(o.Expr, typ)
		return append(seq, &sem.FilterOp{Node: o, Expr: e}), inType
	case *ast.TopOp:
		limit := 1
		if o.Limit != nil {
			l, _ := t.expr(o.Limit, inType)
			val, ok := t.mustEval(l)
			if !ok {
				return append(seq, badOp), t.checker.unknown
			}
			if !super.IsSigned(val.Type().ID()) {
				t.error(o.Limit, errors.New("limit argument must be an integer"))
				return append(seq, badOp), t.checker.unknown
			}
			if limit = int(val.Int()); limit < 1 {
				t.error(o.Limit, errors.New("limit argument value must be greater than 0"))
				return append(seq, badOp), t.checker.unknown
			}
		}
		var exprs []sem.SortExpr
		for _, e := range o.Exprs {
			exprs = append(exprs, t.sortExpr(nil, e, o.Reverse, inType))
		}
		return append(seq, &sem.TopOp{
			Node:    o,
			Limit:   limit,
			Exprs:   exprs,
			Reverse: o.Reverse && len(exprs) == 0,
		}), inType
	case *ast.PutOp:
		assignments, paths := t.assignments(o.Args, inType)
		// We can do collision checking on static paths, so check what we can.
		var fields field.List
		for _, a := range assignments {
			if this, ok := a.LHS.(*sem.ThisExpr); ok {
				fields = append(fields, this.Chain.Path())
			}
		}
		if err := checkPutFields(fields); err != nil {
			t.error(o, err)
		}
		return append(seq, &sem.PutOp{
			Node: o,
			Args: assignments,
		}), t.checker.putPaths(inType, paths)
	case *ast.AssignmentOp:
		op, typ := t.assignmentOp(o, inType)
		return append(seq, op), typ
	case *ast.RenameOp:
		var assignments []sem.Assignment
		var paths []pathType
		for _, fa := range o.Args {
			assign, path := t.assignment(&fa, inType)
			if _, ok := isLval(assign.RHS); !ok {
				t.error(fa.RHS, fmt.Errorf("illegal right-hand side of assignment"))
			}
			// If both paths are static validate them. Otherwise this will be
			// done at runtime.
			lhs, lhsOk := assign.LHS.(*sem.ThisExpr)
			rhs, rhsOk := assign.RHS.(*sem.ThisExpr)
			if rhsOk && lhsOk {
				if err := expr.CheckRenameField(lhs.Chain.Path(), rhs.Chain.Path()); err != nil {
					t.error(&fa, err)
				}
			}
			assignments = append(assignments, assign)
			paths = append(paths, path)
		}
		// Type checking for rename TBD.  Return unknown here.
		return append(seq, &sem.RenameOp{
			Node: o,
			Args: assignments,
		}), t.checker.unknown
	case *ast.FuseOp:
		return append(seq, &sem.FuseOp{Node: o, Complete: o.Complete}), inType
	case *ast.JoinOp:
		return t.joinOp(o, seq, []super.Type{inType})
	case *ast.MergeOp:
		var ok bool
		if len(seq) > 0 {
			switch seq[len(seq)-1].(type) {
			case *sem.ForkOp, *sem.SwitchOp:
				ok = true
			}
		}
		if !ok {
			t.error(o, errors.New("merge operator must follow fork or switch"))
		}
		var exprs []sem.SortExpr
		for _, e := range o.Exprs {
			exprs = append(exprs, t.sortExpr(nil, e, false, inType))
		}
		return append(seq, &sem.MergeOp{Node: o, Exprs: exprs}), inType
	case *ast.UnnestOp:
		e, typ := t.expr(o.Expr, inType)
		t.enterScope()
		defer t.exitScope()
		typ = t.checker.unnest(o.Expr, typ)
		var body sem.Seq
		if o.Body != nil {
			body, typ = t.seq(o.Body, typ)
		}
		return append(seq, &sem.UnnestOp{
			Node: o,
			Expr: e,
			Body: body,
		}), typ
	case *ast.ShapesOp:
		e := sem.Expr(sem.NewThis(o, nil))
		if o.Expr != nil {
			e, _ = t.expr(o.Expr, inType)
		}
		seq = append(seq, &sem.FilterOp{
			Node: o,
			Expr: sem.NewUnaryExpr(o, "!", &sem.IsNullExpr{Node: o, Expr: e}),
		})
		seq = append(seq, &sem.AggregateOp{
			Node: o,
			Aggs: []sem.Assignment{
				{
					Node: o,
					LHS:  sem.NewThis(o, field.NewChain("sample")),
					RHS:  &sem.AggFunc{Node: o, Name: "any", Expr: e},
				},
			},
			Keys: []sem.Assignment{
				{
					Node: o,
					LHS:  sem.NewThis(o, field.NewChain("shape")),
					RHS:  sem.NewCall(o, "typeof", []sem.Expr{e}),
				},
			},
		})
		return append(seq, sem.NewValues(o, sem.NewThis(o, field.NewChain("sample")))), t.checker.unknown
	case *ast.AssertOp:
		cond, _ := t.expr(o.Expr, inType)
		// 'assert EXPR' is equivalent to
		// 'values EXPR ? this : error({message: "assertion failed", "expr": EXPR_text, "on": this}'
		// where EXPR_text is the literal text of EXPR.
		fields := []super.Field{
			super.NewField("message", super.TypeString),
			super.NewField("expr", super.TypeString),
			super.NewField("on", inType),
		}
		errType := t.sctx.LookupTypeError(t.sctx.MustLookupTypeRecord(fields))
		outType := t.checker.fuse([]super.Type{inType, errType})
		return append(seq, sem.NewValues(o,
			&sem.CondExpr{
				Node: o.Expr,
				Cond: cond,
				Then: sem.NewThis(o, nil),
				Else: sem.NewCall(
					o.Expr,
					"error",
					[]sem.Expr{&sem.RecordExpr{
						Node: o.Expr,
						Elems: []sem.RecordElem{
							&sem.FieldElem{
								Node:  o.Expr,
								Name:  "message",
								Value: sem.NewLiteral(o, super.NewString("assertion failed"), t.defs),
							},
							&sem.FieldElem{
								Node:  o.Expr,
								Name:  "expr",
								Value: sem.NewLiteral(o, super.NewString(o.Text), t.defs),
							},
							&sem.FieldElem{
								Node:  o.Expr,
								Name:  "on",
								Value: sem.NewThis(o.Expr, nil),
							},
						},
					}},
				),
			})), outType
	case *ast.ValuesOp:
		exprs, types := t.exprs(o.Exprs, inType)
		return append(seq, sem.NewValues(o, exprs...)), t.checker.fuse(types)
	case *ast.LoadOp:
		if !t.env.IsAttached() {
			t.error(o, errors.New("load operator cannot be used without an attached database"))
			return sem.Seq{badOp}, badType
		}
		poolID, err := dbid.ParseID(o.Pool.Text)
		if err != nil {
			poolID, err = t.env.PoolID(t.ctx, o.Pool.Text)
			if err != nil {
				t.error(o, err)
				return append(seq, badOp), badType
			}
		}
		opArgs := t.opArgs(o.Args, "commit", "author", "message", "meta")
		branch, _ := t.textArg(opArgs, "commit")
		author, _ := t.textArg(opArgs, "author")
		message, _ := t.textArg(opArgs, "message")
		meta, _ := t.textArg(opArgs, "meta")
		return append(seq, &sem.LoadOp{
			Node:    o,
			Pool:    poolID,
			Branch:  branch,
			Author:  author,
			Message: message,
			Meta:    meta,
		}), t.checker.unknown
	case *ast.OutputOp:
		return append(seq, &sem.OutputOp{Node: o, Name: o.Name.Name}), t.checker.unknown
	}
	panic(o)
}

func checkPutFields(fields field.List) error {
	for i, f := range fields {
		if f.IsEmpty() {
			return fmt.Errorf("left-hand side cannot be 'this' (use 'values' operator)")
		}
		for _, c := range fields[i+1:] {
			if f.Equal(c) {
				return fmt.Errorf("multiple assignments to %s", f)
			}
			if c.HasStrictPrefix(f) {
				return fmt.Errorf("conflicting nested assignments to %s and %s", f, c)
			}
			if f.HasStrictPrefix(c) {
				return fmt.Errorf("conflicting nested assignments to %s and %s", c, f)
			}
		}
	}
	return nil
}

func (t *translator) forkOp(op *ast.ForkOp, seq sem.Seq, inType super.Type) (sem.Seq, []super.Type) {
	var paths []sem.Seq
	var types []super.Type
	for _, seq := range op.Paths {
		seq, typ := t.seq(seq, inType)
		paths = append(paths, seq)
		types = append(types, typ)
	}
	return append(seq, &sem.ForkOp{Paths: paths}), types
}

func (t *translator) joinOp(op *ast.JoinOp, parent sem.Seq, inTypes []super.Type) (sem.Seq, super.Type) {
	var subquery sem.Seq
	var rightType super.Type
	leftType := inTypes[0]
	if op.RightInput != nil {
		// With a subquery, we need exactly one parent.
		if len(inTypes) != 1 {
			t.error(op, errors.New("join with subquery requires a single pipe parent"))
			return append(parent, badOp), badType
		}
		subquery, rightType = t.seq(op.RightInput, super.TypeNull)
	} else {
		// When there's no subquery, we need two parents.
		if len(inTypes) != 2 {
			t.error(op, errors.New("join without subquery requires two pipe parents"))
			return append(parent, badOp), badType
		}
		rightType = inTypes[1]
	}
	leftAlias, rightAlias := "left", "right"
	if op.Alias != nil {
		leftAlias = op.Alias.Left.Name
		rightAlias = op.Alias.Right.Name
	}
	if leftAlias == rightAlias {
		t.error(op.Alias, errors.New("left and right join aliases cannot be the same"))
		return append(parent, badOp), t.checker.unknown
	}
	typ := t.sctx.MustLookupTypeRecord([]super.Field{
		super.NewField(leftAlias, leftType),
		super.NewField(rightAlias, rightType),
	})
	var cond sem.Expr
	if op.Cond != nil {
		cond = t.pipeJoinCond(op.Cond, leftAlias, rightAlias, typ)
	}
	style := op.Style
	if style == "" {
		style = "inner"
	}
	join := &sem.JoinOp{
		Node:       op,
		Style:      style,
		LeftAlias:  leftAlias,
		RightAlias: rightAlias,
		Cond:       cond,
	}
	if subquery == nil {
		return append(parent, join), typ
	}
	if len(parent) == 0 {
		parent = append(parent, &sem.PassOp{Node: join})
	}
	fork := &sem.ForkOp{
		Paths: []sem.Seq{parent, subquery},
	}
	return sem.Seq{fork, join}, typ
}

func (t *translator) pipeJoinCond(cond ast.JoinCond, leftAlias, rightAlias string, inType super.Type) sem.Expr {
	switch cond := cond.(type) {
	case *ast.JoinOnCond:
		e, typ := t.expr(cond.Expr, inType)
		t.checker.boolean(cond.Expr, typ)
		// hack: e is wrapped in []sem.Expr to work around CanSet() model in WalkT
		dag.WalkT(reflect.ValueOf([]sem.Expr{e}), func(e *sem.ThisExpr) *sem.ThisExpr {
			if len(e.Chain) == 0 {
				t.error(cond.Expr, errors.New(`join expression cannot refer to "this"`))
			}
			return e
		})
		return e
	case *ast.JoinUsingCond:
		var exprs []sem.Expr
		for _, id := range cond.Fields {
			lhs := sem.NewThis(id, field.NewChain(leftAlias, id.Name))
			rhs := sem.NewThis(id, field.NewChain(rightAlias, id.Name))
			exprs = append(exprs, sem.NewBinaryExpr(id, "==", lhs, rhs))
		}
		return andUsingExprs(cond, exprs)
	default:
		panic(cond)
	}
}

func (t *translator) switchOp(op *ast.SwitchOp, seq sem.Seq, inType super.Type) (sem.Seq, []super.Type) {
	var types []super.Type
	var expr sem.Expr
	if op.Expr != nil {
		expr, _ = t.expr(op.Expr, inType)
	}
	var cases []sem.Case
	for _, c := range op.Cases {
		var e sem.Expr
		if c.Expr != nil {
			e, _ = t.expr(c.Expr, inType)
		} else if op.Expr == nil {
			e = sem.NewLiteral(op, super.True, t.defs)
		}
		path, typ := t.seq(c.Path, inType)
		types = append(types, typ)
		cases = append(cases, sem.Case{Expr: e, Path: path})
	}
	return append(seq, &sem.SwitchOp{
		Node:  op,
		Expr:  expr,
		Cases: cases,
	}), types
}

func (t *translator) singletonAgg(assignment *ast.Assignment, seq sem.Seq, inType super.Type) (sem.Seq, super.Type) {
	if assignment.LHS != nil {
		return nil, nil
	}
	out, path := t.assignment(assignment, inType)
	this, ok := out.LHS.(*sem.ThisExpr)
	if !ok || len(this.Chain) != 1 {
		return nil, nil
	}
	// The return type is simply pulled out of the path since the
	// single column of the record is emitted, e.g., a singleton
	// count() yields an int64 not a {count:int64}.
	return append(seq,
		&sem.AggregateOp{
			Node: assignment,
			Aggs: []sem.Assignment{out},
		},
		sem.NewValues(assignment, this),
	), path.typ
}

func (t *translator) singletonKey(agg ast.Assignment, seq sem.Seq, inType super.Type) (sem.Seq, super.Type) {
	if agg.LHS != nil {
		return nil, nil
	}
	out, path := t.assignment(&agg, inType)
	this, ok := out.LHS.(*sem.ThisExpr)
	if !ok || len(this.Chain) != 1 {
		return nil, nil
	}
	return append(seq,
		&sem.AggregateOp{
			Node: out.Node,
			Keys: []sem.Assignment{out},
		},
		sem.NewValues(out.Node, this),
	), path.typ
}

// semDecls enters a block of declarations into the current scope.  We do late binding
// of symbols to sem-tree entities so that the order of definition doesn't matter.
// For type declarations, we gather them all up into a scoped block and analyze
// them together so that mutually recursive types all know about each other.
func (t *translator) decls(decls []ast.Decl) {
	var typeDecls []*ast.TypeDecl
	for _, d := range decls {
		switch d := d.(type) {
		case *ast.ConstDecl:
			t.constDecl(d)
		case *ast.FuncDecl:
			t.funcDecl(d)
		case *ast.OpDecl:
			t.opDecl(d)
		case *ast.PragmaDecl:
			t.pragmaDecl(d)
		case *ast.QueryDecl:
			t.queryDecl(d)
		case *ast.TypeDecl:
			typeDecls = append(typeDecls, d)
		default:
			panic(d)
		}
	}
	if len(typeDecls) != 0 {
		t.typeDecls(typeDecls)
	}
}

type constDecl struct {
	decl    *ast.ConstDecl
	expr    sem.Expr
	scope   *Scope
	pending bool
}

func (c *constDecl) resolve(t *translator) sem.Expr {
	if c.expr == nil {
		if !c.pending {
			c.pending = true
			save := t.scope
			t.scope = NewScope(c.scope)
			defer func() {
				c.pending = false
				t.scope = save
			}()
			e, _ := t.expr(c.decl.Expr, super.TypeNull)
			c.expr = t.mustEvalConst(e)
		} else {
			t.error(c.decl.Name, fmt.Errorf("const %q involved in cyclic dependency", c.decl.Name.Name))
			c.expr = badExpr
		}
	}
	return c.expr
}

func (t *translator) constDecl(c *ast.ConstDecl) {
	decl := &constDecl{
		decl:  c,
		scope: t.scope,
	}
	if err := t.scope.BindSymbol(c.Name.Name, decl); err != nil {
		t.error(c.Name, err)
	}
}

type queryDecl struct {
	decl    *ast.QueryDecl
	body    sem.Seq
	typ     super.Type
	scope   *Scope
	pending bool
}

func (q *queryDecl) resolve(t *translator) (sem.Seq, super.Type) {
	if q.body == nil {
		if !q.pending {
			save := t.scope
			q.pending = true
			t.scope = NewScope(q.scope)
			defer func() {
				q.pending = false
				t.scope = save
			}()
			q.body, q.typ = t.seq(q.decl.Body, super.TypeNull)
		} else {
			t.error(q.decl.Name, fmt.Errorf("named query %q involved in cyclic dependency", q.decl.Name.Name))
			q.body = sem.Seq{badOp}
		}
	}
	return q.body, q.typ
}

func (t *translator) queryDecl(q *ast.QueryDecl) {
	decl := &queryDecl{
		decl:  q,
		scope: t.scope,
	}
	if err := t.scope.BindSymbol(q.Name.Name, decl); err != nil {
		t.error(q.Name, err)
	}
}

func (t *translator) typeDecls(in []*ast.TypeDecl) {
	// First check for name collisions within the scope since
	// BindTypeScope requires unique names to avoid deadlocks.
	names := make(map[string]struct{})
	var decls []*ast.TypeDecl
	for _, d := range in {
		name := d.Name.Name
		if _, ok := names[name]; ok {
			t.error(d.Name, fmt.Errorf("type %q already exists", name))
			continue
		}
		names[name] = struct{}{}
		decls = append(decls, d)
	}
	ids, errs := t.types.BindTypeScope(decls)
	if errs != nil {
		for k, err := range errs {
			if err != nil {
				t.error(decls[k].Type, err)
			}
		}
		return
	}
	for k, d := range decls {
		typeRef := &sem.TypeExpr{
			Node: d.Type,
			ID:   ids[k],
		}
		if err := t.scope.BindSymbol(d.Name.Name, typeRef); err != nil {
			t.error(d.Name, err)
		}
	}
}

func (t *translator) funcDecl(d *ast.FuncDecl) {
	funcDecl := t.resolver.newFuncDecl(d.Name.Name, d.Lambda, t.scope)
	if err := t.scope.BindSymbol(d.Name.Name, funcDecl); err != nil {
		t.error(d.Name, err)
	}
}

func (t *translator) opDecl(d *ast.OpDecl) {
	m := make(map[string]bool)
	for _, formal := range d.Params {
		if m[formal.Name] {
			t.error(formal, fmt.Errorf("duplicate parameter %q", formal.Name))
			t.scope.BindSymbol(formal.Name, &opDecl{bad: true})
			return
		}
		m[formal.Name] = true
	}
	if err := t.scope.BindSymbol(d.Name.Name, &opDecl{ast: d, scope: t.scope}); err != nil {
		t.error(d, err)
	}
}

func (t *translator) pragmaDecl(d *ast.PragmaDecl) {
	name := d.Name.Name
	if _, ok := t.scope.pragmas[name]; ok {
		t.error(d.Name, fmt.Errorf("%q redefined", name))
		return
	}
	expr := d.Expr
	if expr == nil {
		expr = &ast.Primitive{
			Kind: "Primitive",
			Type: "bool",
			Text: "true",
			Loc:  d.Name.Loc,
		}
	}
	switch name {
	case "index_base":
		if v := t.mustEvalPositiveInteger(expr); v <= 1 {
			t.scope.pragmas["index_base"] = v
		} else {
			t.error(d.Name, errors.New("index_base must be 0 or 1"))
		}
	case "pg":
		if v, ok := t.mustEvalBool(expr); ok {
			t.scope.pragmas["pg"] = v
		}
	default:
		t.error(d.Name, fmt.Errorf("unknown pragma %q", name))
	}
}

func (t *translator) assignmentOp(p *ast.AssignmentOp, inType super.Type) (sem.Op, super.Type) {
	var aggs, puts []sem.Assignment
	var paths []pathType
	for _, astAssign := range p.Assignments {
		// Parition assignments into agg vs. puts.
		assign, path := t.assignment(&astAssign, inType)
		if _, ok := assign.RHS.(*sem.AggFunc); ok {
			if _, ok := assign.LHS.(*sem.ThisExpr); !ok {
				t.error(astAssign.LHS, errors.New("aggregate output field must be static"))
			}
			aggs = append(aggs, assign)
		} else {
			puts = append(puts, assign)
		}
		paths = append(paths, path)
	}
	if len(puts) > 0 && len(aggs) > 0 {
		t.error(p, errors.New("mix of aggregations and non-aggregations in assignment list"))
		return badOp, badType
	}
	if len(puts) > 0 {
		return &sem.PutOp{
			Node: p,
			Args: puts,
		}, t.checker.putPaths(inType, paths)
	}
	return &sem.AggregateOp{
		Node: p,
		Aggs: aggs,
	}, t.checker.pathsToType(paths)
}

func (t *translator) checkStaticAssignment(asts []ast.Assignment, assignments []sem.Assignment) bool {
	for k, assign := range assignments {
		if _, ok := assign.LHS.(*sem.BadExpr); ok {
			continue
		}
		if _, ok := assign.LHS.(*sem.ThisExpr); !ok {
			t.error(asts[k].LHS, errors.New("output field must be static"))
			return true
		}
	}
	return false
}

func (t *translator) exprOp(e ast.Expr, seq sem.Seq, inType super.Type) (sem.Seq, super.Type) {
	if call, ok := e.(*ast.CallExpr); ok {
		if seq, typ := t.maybeCallShortcut(call, seq, inType); seq != nil {
			return seq, typ
		}
	} else if agg, ok := e.(*ast.AggFuncExpr); ok {
		return t.aggFuncShortcut(agg, seq, inType)
	}
	// For stand-alone identifiers with no arguments, see if it's a user op
	// or a named query.
	if id, ok := e.(*ast.IDExpr); ok {
		if decl, err := t.scope.lookupOp(id.Name); err == nil {
			op, typ := t.userOp(id.Loc, decl, nil, inType)
			return append(seq, op...), typ
		}
		if querySeq, typ := t.scope.lookupQuery(t, id.Name); querySeq != nil {
			return append(seq, querySeq...), typ
		}
	}
	out, typ := t.expr(e, inType)
	if t.isBool(out) {
		return append(seq, &sem.FilterOp{Node: e, Expr: out}), inType
	}
	return append(seq, sem.NewValues(e, out)), typ
}

func (t *translator) isBool(e sem.Expr) bool {
	switch e := e.(type) {
	case *sem.PrimitiveExpr:
		return e.Value == "true" || e.Value == "false"
	case *sem.UnaryExpr:
		return t.isBool(e.Operand)
	case *sem.BinaryExpr:
		switch e.Op {
		case "and", "or", "in", "==", "!=", "<", "<=", ">", ">=":
			return true
		default:
			return false
		}
	case *sem.CondExpr:
		return t.isBool(e.Then) && t.isBool(e.Else)
	case *sem.CallExpr:
		if funcDef, ok := t.resolver.funcs[e.Tag]; ok {
			return t.isBool(funcDef.body)
		}
		if e.Tag == "cast" {
			if len(e.Args) != 2 {
				return false
			}
			if typval, ok := e.Args[1].(*sem.PrimitiveExpr); ok {
				return typval.Value == "bool"
			}
			return false
		}
		return function.HasBoolResult(e.Tag)
	case *sem.IsNullExpr:
		return true
	case *sem.SearchTermExpr, *sem.RegexpMatchExpr, *sem.RegexpSearchExpr:
		return true
	default:
		return false
	}
}

func (t *translator) maybeCallShortcut(call *ast.CallExpr, seq sem.Seq, inType super.Type) (sem.Seq, super.Type) {
	f, ok := call.Func.(*ast.FuncNameExpr)
	if !ok {
		return nil, nil
	}
	name := f.Name
	if agg, typ := t.maybeConvertAgg(call, inType); agg != nil {
		aggregate := &sem.AggregateOp{
			Node: call,
			Aggs: []sem.Assignment{
				{
					Node: call,
					LHS:  sem.NewThis(f, field.NewChain(name)),
					RHS:  agg,
				},
			},
		}
		values := sem.NewValues(call, sem.NewThis(call, field.NewChain(name)))
		return append(append(seq, aggregate), values), typ
	}
	if !function.HasBoolResult(strings.ToLower(name)) {
		return nil, nil
	}
	expr, _ := t.semCall(call, inType)
	// At some point, maybe we use this instead of HasBoolResult?
	//t.checker.boolean(call, typ)
	return append(seq, &sem.FilterOp{Node: call, Expr: expr}), inType
}

func (t *translator) aggFuncShortcut(agg *ast.AggFuncExpr, seq sem.Seq, inType super.Type) (sem.Seq, super.Type) {
	name := agg.Name
	aggFunc, typ := t.aggFunc(agg, name, agg.Expr, agg.Filter, agg.Distinct, inType)
	aggregate := &sem.AggregateOp{
		Node: agg,
		Aggs: []sem.Assignment{
			{
				Node: aggFunc,
				LHS:  sem.NewThis(agg, field.NewChain(name)),
				RHS:  aggFunc,
			},
		},
	}
	values := sem.NewValues(agg, sem.NewThis(agg, field.NewChain(name)))
	return append(seq, aggregate, values), typ
}

func (t *translator) callOp(call *ast.CallOp, seq sem.Seq, inType super.Type) (sem.Seq, super.Type) {
	decl, err := t.scope.lookupOp(call.Name.Name)
	if err != nil {
		t.error(call, err)
		return sem.Seq{badOp}, badType
	}
	if decl == nil {
		t.error(call, fmt.Errorf("no such user operator: %q", call.Name.Name))
		return sem.Seq{badOp}, badType
	}
	if decl.bad {
		return sem.Seq{badOp}, badType
	}
	ops, typ := t.userOp(call.Loc, decl, call.Args, inType)
	return append(seq, ops...), typ
}

type thunk struct {
	name  string
	expr  ast.Expr
	scope *Scope
}

func (t *translator) resolveThunk(th thunk, inType super.Type) (sem.Expr, super.Type) {
	save := t.scope
	t.scope = th.scope
	defer func() {
		t.scope = save
	}()
	return t.expr(th.expr, inType)
}

func (t *translator) userOp(loc ast.Loc, decl *opDecl, args []ast.Expr, inType super.Type) (sem.Seq, super.Type) {
	// We've found a user op bound to the name being invoked, so we pull out the
	// AST elements that were stashed from the definition of the user op and subsitute
	// them into the call site here.  This is essentially a thunk... each use of the
	// user op is compiled into the context in which it appears and all the references
	// in that expression are bound appropriately with respect to this context.
	params := decl.ast.Params
	if len(params) != len(args) {
		t.error(loc, fmt.Errorf("%d arg%s provided when operator expects %d arg%s", len(args), plural.Slice(args, "s"), len(params), plural.Slice(params, "s")))
		return sem.Seq{badOp}, badType
	}
	// https://en.wikipedia.org/wiki/42_(number)
	if t.opCnt[decl.ast] >= 42 {
		t.error(loc, opCycleError(append(t.opStack, "etc...")))
		return sem.Seq{badOp}, badType
	}
	t.opCnt[decl.ast]++
	var opStackChanged bool
	if len(t.opStack) < 4 {
		opStackChanged = true
		t.opStack = append(t.opStack, decl.ast.Name.Name)
	}
	oldscope := t.scope
	t.scope = NewScope(decl.scope)
	defer func() {
		t.opCnt[decl.ast]--
		t.scope = oldscope
		if opStackChanged {
			t.opStack = t.opStack[:len(t.opStack)-1]
		}
	}()
	for i, param := range params {
		if err := t.scope.BindSymbol(param.Name, thunk{param.Name, args[i], oldscope}); err != nil {
			t.error(loc, err)
			return sem.Seq{badOp}, badType
		}
	}
	return t.seq(decl.ast.Body, inType)
}

func (t *translator) opArgs(args []ast.OpArg, allowed ...string) opArgs {
	guard := make(map[string]struct{})
	for _, s := range allowed {
		guard[s] = struct{}{}
	}
	return t.opArgsAny(args, guard)
}

func (t *translator) opArgsAny(args []ast.OpArg, allowed map[string]struct{}) opArgs {
	opArgs := make(opArgs)
	for _, arg := range args {
		switch arg := arg.(type) {
		case *ast.ArgText:
			key := strings.ToLower(arg.Key)
			if _, ok := opArgs[key]; ok {
				t.error(arg, fmt.Errorf("duplicate argument %q", arg.Key))
				continue
			}
			if _, ok := allowed[key]; !ok {
				t.error(arg, fmt.Errorf("unknown argument %q", arg.Key))
				continue
			}
			opArgs[key] = arg
		case *ast.ArgExpr:
			key := strings.ToLower(arg.Key)
			if _, ok := opArgs[key]; ok {
				t.error(arg, fmt.Errorf("duplicate argument %q", arg.Key))
				continue
			}
			if _, ok := allowed[key]; !ok {
				t.error(arg, fmt.Errorf("unknown argument %q", arg.Key))
				continue
			}
			e, _ := t.expr(arg.Value, super.TypeNull)
			opArgs[key] = argExpr{arg: arg, expr: e}
		default:
			panic(fmt.Sprintf("unknown arg type %T", arg))
		}
	}
	return opArgs
}

type opArgs map[string]any

type argExpr struct {
	arg  *ast.ArgExpr
	expr sem.Expr
}

func (t *translator) textArg(o opArgs, key string) (string, ast.Loc) {
	if v, ok := o[key]; ok {
		if arg, ok := v.(*ast.ArgText); ok {
			return arg.Value.Text, arg.Loc
		}
		// The PEG parser currently doesn't allow this but this may change.
		t.error(v.(*ast.ArgExpr).Loc, fmt.Errorf("argument %q must be plain text", key))
	}
	return "", ast.Loc{}
}

func (t *translator) exprArg(o opArgs, key string) (sem.Expr, ast.Loc) {
	if v, ok := o[key]; ok {
		if arg, ok := v.(argExpr); ok {
			return arg.expr, arg.arg.Loc
		}
		// The PEG parser currently doesn't allow this but this may change.
		t.error(v.(*ast.ArgText).Loc, fmt.Errorf("argument %q must be expression", key))
	}
	return nil, ast.Loc{}
}

func (t *translator) mustEvalString(e sem.Expr) (field string, ok bool) {
	val, ok := t.mustEval(e)
	if ok && !val.IsError() && super.TypeUnder(val.Type()) == super.TypeString {
		return string(val.Bytes()), true
	}
	return "", false
}

func (t *translator) mustEvalBool(in ast.Expr) (bool, bool) {
	e, _ := t.expr(in, super.TypeNull)
	val, ok := t.mustEval(e)
	if ok {
		if super.TypeUnder(val.Type()) != super.TypeBool {
			t.error(in, errors.New("expected type bool"))
			return false, false
		}
		return val.AsBool(), true
	}
	return false, false
}

func (t *translator) maybeEvalString(e sem.Expr) (field string, ok bool) {
	val, ok := t.maybeEval(e)
	if ok && super.TypeUnder(val.Type()) == super.TypeString {
		return string(val.Bytes()), true
	}
	return "", false
}

func (t *translator) mustEvalPositiveInteger(ae ast.Expr) int {
	e, _ := t.expr(ae, super.TypeNull)
	val, ok := t.mustEval(e)
	if !ok {
		return 0
	}
	if !super.IsInteger(val.Type().ID()) || val.IsNull() {
		t.error(ae, errors.New("expected integer"))
		return 0
	}
	v := int(val.AsInt())
	if v < 0 {
		t.error(ae, errors.New("expected positive integer"))
		return 0
	}
	return v
}

func (t *translator) mustEvalConst(e sem.Expr) sem.Expr {
	val, ok := t.mustEval(e)
	if !ok {
		return badExpr
	}
	return sem.NewLiteral(e, val, t.defs)
}

// mustEval leaves errors on the reporter and returns a bool as to whether
// the eval was successful
func (t *translator) mustEval(e sem.Expr) (super.Value, bool) {
	// We're in the middle of a semantic analysis but want to compile the
	// translated expression.  Operator thunks have been unfolded but
	// funcs haven't been resolved, but that's ok because we'll copy all the state
	// we need into a new instance of a translator (using the evaulator)
	// and we'll compile this all the way to a DAG and rungen it.  This is pretty
	// general because we need to handle things like subqueries that call
	// operator sequences that result in a constant value.
	return newEvaluator(t, t.resolver.funcs).mustEval(t.sctx, e)
}

// maybeEVal leaves no errors behind and simply returns a value and bool
// indicating if the eval was successful
func (t *translator) maybeEval(e sem.Expr) (super.Value, bool) {
	return newEvaluator(t, t.resolver.funcs).maybeEval(t.sctx, e)
}
