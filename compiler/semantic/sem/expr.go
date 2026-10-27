package sem

import (
	"slices"

	"github.com/superdb/super"
	"github.com/superdb/super/compiler/ast"
	"github.com/superdb/super/pkg/field"
	"github.com/superdb/super/scode"
	"github.com/superdb/super/sup"
)

type Expr interface {
	ast.Node
	exprNode()
}

type (
	AggFunc struct {
		ast.Node
		Name     string // convert to lower case
		Distinct bool
		Expr     Expr
		Filter   Expr
	}
	AggRef struct {
		ast.Node
		Index int
	}
	ArrayExpr struct {
		ast.Node
		Elems []ArrayElem
	}
	BadExpr struct {
		ast.Node
	}
	BinaryExpr struct {
		ast.Node
		Op  string // normalized Op string
		LHS Expr
		RHS Expr
	}
	// An ast.Call that has been resolved to an actual function call in expression
	// context... this now refers to the flattened function table (or a built-in) via tag
	CallExpr struct {
		ast.Node
		Tag  string
		Args []Expr
	}
	CondExpr struct {
		ast.Node
		Cond Expr
		Then Expr
		Else Expr
	}
	DotExpr struct {
		ast.Node
		LHS     Expr
		RHS     string
		Noneish bool
		Nullish bool
	}
	IndexExpr struct {
		ast.Node
		Expr  Expr
		Index Expr
		Base1 bool
	}
	IsNullExpr struct {
		ast.Node
		Expr Expr
	}
	MapCallExpr struct {
		ast.Node
		Expr   Expr
		Lambda *CallExpr
	}
	MapExpr struct {
		ast.Node
		Entries []Entry
	}
	PrimitiveExpr struct {
		ast.Node
		Value string
	}
	RecordExpr struct {
		ast.Node
		Elems []RecordElem
	}
	RegexpMatchExpr struct {
		ast.Node
		Pattern string
		Expr    Expr
	}
	RegexpSearchExpr struct {
		ast.Node
		Pattern string
		Expr    Expr
	}
	SearchTermExpr struct {
		ast.Node
		Text  string
		Value string
		Expr  Expr
	}
	SetExpr struct {
		ast.Node
		Elems []ArrayElem
	}
	SliceExpr struct {
		ast.Node
		Expr  Expr
		From  Expr
		To    Expr
		Base1 bool
	}
	SubqueryExpr struct {
		ast.Node
		Correlated bool
		Array      bool
		Body       Seq
	}
	ThisExpr struct {
		ast.Node
		Chain field.Chain
	}
	TypeExpr struct {
		ast.Node
		ID int
	}
	UnaryExpr struct {
		ast.Node
		Op      string
		Operand Expr
	}
)

// Support structures embedded in Expr nodes

// Entry used by MapExpr
type Entry struct {
	Key   Expr
	Value Expr
}

// The sum type for array, set, and record elements.  There is not an easy way
// of creating a Go pseudo-type

type ArrayElem interface {
	ast.Node
	arrayElemNode()
}

type RecordElem interface {
	ast.Node
	recordElemNode()
}

type (
	FieldElem struct {
		ast.Node
		Name  string
		Value Expr
	}
	SpreadElem struct {
		ast.Node
		Expr Expr
	}
	ExprElem struct {
		ast.Node
		Expr Expr
	}
)

func (*ExprElem) arrayElemNode()    {}
func (*FieldElem) recordElemNode()  {}
func (*SpreadElem) arrayElemNode()  {}
func (*SpreadElem) recordElemNode() {}

func (*AggFunc) exprNode()          {}
func (*AggRef) exprNode()           {}
func (*ArrayExpr) exprNode()        {}
func (*BadExpr) exprNode()          {}
func (*BinaryExpr) exprNode()       {}
func (*CondExpr) exprNode()         {}
func (*CallExpr) exprNode()         {}
func (*DotExpr) exprNode()          {}
func (*IndexExpr) exprNode()        {}
func (*IsNullExpr) exprNode()       {}
func (*MapCallExpr) exprNode()      {}
func (*MapExpr) exprNode()          {}
func (*PrimitiveExpr) exprNode()    {}
func (*RecordExpr) exprNode()       {}
func (*RegexpMatchExpr) exprNode()  {}
func (*RegexpSearchExpr) exprNode() {}
func (*SearchTermExpr) exprNode()   {}
func (*SetExpr) exprNode()          {}
func (*SliceExpr) exprNode()        {}
func (*SubqueryExpr) exprNode()     {}
func (*ThisExpr) exprNode()         {}
func (*TypeExpr) exprNode()         {}
func (*UnaryExpr) exprNode()        {}

