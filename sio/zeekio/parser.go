package zeekio

import (
	"errors"
	"fmt"
	"strings"

	"github.com/superdb/super"
)

type header struct {
	separator    string
	setSeparator string
	emptyField   string
	unsetField   string
	Path         string
	open         string
	close        string
	fields       []super.Field
}

type Parser struct {
	header
	sctx       *super.Context
	unknown    int // Count of unknown directives
	needfields bool
	needtypes  bool
	addpath    bool
	// descriptor is a lazily-allocated Descriptor corresponding
	// to the contents of the #fields and #types directives.
	descriptor   *super.TypeRecord
	builder      builder
	sourceFields []int
}

var ErrBadRecordDef = errors.New("bad types/fields definition in zeek header")

func NewParser(r *super.Context) *Parser {
	return &Parser{
		header: header{separator: " "},
		sctx:   r,
	}
}

func badfield(field string) error {
	return fmt.Errorf("encountered bad header field %s parsing zeek log", field)
}

func (p *Parser) parseFields(fieldNames []string) error {
	if len(p.fields) != len(fieldNames) {
		p.fields = make([]super.Field, len(fieldNames))
		p.needtypes = true
	}
	for k, name := range fieldNames {
		//XXX check that string conforms to a field name syntax
		p.fields[k].Name = name
	}
	p.needfields = false
	p.descriptor = nil
	return nil
}

func (p *Parser) parseTypes(types []string) error {
	if len(p.fields) != len(types) {
		p.fields = make([]super.Field, len(types))
		p.needfields = true
	}
	for k, name := range types {
		if name == "" {
			// zeek-cut leaves empty columns for missing fields.
			continue
		}
		typ, err := p.parseType(name)
		if err != nil {
			return err
		}
		p.fields[k].Type = typ
	}
	p.needtypes = false
	p.descriptor = nil
	return nil
}

func (p *Parser) parseType(in string) (super.Type, error) {
	in = strings.TrimSpace(in)
	if words := strings.SplitN(in, "[", 2); len(words) == 2 && strings.HasSuffix(words[1], "]") {
		if typ, err := p.parsePrimitiveType(strings.TrimSuffix(words[1], "]")); err == nil {
			if words[0] == "set" {
				return p.makeNullableType(p.sctx.LookupTypeSet(typ)), nil
			}
			if words[0] == "vector" {
				return p.makeNullableType(p.sctx.LookupTypeArray(typ)), nil
			}
		}
	}
	return p.parsePrimitiveType(in)
}

func (p *Parser) parsePrimitiveType(in string) (super.Type, error) {
	in = strings.TrimSpace(in)
	var typ super.Type
	var err error
	switch in {
	case "addr":
		typ = super.TypeIP
	case "bool":
		typ = super.TypeBool
	case "count":
		typ = super.TypeUint64
	case "double":
		typ = super.TypeFloat64
	case "enum":
		typ, err = p.sctx.LookupTypeNamed("zenum", super.TypeString)
	case "int":
		typ = super.TypeInt64
	case "interval":
		typ = super.TypeDuration
	case "port":
		typ, err = p.sctx.LookupTypeNamed("port", super.TypeUint16)
	case "string":
		typ = super.TypeString
	case "subnet":
		typ = super.TypeNet
	case "time":
		typ = super.TypeTimestamp
	default:
		return nil, fmt.Errorf("unknown type: %s", in)
	}
	if err != nil {
		return nil, err
	}
	return p.makeNullableType(typ), nil
}

func (p *Parser) makeNullableType(typ super.Type) super.Type {
	return p.sctx.Nullable(typ)
}

func (p *Parser) ParseDirective(line []byte) error {
	if line[0] == '#' {
		line = line[1:]
	}
	tokens := strings.Split(string(line), p.separator)
	switch tokens[0] {
	case "separator":
		if len(tokens) != 2 {
			return badfield("separator")
		}
		p.separator = string(unescapeZeekString([]byte(tokens[1])))
	case "set_separator":
		if len(tokens) != 2 {
			return badfield("set_separator")
		}
		p.setSeparator = tokens[1]
	case "empty_field":
		if len(tokens) != 2 {
			return badfield("empty_field")
		}
		//XXX this should be ok now as we process on ingest
		if tokens[1] != "(empty)" {
			return badfield(fmt.Sprintf("#empty_field (non-standard value '%s')", tokens[1]))
		}
		p.emptyField = tokens[1]
	case "unset_field":
		if len(tokens) != 2 {
			return badfield("unset_field")
		}
		//XXX this should be ok now as we process on ingest
		if tokens[1] != "-" {
			return badfield(fmt.Sprintf("#unset_field (non-standard value '%s')", tokens[1]))
		}
		p.unsetField = tokens[1]
	case "path":
		if len(tokens) != 2 {
			return badfield("path")
		}
		p.Path = tokens[1]
		if p.Path == "-" {
			p.Path = ""
		}
	case "open":
		if len(tokens) != 2 {
			return badfield("open")
		}
		p.open = tokens[1]
	case "close":
		if len(tokens) > 2 {
			return badfield("close")
		}
		if len(tokens) == 1 {
			p.close = ""
		} else {
			p.close = tokens[1]
		}

	case "fields":
		if len(tokens) < 2 {
			return badfield("fields")
		}
		if err := p.parseFields(tokens[1:]); err != nil {
			return err
		}
	case "types":
		if len(tokens) < 2 {
			return badfield("types")
		}
		if err := p.parseTypes(tokens[1:]); err != nil {
			return err
		}
	default:
		// XXX return an error?
		p.unknown++
	}
	return nil
}

