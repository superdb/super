package ast

type Value interface {
	valueNode()
}

type DeclsValue struct {
	Kind  string     `json:"kind" unpack:""`
	Value Value      `json:"value"`
	Decls []TypeDecl `json:"decls"`
}

type Decorated struct {
	Kind  string `json:"kind" unpack:""`
	Value Value  `json:"value"`
	Type  Type   `json:"type"`
}

func (*DeclsValue) valueNode() {}
func (*Decorated) valueNode()  {}

// Only Primitive and TypeValue are used in the parser so only these are given
// ast.Node features.

type (
	Primitive struct {
		Kind string `json:"kind" unpack:""`
		Type string `json:"type"`
		Text string `json:"text"`
		Loc  `json:"loc"`
	}
	Record struct {
		Kind   string  `json:"kind" unpack:""`
		Fields []Field `json:"fields"`
	}
	Field struct {
		Name  string `json:"name"`
		Value Value  `json:"value"`
		Opt   bool   `json:"opt"`
	}
	Array struct {
		Kind     string  `json:"kind" unpack:""`
		Elements []Value `json:"elements"`
	}
	Set struct {
		Kind     string  `json:"kind" unpack:""`
		Elements []Value `json:"elements"`
	}
	Map struct {
		Kind    string  `json:"kind" unpack:""`
		Entries []Entry `json:"entries"`
	}
	Entry struct {
		Key   Value `json:"key"`
		Value Value `json:"value"`
	}
	TypeValue struct {
		Kind  string `json:"kind" unpack:""`
		Value Type   `json:"value"`
		Loc   `json:"loc"`
	}
	Error struct {
		Kind  string `json:"kind" unpack:""`
		Value Value  `json:"value"`
	}
	Super struct {
		Kind  string     `json:"kind" unpack:""`
		Value Value      `json:"value"`
		Type  *TypeValue `json:"type"`
	}
	Some struct {
		Kind  string `json:"kind" unpack:""`
		Value Value  `json:"value"`
	}
)

func (*Primitive) valueNode()       {}
func (*Record) valueNode()          {}
func (*Array) valueNode()           {}
func (*Set) valueNode()             {}
func (*Map) valueNode()             {}
func (*TypeValue) valueNode()       {}
func (*Error) valueNode()           {}
func (*Super) valueNode()           {}
func (*Some) valueNode()            {}
func (*DoubleQuoteExpr) valueNode() {}

func (*Primitive) ExprAST() {}
func (*TypeValue) ExprAST() {}
