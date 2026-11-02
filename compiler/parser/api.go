package parser

import (
	"errors"
	"slices"

	"github.com/superdb/super/compiler/ast"
	"github.com/superdb/super/compiler/srcfiles"
)

type AST struct {
	seq   ast.Seq
	files *srcfiles.List
}

func (a *AST) Parsed() ast.Seq {
	return a.seq
}

func (a *AST) Copy() ast.Seq {
	return ast.CopySeq(a.seq)
}

// Script returns the Script of a program that is a script and nil otherwise.
func (a *AST) Script() *ast.Script {
	if len(a.seq) == 1 {
		if s, ok := a.seq[0].(*ast.Script); ok {
			return s
		}
	}
	return nil
}

// WithSeq returns an AST for seq that shares a's source files so that
// errors in seq (e.g., a statement of a's script) are reported in terms of
// a's source text.
func (a *AST) WithSeq(seq ast.Seq) *AST {
	return &AST{seq: seq, files: a.files}
}

func (a *AST) Files() *srcfiles.List {
	return a.files
}

func (a *AST) ConvertToDeleteWhere(table, branch string) error {
	if len(a.seq) == 0 {
		return errors.New("internal error: AST seq cannot be empty")
	}
	a.seq.Prepend(&ast.Delete{
		Kind:   "Delete",
		Table:  table,
		Branch: branch,
	})
	return nil
}

func (a *AST) PrependFileScan(paths []string) []string {
	var args []string
	if k := slices.Index(paths, "--"); k >= 0 {
		args = paths[k+1:]
		paths = paths[:k]
	}
	if len(paths) > 0 {
		a.seq.Prepend(&ast.FileScan{
			Kind:  "FileScan",
			Paths: paths,
		})
	}
	if args != nil {
		a.BindArgs(args)
	}
	return paths
}

// BindArgs declares the constant args as the array of strings in args for
// the query or, if the query is a script, for the script's statements.
func (a *AST) BindArgs(args []string) {
	decl := &ast.ConstDecl{
		Kind: "ConstDecl",
		Name: &ast.ID{Name: "args"},
		Expr: stringArray(args),
	}
	if s := a.Script(); s != nil {
		s.Decls = append([]ast.Decl{decl}, s.Decls...)
		return
	}
	a.seq = []ast.Op{
		&ast.ScopeOp{
			Kind:  "ScopeOp",
			Decls: []ast.Decl{decl},
			Body:  a.seq,
		},
	}
}

func stringArray(in []string) ast.Expr {
	var elems []ast.ArrayElem
	for _, s := range in {
		e := &ast.Primitive{
			Kind: "Primitive",
			Type: "string",
			Text: s,
		}
		elems = append(elems, &ast.ExprElem{Kind: "ExprElem", Expr: e})
	}
	return &ast.ArrayExpr{
		Kind:  "ArrayExpr",
		Elems: elems,
	}
}

// ParseText parses a query text in string form.
func ParseText(text string) (*AST, error) {
	return ParseFiles(srcfiles.Plain(text))
}

// ParseFiles parses a query text comprised of a mixture of plain text
// and source files, tracking file names and line numbers for error reporting.
func ParseFiles(inputs []srcfiles.Input) (*AST, error) {
	files, err := srcfiles.Concat(inputs)
	if err != nil {
		return nil, err
	}
	if files.Text == "" {
		return &AST{files: files}, nil
	}
	p, err := Parse("", []byte(files.Text), Recover(false))
	if err != nil {
		if err := convertParseErrs(err, files); err != nil {
			return nil, err
		}
		return nil, files.Error()
	}
	return &AST{sliceOf[ast.Op](p), files}, nil
}

func convertParseErrs(err error, files *srcfiles.List) error {
	errs, ok := err.(errList)
	if !ok {
		return err
	}
	for _, e := range errs {
		pe, ok := e.(*parserError)
		if !ok {
			return err
		}
		files.AddError("parse error", pe.pos.offset, -1)
	}
	return nil
}
