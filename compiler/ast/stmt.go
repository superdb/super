package ast

// A Script is a sequence of statements that are run one after the other,
// each to completion, where the declarations in Decls are in scope for
// every statement.  A query comprising a single query statement is not
// a Script but the Seq (or ScopeOp) of the query itself.
//
// Script implements Op so that it may appear as the sole element of the
// top-level Seq of a parsed program.
type Script struct {
	Kind  string `json:"kind" unpack:""`
	Decls []Decl `json:"decls"`
	Stmts []Stmt `json:"stmts"`
	Loc   `json:"loc"`
}

func (*Script) opNode() {}

// Stmt is the interface implemented by the statements of a Script.
type Stmt interface {
	Node
	stmtNode()
}

type (
	// QueryStmt is a query run as a statement of a script.
	QueryStmt struct {
		Kind string `json:"kind" unpack:""`
		Body Seq    `json:"body"`
		Loc  `json:"loc"`
	}
	// CreateTableStmt creates a database table.  The type of the table,
	// if any, is given either by Columns (a SQL schema converted to a
	// record type) or by Type (any SuperSQL type for a super table, i.e.,
	// CREATE SUPER TABLE) but not both.  A table without a type accepts any
	// value while values loaded into a table with a type are cast to it.
	CreateTableStmt struct {
		Kind        string      `json:"kind" unpack:""`
		Name        *Text       `json:"name"`
		IfNotExists bool        `json:"if_not_exists"`
		Columns     []ColumnDef `json:"columns"`
		Type        Type        `json:"type"`
		OrderBy     *SQLOrderBy `json:"order_by"`
		Loc         `json:"loc"`
	}
	// DropTableStmt drops a database table.
	DropTableStmt struct {
		Kind     string `json:"kind" unpack:""`
		Name     *Text  `json:"name"`
		IfExists bool   `json:"if_exists"`
		Loc      `json:"loc"`
	}
	// InsertStmt inserts the results of the query in Body into the table
	// Table.  If Columns is non-empty or Body is a SQL query, the result
	// columns are assigned positionally to the table's columns.
	InsertStmt struct {
		Kind    string `json:"kind" unpack:""`
		Table   *Text  `json:"table"`
		Columns []*ID  `json:"columns"`
		Body    Seq    `json:"body"`
		Loc     `json:"loc"`
	}
)

// ColumnDef is a column of a SQL table schema.
type ColumnDef struct {
	Name    *ID  `json:"name"`
	Type    Type `json:"type"`
	NotNull bool `json:"not_null"`
	Loc     `json:"loc"`
}

func (*QueryStmt) stmtNode()       {}
func (*CreateTableStmt) stmtNode() {}
func (*DropTableStmt) stmtNode()   {}
func (*InsertStmt) stmtNode()      {}
