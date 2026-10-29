package ast

type Decl interface {
	Node
	declNode()
}

type ConstDecl struct {
	Kind string `json:"kind" unpack:""`
	Name *ID    `json:"name"`
	Expr Expr   `json:"expr"`
	Loc  `json:"loc"`
}

type FuncDecl struct {
	Kind   string      `json:"kind" unpack:""`
	Name   *ID         `json:"name"`
	Lambda *LambdaExpr `json:"lambda"`
	Loc    `json:"loc"`
}

type OpDecl struct {
	Kind   string `json:"kind" unpack:""`
	Name   *ID    `json:"name"`
	Params []*ID  `json:"params"`
	Body   Seq    `json:"body"`
	Loc    `json:"loc"`
}

type PragmaDecl struct {
	Kind string `json:"kind" unpack:""`
	Name *ID    `json:"name"`
	Expr Expr   `json:"expr"`
	Loc  `json:"loc"`
}

type QueryDecl struct {
	Kind string `json:"kind" unpack:""`
	Name *ID    `json:"name"`
	Body Seq    `json:"body"`
	Loc  `json:"loc"`
}

type TypeDecl struct {
	Kind string `json:"kind" unpack:""`
	Name *ID    `json:"name"`
	Type Type   `json:"type"`
	Loc  `json:"loc"`
}

// TypeQueryDecl binds a type name to the type value computed by a query,
// e.g., type T = (from file | aggregate fuse(this)).
type TypeQueryDecl struct {
	Kind string `json:"kind" unpack:""`
	Name *ID    `json:"name"`
	Body Seq    `json:"body"`
	Loc  `json:"loc"`
}

func (*ConstDecl) declNode()     {}
func (*FuncDecl) declNode()      {}
func (*OpDecl) declNode()        {}
func (*PragmaDecl) declNode()    {}
func (*QueryDecl) declNode()     {}
func (*TypeDecl) declNode()      {}
func (*TypeQueryDecl) declNode() {}