// Unflatten turns a set of fields from legacy Zeek logs into a
// super-structured format by creating nested records for any dotted
// field names. If addpath is true, a _path field is added if not
// already present. The fields are returned as a slice along with a
// bool indicating if a _path field was added.
// Note that according to the super-structured data model, all the fields for a nested
// record must be adjacent which simplifies the logic here.
func Unflatten(sctx *super.Context, fields []super.Field, addPath bool) ([]super.Field, bool, error) {
	hasPath := false
	for _, f := range fields {
		// XXX could validate field names here...
		if f.Name == "_path" {
			hasPath = true
		}
	}
	out, err := unflattenRecord(sctx, fields)
	if err != nil {
		return nil, false, err
	}

	var needpath bool
	if addPath && !hasPath {
		out = append([]super.Field{super.NewField("_path", super.TypeString)}, out...)
		needpath = true
	}
	return out, needpath, nil
}

func unflattenRecord(sctx *super.Context, fields []super.Field) ([]super.Field, error) {
	// Extract a []super.Field consisting of all the leading fields
	// from the input that belong to the same record, with the
	// common prefix removed from their name.
	// Returns the prefix and the extracted same-record fields.
	recFields := func(fields []super.Field) (string, []super.Field) {
		var ret []super.Field
		var prefix string
		if dot := strings.IndexByte(fields[0].Name, '.'); dot != -1 {
			prefix = fields[0].Name[:dot]
		}
		for i := range fields {
			if !strings.HasPrefix(fields[i].Name, prefix+".") {
				break
			}
			trimmed := strings.TrimPrefix(fields[i].Name, prefix+".")
			ret = append(ret, super.NewField(trimmed, fields[i].Type))
		}
		return prefix, ret
	}
	var out []super.Field
	i := 0
	for i < len(fields) {
		f := fields[i]
		if strings.IndexByte(f.Name, '.') < 0 {
			// Just a top-level field.
			out = append(out, f)
			i++
			continue
		}
		prefix, nestedFields := recFields(fields[i:])
		recFields, err := unflattenRecord(sctx, nestedFields)
		if err != nil {
			return nil, err
		}
		recType, err := sctx.LookupTypeRecord(recFields)
		if err != nil {
			return nil, err
		}
		out = append(out, super.NewField(prefix, recType))
		i += len(nestedFields)
	}
	return out, nil
}

func (p *Parser) setDescriptor() error {
	if len(p.fields) == 0 || p.needfields || p.needtypes {
		return ErrBadRecordDef
	}
	fields, sourceFields := coalesceRecordFields(p.fields)
	fields, addpath, err := Unflatten(p.sctx, fields, p.Path != "")
	if err != nil {
		return err
	}
	p.descriptor, err = p.sctx.LookupTypeRecord(fields)
	if err != nil {
		return err
	}
	p.addpath = addpath
	p.sourceFields = sourceFields
	return nil
}

// coalesceRecordFields returns a permutation of fields in which the fields of
// each nested record have been made adjacent along with a slice containing the
// index of the source field for each field in that permutation.
func coalesceRecordFields(fields []super.Field) ([]super.Field, []int) {
	prefixes := map[string]bool{"": true}
	var outFields []super.Field
	var sourceFields []int
	for i, f := range fields {
		if f.Name == "" {
			continue
		}
		outFields = append(outFields, f)
		sourceFields = append(sourceFields, i)
		prefix := getPrefix(f.Name)
		for !prefixes[prefix] {
			prefixes[prefix] = true
			prefix = getPrefix(prefix)
		}
		if prefix != "" {
			for j := i; j > 0; j-- {
				if strings.HasPrefix(outFields[j-1].Name, prefix) {
					// Insert c at j.
					copy(outFields[j+1:], outFields[j:])
					outFields[j] = f
					copy(sourceFields[j+1:], sourceFields[j:])
					sourceFields[j] = i
					break
				}
			}
		}
	}
	return outFields, sourceFields
}

// getPrefix returns the prefix of dotpath up to and including its final period.
// If dotpath does not contain a period, getPrefix returns the empty string.
func getPrefix(name string) string {
	name = strings.TrimRight(name, ".")
	i := strings.LastIndex(name, ".")
	if i < 0 {
		return ""
	}
	return name[:i+1]
}

// Descriptor returns the current descriptor (from the most recently
// seen #types and #fields lines) and a bool indicating whether _path
// was added to the descriptor. If no descriptor is present, nil and
// and false are returned.
func (p *Parser) Descriptor() (*super.TypeRecord, bool) {
	if p.descriptor != nil {
		return p.descriptor, p.addpath
	}
	if err := p.setDescriptor(); err != nil {
		return nil, false
	}
	return p.descriptor, p.addpath

}

func (p *Parser) ParseValue(line []byte) (*super.Value, error) {
	if p.descriptor == nil {
		if err := p.setDescriptor(); err != nil {
			return nil, err
		}
	}
	var path []byte
	if p.Path != "" && p.addpath {
		//XXX should store path as a byte slice so it doens't get copied
		// each time here
		path = []byte(p.Path)
	}
	return p.builder.build(p.descriptor, len(p.fields), p.sourceFields, path, line)
}
