package parser_test

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/superdb/super/compiler/parser"
	"github.com/superdb/super/pkg/fs"
	"github.com/superdb/super/ztest"
)

func BenchmarkParse(b *testing.B) {
	queries, err := searchForSuperSQL()
	require.NoError(b, err)
	b.ResetTimer()
	var buf []byte
	for b.Loop() {
		for _, q := range queries {
			buf = append(buf[:0], q...)
			parser.Parse("", buf)
		}
	}
}

func searchForSuperSQL() ([]string, error) {
	var queries []string
	pattern := fmt.Sprintf(`.*ztests\%c.*\.yaml$`, filepath.Separator)
	re := regexp.MustCompile(pattern)
	err := filepath.Walk("..", func(path string, info os.FileInfo, err error) error {
		if !info.IsDir() && strings.HasSuffix(path, ".yaml") && re.MatchString(path) {
			ztests, err := ztest.FromYAMLFile(path)
			if err != nil {
				return fmt.Errorf("%s: %w", path, err)
			}
			for _, z := range ztests {
				if s := z.SPQ; s != "" {
					queries = append(queries, s)
				}
			}
		}
		return err
	})
	return queries, err
}

func parseOp(z string) ([]byte, error) {
	ast, err := parser.ParseText(z)
	if err != nil {
		return nil, err
	}
	return json.Marshal(ast.Parsed())
}

func parsePigeon(z string) ([]byte, error) {
	ast, err := parser.Parse("", []byte(z))
	if err != nil {
		return nil, err
	}
	return json.Marshal(ast)
}

// testQuery checks both that the parse is successful and that the
// two resulting ASTs from the round trip through json marshal and
// unmarshal are equivalent.
func testQuery(t *testing.T, line string) {
	pigeonJSON, err := parsePigeon(line)
	assert.NoError(t, err, "parsePigeon: %q", line)

	astJSON, err := parseOp(line)
	assert.NoError(t, err, "parseOp: %q", line)

	assert.JSONEq(t, string(pigeonJSON), string(astJSON), "pigeon and AST mismatch: %q", line)
}

func TestValid(t *testing.T) {
	file, err := fs.Open("valid.spq")
	require.NoError(t, err)
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Bytes()
		testQuery(t, string(line))
	}
}

func TestZtestSuperSQL(t *testing.T) {
	queries, err := searchForSuperSQL()
	require.NoError(t, err)
	for _, q := range queries {
		testQuery(t, q)
	}
}

func TestInvalid(t *testing.T) {
	file, err := fs.Open("invalid.spq")
	require.NoError(t, err)
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Bytes()
		_, err := parser.Parse("", line)
		assert.Error(t, err, "query: %q", line)
	}
}
