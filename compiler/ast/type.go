package ast

type Type interface {
	Node
	typeNode()
}

type (
	TypePrimitive struct {
		Kind string `json:"kind" unpack:""`
		Name string `json:"name"`
		Loc  `json:"loc"`
	}
	TypeRecord struct {
		Kind   string      `json:"kind" unpack:""`
		Fields []TypeField `json:"fields"`
		Loc    `json:"loc"`
	}
	TypeField struct {
		Name string `json:"name"`
		Type Type   `json:"type"`
		Opt  bool   `json:"opt"`
		Loc  `json:"loc"`
	}
	TypeArray struct {
		Kind string `json:"kind" unpack:""`
		Type Type   `json:"type"`
		Loc  `json:"loc"`
	}
	TypeSet struct {
		Kind string `json:"kind" unpack:""`
		Type Type   `json:"type"`
		Loc  `json:"loc"`
	}
	TypeUnion struct {
		Kind  string `json:"kind" unpack:""`
		Types []Type `json:"types"`
		Loc   `json:"loc"`
	}
	TypeEnum struct {
		Kind    string  `json:"kind" unpack:""`
		Symbols []*Text `json:"symbols"`
		Loc     `json:"loc"`
	}
	TypeMap struct {
		Kind    string `json:"kind" unpack:""`
		KeyType Type   `json:"key_type"`
		ValType Type   `json:"val_type"`
		Loc     `json:"loc"`
	}
	TypeError struct {
		Kind string `json:"kind" unpack:""`
		Type Type   `json:"type"`
		Loc  `json:"loc"`
	}
	TypeSuper struct {
		Kind string `json:"kind" unpack:""`
		Type Type   `json:"type"`
		Loc  `json:"loc"`
	}
	TypeOption struct {
		Kind string `json:"kind" unpack:""`
		Type Type   `json:"type"`
		Loc  `json:"loc"`
	}
	TypeRef struct {
		Kind string `json:"kind" unpack:""`
		Name string `json:"name"`
		Loc  `json:"loc"`
	}
	TypeDecimal struct {
		Kind      string `json:"kind" unpack:""`
		Precision int    `json:"precision"`
		Scale     int    `json:"scale"`
		Loc       `json:"loc"`
	}
)

func (*TypePrimitive) typeNode() {}
func (*TypeRecord) typeNode()    {}
func (*TypeArray) typeNode()     {}
func (*TypeSet) typeNode()       {}
func (*TypeUnion) typeNode()     {}
func (*TypeEnum) typeNode()      {}
func (*TypeMap) typeNode()       {}
func (*TypeError) typeNode()     {}
func (*TypeSuper) typeNode()     {}
func (*TypeOption) typeNode()    {}
func (*TypeRef) typeNode()       {}
func (*TypeDecimal) typeNode()   {}

// This is a hack that allows us to express a "date" type until we
// get proper coverage of SQL date/time types natively in the super-structured
// data model.
type DateTypeHack struct {
	Kind string `json:"kind" unpack:""`
	Loc  `json:"loc"`
}

func (*DateTypeHack) typeNode() {}
