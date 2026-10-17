package vector

import (
	"fmt"
)

type Kind int
type Form int

const (
	KindInvalid = 0
	KindInt     = 1
	KindUint    = 2
	KindFloat   = 3
	KindString  = 4
	KindBytes   = 5
	KindIP      = 6
	KindNet     = 7
	KindType    = 8
	KindNull    = 9
	KindError   = 10
	KindArray   = 11
	KindSet     = 12
	KindMap     = 13
	KindRecord  = 14
	KindBool    = 15
	KindUnion   = 16
	KindEnum    = 17
	KindFusion  = 19
	KindMissing = 20
	KindOption  = 21
	KindNil     = 22
)

const (
	FormFlat  = 0
	FormDict  = 1
	FormView  = 2
	FormConst = 3
)

func KindFromString(v string) Kind {
	switch v {
	case "Int":
		return KindInt
	case "Uint":
		return KindUint
	case "Float":
		return KindFloat
	case "Bytes":
		return KindBytes
	case "String":
		return KindString
	case "TypeValue":
		return KindType
	case "Array":
		return KindArray
	case "Set":
		return KindSet
	case "Map":
		return KindMap
	case "Record":
		return KindRecord
	case "Missing":
		return KindMissing
	case "Option":
		return KindOption
	default:
		return KindInvalid
	}
}

func FormOf(v Any) (Form, bool) {
	switch v.(type) {
	case *Int, *Uint, *Float, *Bytes, *String, *TypeValue: //XXX IP, Net
		return FormFlat, true
	case *Dict:
		return FormDict, true
	case *View:
		return FormView, true
	case *Const:
		return FormConst, true
	default:
		return 0, false
	}
}

func (f Form) String() string {
	switch f {
	case FormFlat:
		return "Flat"
	case FormDict:
		return "Dict"
	case FormView:
		return "View"
	case FormConst:
		return "Const"
	default:
		return fmt.Sprintf("Form-Unknown-%d", f)
	}
}

const (
	CompLT = 0
	CompLE = 1
	CompGT = 2
	CompGE = 3
	CompEQ = 4
	CompNE = 6
)

func CompareOpFromString(op string) int {
	switch op {
	case "<":
		return CompLT
	case "<=":
		return CompLE
	case ">":
		return CompGT
	case ">=":
		return CompGE
	case "==":
		return CompEQ
	case "!=":
		return CompNE
	}
	panic("CompareOpFromString")
}

func CompareOpToString(op int) string {
	switch op {
	case CompLT:
		return "<"
	case CompLE:
		return "<="
	case CompGT:
		return ">"
	case CompGE:
		return ">="
	case CompEQ:
		return "=="
	case CompNE:
		return "!="
	}
	panic("CompareOpToString")
}

const (
	ArithAdd = iota
	ArithSub
	ArithMul
	ArithDiv
	ArithMod
)

func ArithOpFromString(op string) int {
	switch op {
	case "+":
		return ArithAdd
	case "-":
		return ArithSub
	case "*":
		return ArithMul
	case "/":
		return ArithDiv
	case "%":
		return ArithMod
	}
	panic(op)
}

func ArithOpToString(op int) string {
	switch op {
	case ArithAdd:
		return "+"
	case ArithSub:
		return "-"
	case ArithMul:
		return "*"
	case ArithDiv:
		return "/"
	case ArithMod:
		return "%"
	}
	panic(op)
}

func FuncCode(op int, kind Kind, lform, rform Form) int {
	// op:4, kind:3, left:2, right:2
	return int(lform) | int(rform)<<2 | int(kind)<<4 | op<<8
}
