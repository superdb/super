package sfmt

import (
	"github.com/superdb/super/compiler/ast"
	"github.com/superdb/super/pkg/field"
	"github.com/superdb/super/sup"
)

type shared struct {
	formatter
}

// XXX this needs to change when we use the SUP values from the ast
func (s *shared) literal(e ast.Primitive) {
	switch e.Type {
	case "missing":
		s.write("none")
	case "null":
		s.write(e.Type)
	case "string", "error":
		s.write("\"%s\"", e.Text)
	case "regexp":
		s.write("/%s/", e.Text)
	default:
		//XXX need decorators for non-implied
		s.write("%s", e.Text)
	}
}

func (s *shared) fieldchain(chain field.Chain) {
	if len(chain) == 0 {
		s.write("this")
		return
	}
	for k, elem := range chain {
		if elem.Noneish || elem.Nullish {
			if k == 0 {
				s.write("this")
			}
			if elem.Noneish {
				s.write("?")
			} else {
				s.write("!")
			}
			if k == 0 {
				s.write(".")
			}
		}
		if k != 0 {
			s.write(".")
		}
		if sup.IsIdentifier(elem.ID) {
			s.write(elem.ID)
		} else {
			s.write("`%s`", elem.ID)
		}
	}
}

func (s *shared) typ(t ast.Type) {
	switch t := t.(type) {
	case *ast.TypePrimitive:
		s.write(t.Name)
	case *ast.TypeRecord:
		s.write("{")
		s.typeFields(t.Fields)
		s.write("}")
	case *ast.TypeArray:
		s.write("[")
		s.typ(t.Type)
		s.write("]")
	case *ast.TypeSet:
		s.write("set[")
		s.typ(t.Type)
		s.write("]")
	case *ast.TypeUnion:
		s.write("(")
		s.types(t.Types, "|")
		s.write(")")
	case *ast.TypeEnum:
		//XXX need to figure out syntax for enum literal which may
		// be different than SUP, requiring some ast adjustments.
		s.write("TBD:ENUM")
	case *ast.TypeMap:
		s.write("map{")
		s.typ(t.KeyType)
		s.write(":")
		s.typ(t.ValType)
		s.write("}")
	case *ast.TypeRef:
		s.write(t.Name)
	case *ast.TypeError:
		s.write("error(")
		s.typ(t.Type)
		s.write(")")
	case *ast.DateTypeHack:
		s.write("date")
	}
}

func (s *shared) typeFields(fields []ast.TypeField) {
	for k, f := range fields {
		if k != 0 {
			s.write(",")
		}
		s.write("%s:", sup.QuotedName(f.Name))
		s.typ(f.Type)
	}
}

func (s *shared) types(types []ast.Type, sep string) {
	for k, t := range types {
		if k != 0 {
			s.write(sep)
		}
		s.typ(t)
	}
}
