package dag

import (
	"encoding/json"
	"fmt"
	"slices"

	"github.com/superdb/super/order"
	"github.com/superdb/super/pkg/field"
)

type MainExpr struct {
	Types []byte     `json:"types"`
	Funcs []*FuncDef `json:"funcs"`
	Expr  Expr       `json:"expr"`
}

type (
	Expr interface {
		exprNode()
	}
	RecordElem interface {
		recordElemNode()
	}
	VectorElem interface {
		vectorElemNode()
	}
)

// Exprs

type (
	AggExpr struct {
		Kind     string `json:"kind" unpack:""`
		Name     string `json:"name"`
		Distinct bool   `json:"distinct"`
		Expr     Expr   `json:"expr"`
		Filter   Expr   `json:"filter"`
	}
	ArrayExpr struct {
		Kind  string       `json:"kind" unpack:""`
		Elems []VectorElem `json:"elems"`
	}
	// A BadExpr node is a placeholder for an expression containing semantic
	// errors.
	BadExpr struct {
		Kind string `json:"kind" unpack:""`
	}
	BinaryExpr struct {
		Kind string `json:"kind" unpack:""`
		Op   string `json:"op"`
		LHS  Expr   `json:"lhs"`
		RHS  Expr   `json:"rhs"`
	}
	CallExpr struct {
		Kind string `json:"kind" unpack:""`
		Tag  string `json:"tag"`
		Args []Expr `json:"args"`
	}
	CondExpr struct {
		Kind string `json:"kind" unpack:""`
		Cond Expr   `json:"cond"`
		Then Expr   `json:"then"`
		Else Expr   `json:"else"`
	}
	DotExpr struct {
		Kind    string `json:"kind" unpack:""`
		LHS     Expr   `json:"lhs"`
		RHS     string `json:"rhs"`
		Noneish bool   `json:"noneish"`
		Nullish bool   `json:"nullish"`
	}
	IndexExpr struct {
		Kind  string `json:"kind" unpack:""`
		Expr  Expr   `json:"expr"`
		Index Expr   `json:"index"`
		Base1 bool   `json:"base1"`
	}
	IsNullExpr struct {
		Kind string `json:"kind" unpack:""`
		Expr Expr   `json:"expr"`
	}
	MapCallExpr struct {
		Kind   string    `json:"kind" unpack:""`
		Expr   Expr      `json:"expr"`
		Lambda *CallExpr `json:"lambda"`
	}
	MapExpr struct {
		Kind    string  `json:"kind" unpack:""`
		Entries []Entry `json:"entries"`
	}
	PrimitiveExpr struct {
		Kind  string `json:"kind" unpack:""`
		Value string `json:"value"`
	}
	RecordExpr struct {
		Kind  string       `json:"kind" unpack:""`
		Elems []RecordElem `json:"elems"`
	}
	RegexpMatchExpr struct {
		Kind    string `json:"kind" unpack:""`
		Pattern string `json:"pattern"`
		Expr    Expr   `json:"expr"`
	}
	RegexpSearchExpr struct {
		Kind    string `json:"kind" unpack:""`
		Pattern string `json:"pattern"`
		Expr    Expr   `json:"expr"`
	}
	SearchExpr struct {
		Kind  string `json:"kind" unpack:""`
		Text  string `json:"text"`
		Value string `json:"value"`
		Expr  Expr   `json:"expr"`
	}
	SetExpr struct {
		Kind  string       `json:"kind" unpack:""`
		Elems []VectorElem `json:"elems"`
	}
	SliceExpr struct {
		Kind  string `json:"kind" unpack:""`
		Expr  Expr   `json:"expr"`
		From  Expr   `json:"from"`
		To    Expr   `json:"to"`
		Base1 bool   `json:"base1"`
	}
	SortExpr struct {
		Key   Expr        `json:"key"`
		Order order.Which `json:"order"`
		Nulls order.Nulls `json:"nulls"`
	}
	SubqueryExpr struct {
		Kind       string `json:"kind" unpack:""`
		Correlated bool   `json:"correlated"`
		Body       Seq    `json:"body"`
	}
	ThisExpr struct {
		Kind  string      `json:"kind" unpack:""`
		Chain field.Chain `json:"chain"`
	}
	TypeExpr struct {
		Kind string `json:"kind" unpack:""`
		ID   int    `json:"id"`
	}
	UnaryExpr struct {
		Kind    string `json:"kind" unpack:""`
		Op      string `json:"op"`
		Operand Expr   `json:"operand"`
	}
)

