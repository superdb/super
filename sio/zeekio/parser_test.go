package zeekio

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/superdb/super"
)

const (
	separator    = "#separator \\x09"
	setSeparator = "#set_separator\t,"
	empty        = "#empty_field\t(empty)"
	unset        = "#unset_field\t-"
)

var (
	standardHeaders = []string{separator, setSeparator, empty, unset}
	fields          = []string{"str", "num", "addr", "ss", "sa"}
	types           = []string{"string", "int", "addr", "set[string]", "set[addr]"}
	values          = []string{"foo", "123", "10.5.5.5", "foo,bar,baz", "10.1.1.0,10.1.1.1,10.1.1.2"}
)

func makeHeader(name string, rest []string) string {
	return strings.Join(append([]string{name}, rest...), "\t")
}

// startTest() creates a new parser and sends all the provided
// directives, expecting them to all be parsed successfully.
// A parser object ready for further testing is returned.
func startTest(t *testing.T, headers []string) *Parser {
	p := NewParser(super.NewContext())
	for _, h := range headers {
		require.NoError(t, p.ParseDirective([]byte(h)))
	}

	return p
}

// startLegacyTest() creates a new parser and sends the standard
// zeek legacy directives.  If any of fields, types, path are provided,
// corresponding #files, #types, and #path directives are also sent.
// A parser object ready for further testing is returned.
func startLegacyTest(t *testing.T, fields, types []string, path string) *Parser {
	headers := standardHeaders
	if len(path) > 0 {
		headers = append(headers, fmt.Sprintf("#path\t%s", path))
	}
	if len(fields) > 0 {
		headers = append(headers, makeHeader("#fields", fields))
	}
	if len(types) > 0 {
		headers = append(headers, makeHeader("#types", types))
	}

	return startTest(t, headers)
}

// sendLegacyValues() formats the array of values as a legacy zeek log line
// and parses it.
func sendLegacyValues(p *Parser, vals []string) (*super.Value, error) {
	return p.ParseValue([]byte(strings.Join(vals, "\t")))
}

func assertError(t *testing.T, err error, pattern, what string) {
	assert.NotNilf(t, err, "Received error for %s", what)
	assert.Containsf(t, err.Error(), pattern, "error message for %s is as expected", what)
}

// Test things related to legacy zeek records that the parser should
// handle successfully.
func TestLegacyZeekValid(t *testing.T) {
	// Test standard headers but no timestamp in records
	parser := startLegacyTest(t, fields, types, "")
	record, err := sendLegacyValues(parser, values)
	require.NoError(t, err)

	assert.Equal(t, record.Deref("ts").NoneAsNull(), super.Null)
	// XXX check contents of other fields?

	// Test standard headers with a timestamp in records
	fieldsWithTs := append(fields, "ts")
	typesWithTs := append(types, "time")
	parser = startLegacyTest(t, fieldsWithTs, typesWithTs, "")

	timestamp := "1573588318384.000"
	valsWithTs := append(values, timestamp)
	record, err = sendLegacyValues(parser, valsWithTs)
	require.NoError(t, err)

	expectedTs, err := parseTime([]byte(timestamp))
	require.NoError(t, err)
	x := record.Deref("ts").Under().Ptr().AsTime()
	assert.Equal(t, expectedTs, x, "Timestamp is correct")

	// Test the #path header
	parser = startLegacyTest(t, fieldsWithTs, typesWithTs, "testpath")
	record, err = sendLegacyValues(parser, valsWithTs)
	require.NoError(t, err)

	path := record.Deref("_path").AsString()
	assert.Equal(t, path, "testpath", "Legacy _path field was set properly")

	// XXX test overriding separator, setSeparator
}