// FuncRef is a pseudo-expression that represents a function reference as a value.
// It is not used by the runtime (but could be if we wanted to support this).  Instead,
// the semantic pass uses this to represent lambda-parameterized functions, e.g.,
// functions that are passed to other functions as arguments.  Whenever such values
// appear as function arguments, they are installed in the symbol table as bound to
// the function declaration's ID then each variation of lambda-invoked function is
// compiled to a unique function by the resolver.
type FuncRef struct {
	ast.Node
	ID string
}

func (*FuncRef) exprNode() {}

func NewThis(n ast.Node, chain field.Chain) *ThisExpr {
	return &ThisExpr{Node: n, Chain: chain}
}

func NewBinaryExpr(n ast.Node, op string, lhs, rhs Expr) *BinaryExpr {
	return &BinaryExpr{
		Node: n,
		Op:   op,
		LHS:  lhs,
		RHS:  rhs,
	}
}

func NewUnaryExpr(n ast.Node, op string, operand Expr) *UnaryExpr {
	return &UnaryExpr{
		Node:    n,
		Op:      op,
		Operand: operand,
	}
}

func NewCall(n ast.Node, tag string, args []Expr) *CallExpr {
	return &CallExpr{
		Node: n,
		Tag:  tag,
		Args: args,
	}
}

func NewStructuredError(n ast.Node, message string, on Expr) Expr {
	rec := &RecordExpr{
		Node: n,
		Elems: []RecordElem{
			&FieldElem{
				Name:  "message",
				Value: NewLiteral(n, super.NewString(message), nil),
			},
			&FieldElem{
				Name:  "on",
				Value: on,
			},
		},
	}
	return &CallExpr{
		Node: n,
		Tag:  "error",
		Args: []Expr{rec},
	}
}

func NewStringError(n ast.Node, message string) Expr {
	return NewCall(n, "error", []Expr{NewLiteral(n, super.NewString(message), nil)})
}

func NewLiteral(n ast.Node, val super.Value, defs *super.Context) Expr {
	if val.Type() == super.TypeType {
		typ, err := defs.LookupByValue(val.Bytes())
		if err != nil {
			panic(err)
		}
		return &TypeExpr{
			Node: n,
			ID:   super.TypeID(typ),
		}
	}
	return valueToExpr(n, val.Type(), val.Bytes(), defs)
}