func (*AggExpr) exprNode()          {}
func (*ArrayExpr) exprNode()        {}
func (*BadExpr) exprNode()          {}
func (*BinaryExpr) exprNode()       {}
func (*CallExpr) exprNode()         {}
func (*CondExpr) exprNode()         {}
func (*DotExpr) exprNode()          {}
func (*IndexExpr) exprNode()        {}
func (*IsNullExpr) exprNode()       {}
func (*MapCallExpr) exprNode()      {}
func (*MapExpr) exprNode()          {}
func (*PrimitiveExpr) exprNode()    {}
func (*RecordExpr) exprNode()       {}
func (*RegexpMatchExpr) exprNode()  {}
func (*RegexpSearchExpr) exprNode() {}
func (*SearchExpr) exprNode()       {}
func (*SetExpr) exprNode()          {}
func (*SliceExpr) exprNode()        {}
func (*SubqueryExpr) exprNode()     {}
func (*ThisExpr) exprNode()         {}
func (*TypeExpr) exprNode()         {}
func (*UnaryExpr) exprNode()        {}

// Various Expr fields.

type (
	Entry struct {
		Key   Expr `json:"key"`
		Value Expr `json:"value"`
	}
	Field struct {
		Kind  string `json:"kind" unpack:""`
		Name  string `json:"name"`
		Value Expr   `json:"value"`
	}
	Spread struct {
		Kind string `json:"kind" unpack:""`
		Expr Expr   `json:"expr"`
	}
	VectorValue struct {
		Kind string `json:"kind" unpack:""`
		Expr Expr   `json:"expr"`
	}
)

func (*Field) recordElemNode()       {}
func (*Spread) recordElemNode()      {}
func (*Spread) vectorElemNode()      {}
func (*VectorValue) vectorElemNode() {}

func NewBinaryExpr(op string, lhs, rhs Expr) *BinaryExpr {
	return &BinaryExpr{
		Kind: "BinaryExpr",
		Op:   op,
		LHS:  lhs,
		RHS:  rhs,
	}
}

func NewCall(tag string, args []Expr) *CallExpr {
	return &CallExpr{
		Kind: "CallExpr",
		Tag:  tag,
		Args: args,
	}
}

func NewThis(chain field.Chain) *ThisExpr {
	return &ThisExpr{"ThisExpr", chain}
}

func (t *ThisExpr) String() string {
	return t.Chain.String()
}

func NewUnaryExpr(op string, e Expr) *UnaryExpr {
	return &UnaryExpr{"UnaryExpr", op, e}
}

func CopyExpr(e Expr) Expr {
	if e == nil {
		panic("CopyExpr nil")
	}
	b, err := json.Marshal(e)
	if err != nil {
		panic(err)
	}
	var copy Expr
	if err := unpacker.Unmarshal(b, &copy); err != nil {
		panic(err)
	}
	return copy
}

// NewRecordExprFromAssignments returns a record expression whose fields are
// the static LHS paths of assignments, nested as needed, and whose values are
// the corresponding RHS expressions.
func NewRecordExprFromAssignments(assignments []Assignment) (*RecordExpr, error) {
	rec := &RecordExpr{Kind: "RecordExpr"}
	for _, a := range assignments {
		lhs, ok := a.LHS.(*ThisExpr)
		if !ok {
			return nil, fmt.Errorf("dynamic field name not supported: %#v", a.LHS)
		}
		addChainToRecordExpr(rec, lhs.Chain, a.RHS)
	}
	return rec, nil
}

func addChainToRecordExpr(rec *RecordExpr, chain field.Chain, expr Expr) {
	if len(chain) == 1 {
		rec.Elems = append(rec.Elems, &Field{Kind: "Field", Name: chain[0].ID, Value: expr})
		return
	}
	i := slices.IndexFunc(rec.Elems, func(elem RecordElem) bool {
		f, ok := elem.(*Field)
		return ok && f.Name == chain[0].ID
	})
	if i == -1 {
		i = len(rec.Elems)
		rec.Elems = append(rec.Elems, &Field{Kind: "Field", Name: chain[0].ID, Value: &RecordExpr{Kind: "RecordExpr"}})
	}
	addChainToRecordExpr(rec.Elems[i].(*Field).Value.(*RecordExpr), chain[1:], expr)
}
