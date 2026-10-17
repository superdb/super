// Package sup provides fundamental interfaces to the SUP data format comprising
// Reader, Writer, Parser, and so forth.  The SUP format includes a type system
// that requries a semantic analysis to parse an input to its structured data
// representation.  To do so, Parser translats a SUP input to an AST, Analyzer
// performs semantic type analysis to turn the AST into a Value, and Builder
// constructs a super.Value from a Value.
package sup

import (
	"slices"
	"strings"

	"github.com/superdb/super"
	"github.com/superdb/super/compiler/ast"
	"github.com/superdb/super/scode"
)

// Implied returns true for primitive types whose type can be inferred
// syntactically from its value and thus never needs a decorator.
func Implied(typ super.Type) bool {
	switch typ := typ.(type) {
	case *super.TypeOfInt64, *super.TypeOfDuration, *super.TypeOfTime, *super.TypeOfFloat64, *super.TypeOfBool, *super.TypeOfBytes, *super.TypeOfString, *super.TypeOfIP, *super.TypeOfNet, *super.TypeOfType, *super.TypeOfNull, *super.TypeOfMissing:
		return true
	case *super.TypeRecord:
		return !slices.ContainsFunc(typ.Fields, func(f super.Field) bool {
			return !Implied(f.Type)
		})
	case *super.TypeArray:
		return Implied(typ.Type)
	case *super.TypeSet:
		return Implied(typ.Type)
	case *super.TypeMap:
		return Implied(typ.KeyType) && Implied(typ.ValType)
	case *super.TypeError:
		return Implied(typ.Type)
	case *super.TypeFusion:
		return true
	}
	return false
}

// SelfDescribing returns true for types whose type name can be entirely derived
// from its typed value, e.g., a record type can be derived from a record value
// because all of the field names and type names are present in the value, but
// an enum type cannot be derived from an enum value because not all the enumerated
// names are present in the value.  In the former case, a decorated typedef can
// use the abbreviated form "(= <name>)", while the letter case, a type def must use
// the longer form "<value> (<name> = (<type>))".
func SelfDescribing(typ super.Type) bool {
	if Implied(typ) {
		return true
	}
	switch typ := typ.(type) {
	case *super.TypeRecord, *super.TypeArray, *super.TypeSet, *super.TypeMap, *super.TypeError:
		return true
	case *super.TypeNamed:
		// A named type that refers to another named type is not self describing
		// because the underlying name must be preserved.
		if super.IsTypeNamed(typ.Type) {
			return false
		}
		return SelfDescribing(typ.Type)
	}
	return false
}

func ParseType(sctx *super.Context, sup string) (super.Type, error) {
	zp := NewParser(strings.NewReader(sup))
	ast, err := zp.parseType()
	if ast == nil || noEOF(err) != nil {
		return nil, err
	}
	return NewAnalyzer(sctx).convertType(ast)
}

func ParseValue(sctx *super.Context, sup string) (super.Value, error) {
	ast, err := NewParser(strings.NewReader(sup)).ParseValue()
	if err != nil {
		return super.Null, err
	}
	val, err := NewAnalyzer(sctx).ConvertValue(ast)
	if err != nil {
		return super.Null, err
	}
	return Build(scode.NewBuilder(), val)
}

func MustParseValue(sctx *super.Context, sup string) super.Value {
	val, err := ParseValue(sctx, sup)
	if err != nil {
		panic(err)
	}
	return val
}

func ParseValueFromAST(sctx *super.Context, ast ast.Value) (super.Value, error) {
	val, err := NewAnalyzer(sctx).ConvertValue(ast)
	if err != nil {
		return super.Null, err
	}
	return Build(scode.NewBuilder(), val)
}

func TranslateType(sctx *super.Context, astType ast.Type) (super.Type, error) {
	return NewAnalyzer(sctx).convertType(astType)
}