func CopyExpr(e Expr) Expr {
	switch e := e.(type) {
	case nil:
		return nil
	case *AggFunc:
		return &AggFunc{
			Node:     e.Node,
			Name:     e.Name,
			Distinct: e.Distinct,
			Expr:     CopyExpr(e.Expr),
			Filter:   CopyExpr(e.Filter),
		}
	case *AggRef:
		return &AggRef{
			Node:  e.Node,
			Index: e.Index,
		}
	case *ArrayExpr:
		return &ArrayExpr{
			Node:  e.Node,
			Elems: copyArrayElems(e.Elems),
		}
	case *BadExpr:
		return &BadExpr{Node: e.Node}
	case *BinaryExpr:
		return &BinaryExpr{
			Node: e.Node,
			Op:   e.Op,
			LHS:  CopyExpr(e.LHS),
			RHS:  CopyExpr(e.RHS),
		}
	case *CondExpr:
		return &CondExpr{
			Node: e.Node,
			Cond: CopyExpr(e.Cond),
			Then: CopyExpr(e.Then),
			Else: CopyExpr(e.Else),
		}
	case *CallExpr:
		return &CallExpr{
			Node: e.Node,
			Tag:  e.Tag,
			Args: copyExprs(e.Args),
		}
	case *DotExpr:
		return &DotExpr{
			Node:    e.Node,
			LHS:     CopyExpr(e.LHS),
			RHS:     e.RHS,
			Noneish: e.Noneish,
			Nullish: e.Nullish,
		}
	case *IndexExpr:
		return &IndexExpr{
			Node:  e.Node,
			Expr:  CopyExpr(e.Expr),
			Index: CopyExpr(e.Index),
			Base1: e.Base1,
		}
	case *IsNullExpr:
		return &IsNullExpr{
			Node: e.Node,
			Expr: CopyExpr(e.Expr),
		}
	case *MapCallExpr:
		return &MapCallExpr{
			Node:   e.Node,
			Expr:   CopyExpr(e.Expr),
			Lambda: CopyExpr(e.Lambda).(*CallExpr),
		}
	case *MapExpr:
		var entries []Entry
		for _, entry := range e.Entries {
			entries = append(entries, Entry{
				Key:   CopyExpr(entry.Key),
				Value: CopyExpr(entry.Value),
			})
		}
		return &MapExpr{
			Node:    e.Node,
			Entries: entries,
		}
		//XXX can we change primitive expr to not have types?
	case *PrimitiveExpr:
		return &PrimitiveExpr{
			Node:  e.Node,
			Value: e.Value,
		}
	case *RecordExpr:
		var elems []RecordElem
		for _, elem := range e.Elems {
			switch elem := elem.(type) {
			case *FieldElem:
				elems = append(elems, &FieldElem{
					Node:  elem.Node,
					Name:  elem.Name,
					Value: CopyExpr(elem.Value),
				})
			case *SpreadElem:
				elems = append(elems, &SpreadElem{
					Node: elem.Node,
					Expr: CopyExpr(elem.Expr),
				})
			default:
				panic(elem)
			}
		}
		return &RecordExpr{
			Node:  e.Node,
			Elems: elems,
		}
	case *RegexpMatchExpr:
		return &RegexpMatchExpr{
			Node:    e.Node,
			Pattern: e.Pattern,
			Expr:    CopyExpr(e.Expr),
		}
	case *RegexpSearchExpr:
		return &RegexpSearchExpr{
			Node:    e.Node,
			Pattern: e.Pattern,
			Expr:    CopyExpr(e.Expr),
		}
	case *SearchTermExpr:
		return &SearchTermExpr{
			Node:  e.Node,
			Text:  e.Text,
			Value: e.Value,
			Expr:  CopyExpr(e.Expr),
		}
	case *SetExpr:
		return &SetExpr{
			Node:  e.Node,
			Elems: copyArrayElems(e.Elems),
		}
	case *SliceExpr:
		return &SliceExpr{
			Node:  e.Node,
			Expr:  CopyExpr(e.Expr),
			From:  CopyExpr(e.Expr),
			To:    CopyExpr(e.Expr),
			Base1: e.Base1,
		}
	case *SubqueryExpr:
		return &SubqueryExpr{
			Node:       e.Node,
			Correlated: e.Correlated,
			Array:      e.Array,
			Body:       CopySeq(e.Body),
		}
	case *ThisExpr:
		return &ThisExpr{
			Node:  e.Node,
			Chain: slices.Clone(e.Chain),
		}
	case *TypeExpr:
		return &TypeExpr{
			Node: e.Node,
			ID:   e.ID,
		}
	case *UnaryExpr:
		return &UnaryExpr{
			Node:    e.Node,
			Op:      e.Op,
			Operand: CopyExpr(e.Operand),
		}
	default:
		panic(e)
	}
}

func copyExprs(exprs []Expr) []Expr {
	var out []Expr
	for _, e := range exprs {
		out = append(out, CopyExpr(e))
	}
	return out
}

func copyArrayElems(elems []ArrayElem) []ArrayElem {
	var out []ArrayElem
	for _, elem := range elems {
		switch elem := elem.(type) {
		case *ExprElem:
			out = append(out, &ExprElem{
				Node: elem.Node,
				Expr: CopyExpr(elem.Expr),
			})
		case *SpreadElem:
			out = append(out, &SpreadElem{
				Node: elem.Node,
				Expr: CopyExpr(elem.Expr),
			})
		default:
			panic(elem)
		}
	}
	return out
}

