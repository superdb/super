package mcp

import (
	"encoding/json"
	"fmt"

	"github.com/superdb/super/compiler/parser"
)

// writeKinds maps the kinds of the AST nodes that modify the database to
// their descriptions.
var writeKinds = map[string]string{
	"CreateTableStmt": "CREATE TABLE",
	"DropTableStmt":   "DROP TABLE",
	"InsertStmt":      "INSERT INTO",
	"LoadOp":          "load",
	"Delete":          "delete",
}

// checkReadOnly returns an error if query modifies the database.  The check
// looks for a node that modifies the database anywhere in the query's AST
// (including in declarations and subqueries).  A query that doesn't parse
// isn't rejected here so running it reports the parse error.
func checkReadOnly(query string) error {
	ast, err := parser.ParseText(query)
	if err != nil {
		return nil
	}
	// Every AST node has a kind in its JSON form, so walk that form
	// rather than every Go node type.
	b, err := json.Marshal(ast.Parsed())
	if err != nil {
		return err
	}
	var tree any
	if err := json.Unmarshal(b, &tree); err != nil {
		return err
	}
	if what := findWrite(tree); what != "" {
		return fmt.Errorf("the server is read-only but the query modifies the database (%s)", what)
	}
	return nil
}

func findWrite(v any) string {
	switch v := v.(type) {
	case map[string]any:
		if kind, ok := v["kind"].(string); ok {
			if what, ok := writeKinds[kind]; ok {
				return what
			}
		}
		for _, elem := range v {
			if what := findWrite(elem); what != "" {
				return what
			}
		}
	case []any:
		for _, elem := range v {
			if what := findWrite(elem); what != "" {
				return what
			}
		}
	}
	return ""
}