func TestNestedRecords(t *testing.T) {
	// Test the parser handling of nested records.
	// The schema used here touches several edge cases:
	//  - nested records separated by a regular field
	//  - adjacent nested records (nest2, nest3)
	//  - nested record containing nonadjacent fields (nest1)
	//  - nested record as the final field
	names := []string{"a", "nest1.a", "nest1.b", "b", "nest2.y", "nest1.nestnest.c", "nest3.z"}
	types := []string{"int", "int", "int", "int", "int", "int", "int"}
	vals := []string{"1", "2", "3", "4", "5", "6", "7"}

	parser := startLegacyTest(t, names, types, "")
	record, err := sendLegacyValues(parser, vals)
	require.NoError(t, err)
	require.NoError(t, record.Validate())

	// First check that the descriptor was created correctly
	fields := super.TypeRecordOf(record.Type()).Fields
	assert.Equal(t, 5, len(fields), "Descriptor has 5 fields")
	assert.Equal(t, "a", fields[0].Name, "Field 0 is a")
	assert.Equal(t, "nest1", fields[1].Name, "Field 1 is nest1")
	nest1Type, ok := fields[1].Type.(*super.TypeRecord)
	assert.True(t, ok, "Fields nest1 is a record")
	assert.Equal(t, 3, len(nest1Type.Fields), "nest1 has 3 fields")
	assert.Equal(t, "a", nest1Type.Fields[0].Name, "First field in nest1 is a")
	assert.Equal(t, "b", nest1Type.Fields[1].Name, "Second field in nest1 is b")
	assert.Equal(t, "nestnest", nest1Type.Fields[2].Name, "Third field in nest1 is nestnest")
	nestnestType, ok := nest1Type.Fields[2].Type.(*super.TypeRecord)
	assert.True(t, ok, "nest1.nestnest is a record")
	assert.Equal(t, 1, len(nestnestType.Fields), "nest1.nestnest has 1 field")
	assert.Equal(t, "c", nestnestType.Fields[0].Name, "First field in nest1.nestnest is c")
	assert.Equal(t, "b", fields[2].Name, "Field 2 is b")
	assert.Equal(t, "nest2", fields[3].Name, "Field 3 is nest2")
	nest2Type, ok := fields[3].Type.(*super.TypeRecord)
	assert.True(t, ok, "Fields nest2 is a record")
	assert.Equal(t, 1, len(nest2Type.Fields), "nest2 has 1 field")
	assert.Equal(t, "y", nest2Type.Fields[0].Name, "field in nest2 is y")
	assert.Equal(t, "nest3", fields[4].Name, "Field 4 is nest3")
	nest3Type, ok := fields[4].Type.(*super.TypeRecord)
	assert.True(t, ok, "Field nest3 is a record")
	assert.Equal(t, 1, len(nest3Type.Fields), "nest3 has 1 field")
	assert.Equal(t, "z", nest3Type.Fields[0].Name, "field in nest3 is z")

	// Now check the actual values
	assert.Equal(t, 1, int(record.Deref("a").Under().Ptr().AsInt()), "Field a has value 1")

	e := record.Deref("nest1")
	assert.Equal(t, nest1Type, e.Type(), "Got right type for field nest1")
	assert.Equal(t, 2, int(e.Deref("a").Under().Ptr().AsInt()), "nest1.a")
	assert.Equal(t, 3, int(e.Deref("b").Under().Ptr().AsInt()), "nest1.b")

	e = e.Deref("nestnest")
	assert.Equal(t, nestnestType, e.Type(), "Got right type for field nest1.nestnest")
	assert.Equal(t, 6, int(e.Deref("c").Under().Ptr().AsInt()), "nest1.nestnest.c")

	assert.Equal(t, 4, int(record.Deref("b").Under().Ptr().AsInt()), "Field b has value 4")

	e = record.Deref("nest2")
	assert.Equal(t, nest2Type, e.Type(), "Got right type for field nest2")
	assert.Equal(t, 5, int(e.Deref("y").Under().Ptr().AsInt()), "nest2.y")

	e = record.Deref("nest3")
	assert.Equal(t, nest3Type, e.Type(), "Got right type for field nest3")
	assert.Equal(t, 7, int(e.Deref("z").Under().Ptr().AsInt()), "nest3.z")
}

// Test things related to legacy zeek records that should cause the
// parser to generate errors.
func TestLegacyZeekInvalid(t *testing.T) {
	// Test that a non-standard value for empty_field is rejected
	parser := startTest(t, []string{separator, setSeparator})
	err := parser.ParseDirective([]byte("#empty_field\tboo"))
	assertError(t, err, "encountered bad header field", "#empty_field header")

	// Test that a non-standard value for unset_field is rejected
	parser = startTest(t, []string{separator, setSeparator})
	err = parser.ParseDirective([]byte("#unset_field\tboo"))
	assertError(t, err, "encountered bad header field", "#unset header")

	// Test that missing #fields/#values headers is an error
	parser = startTest(t, standardHeaders)
	_, err = sendLegacyValues(parser, values)
	assertError(t, err, "bad types/fields definition", "missing #fields/#types header")

	// Test that #fields header without #values is an error
	fh := makeHeader("#fields", fields)
	parser = startTest(t, append(standardHeaders, fh))
	_, err = sendLegacyValues(parser, values)
	assertError(t, err, "bad types/fields definition", "missing #types header")

	// Test that #types header without #fields is an error
	th := makeHeader("#types", types)
	parser = startTest(t, append(standardHeaders, th))
	_, err = sendLegacyValues(parser, values)
	assertError(t, err, "bad types/fields definition", "values without #fields")

	// Test that mismatched #fields/#types headers is an error
	/* XXX fixme
	parser = startTest(t, append(standardHeaders, fh))
	err = parser.parseDirective([]byte(makeHeader("#types", append(types, "int"))))
	assertError(t, err, "bad types/fields definition", "mismatched #fields/#types headers")
	*/

	// Test that too many values is an error
	parser = startTest(t, append(standardHeaders, fh, th))
	_, err = sendLegacyValues(parser, append(values, "extra"))
	assertError(t, err, "too many values", "wrong number of values")

	// Test that too few values is an error
	parser = startTest(t, append(standardHeaders, fh, th))
	_, err = sendLegacyValues(parser, values[:len(values)-2])
	assertError(t, err, "too few values", "wrong number of values")

	// XXX check invalid types?
}