func valueToExpr(loc ast.Node, typ super.Type, bytes scode.Bytes, defs *super.Context) Expr {
	if !super.IsOptionType(typ) && super.IsPrimitiveType(typ) {
		if super.TypeUnder(typ) == super.TypeType {
			t, err := defs.LookupByValue(bytes)
			if err != nil {
				panic(err)
			}
			return &TypeExpr{
				Node: loc,
				ID:   super.TypeID(t),
			}
		}
		return &PrimitiveExpr{
			Node:  loc,
			Value: sup.FormatValue(super.NewValue(typ, bytes)),
		}
	}
	switch typ := typ.(type) {
	case *super.TypeNamed:
		_, _ = defs.LookupTypeNamed(typ.Name, typ.Type)
		return NewCast(loc, valueToExpr(loc, typ.Type, bytes, defs), typ, defs)
	case *super.TypeRecord:
		return recordToExpr(loc, typ, bytes, defs)
	case *super.TypeArray:
		return arrayToExpr(loc, typ, bytes, defs)
	case *super.TypeSet:
		return setToExpr(loc, typ, bytes, defs)
	case *super.TypeUnion:
		return unionToExpr(loc, typ, bytes, defs)
	case *super.TypeMap:
		return mapToExpr(loc, typ, bytes, defs)
	case *super.TypeOption:
		return optionToExpr(loc, typ, bytes, defs)
	case *super.TypeError:
		args := []Expr{valueToExpr(loc, typ.Type, bytes, defs)}
		return NewCall(loc, "error", args)
	default:
		panic(typ)
	}
}

func recordToExpr(loc ast.Node, typ *super.TypeRecord, bytes scode.Bytes, defs *super.Context) Expr {
	var elems []RecordElem
	it := bytes.Iter()
	for _, f := range typ.Fields {
		if it.Done() {
			panic(it)
		}
		elems = append(elems, &FieldElem{Node: loc, Name: f.Name, Value: valueToExpr(loc, f.Type, it.Next(), defs)})
	}
	return &RecordExpr{
		Node:  loc,
		Elems: elems,
	}
}

func arrayToExpr(loc ast.Node, typ *super.TypeArray, bytes scode.Bytes, defs *super.Context) Expr {
	var elems []ArrayElem
	inner := super.InnerType(typ)
	for it := bytes.Iter(); !it.Done(); {
		elems = append(elems, &ExprElem{Node: loc, Expr: valueToExpr(loc, inner, it.Next(), defs)})
	}
	return &ArrayExpr{
		Node:  loc,
		Elems: elems,
	}
}

func setToExpr(loc ast.Node, typ *super.TypeSet, bytes scode.Bytes, defs *super.Context) Expr {
	var elems []ArrayElem
	inner := super.InnerType(typ)

	for it := bytes.Iter(); !it.Done(); {
		elems = append(elems, &ExprElem{Node: loc, Expr: valueToExpr(loc, inner, it.Next(), defs)})
	}
	return &SetExpr{
		Node:  loc,
		Elems: elems,
	}
}

func unionToExpr(loc ast.Node, typ *super.TypeUnion, bytes scode.Bytes, defs *super.Context) Expr {
	innerType, innerBytes := typ.Untag(bytes)
	return NewCast(loc, valueToExpr(loc, innerType, innerBytes, defs), typ, defs)
}

func mapToExpr(loc ast.Node, typ *super.TypeMap, bytes scode.Bytes, defs *super.Context) Expr {
	keyType := super.TypeUnder(typ.KeyType)
	valType := super.TypeUnder(typ.ValType)
	var entries []Entry
	for it := bytes.Iter(); !it.Done(); {
		key := valueToExpr(loc, keyType, it.Next(), defs)
		val := valueToExpr(loc, valType, it.Next(), defs)
		entries = append(entries, Entry{Key: key, Value: val})

	}
	return &MapExpr{
		Node:    loc,
		Entries: entries,
	}
}

func optionToExpr(loc ast.Node, typ *super.TypeOption, bytes scode.Bytes, defs *super.Context) Expr {
	someType, someBytes := typ.Decode(bytes)
	if someType == nil {
		return NewCast(loc, &PrimitiveExpr{Node: loc, Value: "none"}, typ, defs)
	}
	return NewCall(loc, "some", []Expr{valueToExpr(loc, someType, someBytes, defs)})
}

func NewCast(loc ast.Node, e Expr, typ super.Type, defs *super.Context) Expr {
	args := []Expr{e, NewLiteral(loc, super.NewTypeValue(typ), defs)}
	return NewCall(loc, "cast", args)
}
