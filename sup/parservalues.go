package sup

import (
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/netip"
	"slices"
	"strconv"
	"time"

	"github.com/superdb/super"
	"github.com/superdb/super/compiler/ast"
	"github.com/superdb/super/pkg/nano"
	"github.com/superdb/super/scode"
)

func (p *Parser) ParseValue() (ast.Value, error) {
	v, err := p.matchOuterValue()
	if v == nil && err == nil {
		err = p.lexerError()
	}
	return v, err
}

func (p *Parser) lexerError() error {
	if err := p.lexer.check(1); err != nil && err != io.EOF {
		return fmt.Errorf("line %d: %w", p.lexer.line, err)
	}
	if len(p.lexer.cursor) > 0 {
		return fmt.Errorf("line %d: syntax error", p.lexer.line)
	}
	return nil
}

func noEOF(err error) error {
	if err == io.EOF {
		err = nil
	}
	return err
}

func (p *Parser) matchOuterValue() (ast.Value, error) {
	var decls []ast.TypeDecl
	for {
		val, decl, err := p.matchValueOrDecl()
		if err != nil {
			return nil, err
		}
		if decl != nil {
			decls = append(decls, *decl)
			continue
		}
		if val == nil {
			return nil, nil
		}
		if len(decls) != 0 {
			val = &ast.DeclsValue{
				Kind:  "DeclsValue",
				Decls: decls,
				Value: val,
			}
		}
		return val, nil
	}
}

func (p *Parser) matchValue() (ast.Value, error) {
	val, decl, err := p.matchValueOrDecl()
	if noEOF(err) != nil {
		return nil, err
	}
	if decl != nil {
		return nil, errors.New("invalid type declaration inside value")
	}
	return val, nil
}

func (p *Parser) matchValueOrDecl() (ast.Value, *ast.TypeDecl, error) {
	if val, err := p.matchRecord(); val != nil || err != nil {
		val, err := p.decorate(val, err)
		return val, nil, err
	}
	if val, err := p.matchArray(); val != nil || err != nil {
		val, err := p.decorate(val, err)
		return val, nil, err
	}
	if val, err := p.matchSetOrMap(); val != nil || err != nil {
		val, err := p.decorate(val, err)
		return val, nil, err
	}
	if val, err := p.matchTypeValue(); val != nil || err != nil {
		val, err := p.decorate(val, err)
		return val, nil, err
	}
	// Primitive comes last as the other matchers short-circuit more
	// efficiently on sentinel characters.
	if val, err := p.matchPrimitive(); val != nil || err != nil {
		val, err := p.decorate(val, err)
		return val, nil, err
	}
	if val, err := p.matchNone(); val != nil || err != nil {
		val, err := p.decorate(val, err)
		return val, nil, err
	}
	name, err := p.matchIdentifier()
	if err != nil {
		return nil, nil, noEOF(err)
	}
	if typ, err := p.matchTypeDecl(name); typ != nil || err != nil {
		return nil, typ, err
	}
	if val, err := p.matchFusion(name); val != nil || err != nil {
		val, err := p.decorate(val, err)
		return val, nil, err
	}
	if val, err := p.matchSome(name); val != nil || err != nil {
		val, err := p.decorate(val, err)
		return val, nil, err
	}
	if val, err := p.matchError(name); val != nil || err != nil {
		val, err := p.decorate(val, err)
		return val, nil, err
	}
	return nil, nil, nil
}

func (p *Parser) decorate(val ast.Value, err error) (ast.Value, error) {
	if err != nil {
		return nil, err
	}
	for {
		decorated, ok, err := p.matchDecorator(val)
		if err != nil {
			return nil, err
		}
		if !ok {
			return val, nil
		}
		val = decorated
	}
}

func (p *Parser) matchDecorator(val ast.Value) (ast.Value, bool, error) {
	l := p.lexer
	// If there isn't a decorator, just return.  A decorator cannot start
	// with ":::" so we check this condition which arises when an IP6 address or net
	// with a "::" prefix follows a map key (and so we are checking for a decorator
	// here after the key value but before the key colon).
	if lookahead, err := l.peek(3); err != nil || lookahead == "" || lookahead[:2] != "::" || lookahead == ":::" {
		return nil, false, err
	}
	l.skip(2)
	ok, err := l.match('=')
	if err != nil {
		return nil, false, err
	}
	if ok {
		return nil, false, errors.New("embedded type syntax ::=<type> no longer supported")
	}
	typ, err := p.matchTypeComponent()
	if noEOF(err) != nil {
		return nil, false, err
	}
	return &ast.Decorated{
		Kind:  "Decorated",
		Value: val,
		Type:  typ,
	}, true, nil
}

func (p *Parser) matchPrimitive() (*ast.Primitive, error) {
	if val, err := p.matchStringPrimitive(); val != nil || err != nil {
		return val, noEOF(err)
	}
	l := p.lexer
	if err := l.skipSpace(); err != nil {
		return nil, noEOF(err)
	}
	s, err := l.peekPrimitive()
	if err != nil {
		return nil, noEOF(err)
	}
	if s == "" {
		return nil, nil
	}
	// Try to parse the string different ways.  This is not intended
	// to be performant.  BSUP provides performance for the Super data model.
	var typ string
	if s == "true" || s == "false" {
		typ = "bool"
	} else if s == "null" {
		typ = "null"
	} else if _, err := strconv.ParseInt(s, 10, 64); err == nil {
		typ = "int64"
	} else if _, err := strconv.ParseUint(s, 10, 64); err == nil {
		typ = "uint64"
	} else if _, err := strconv.ParseFloat(s, 64); err == nil {
		typ = "float64"
	} else if _, err := time.Parse(time.RFC3339Nano, s); err == nil {
		typ = "time"
	} else if _, err := nano.ParseDuration(s); err == nil {
		typ = "duration"
	} else if _, err := netip.ParsePrefix(s); err == nil {
		typ = "net"
	} else if _, err := netip.ParseAddr(s); err == nil {
		typ = "ip"
	} else if len(s) >= 2 && s[0:2] == "0x" {
		if len(s) == 2 {
			typ = "bytes"
		} else if _, err := hex.DecodeString(s[2:]); err == nil {
			typ = "bytes"
		} else {
			return nil, err
		}
	} else {
		// no match
		return nil, nil
	}
	l.skip(len(s))
	return &ast.Primitive{
		Kind: "Primitive",
		Type: typ,
		Text: s,
	}, nil
}

func (p *Parser) matchStringPrimitive() (*ast.Primitive, error) {
	s, ok, err := p.matchString()
	if err != nil || !ok {
		return nil, noEOF(err)
	}
	return &ast.Primitive{
		Kind: "Primitive",
		Type: "string",
		Text: s,
	}, nil
}

func (p *Parser) matchString() (string, bool, error) {
	l := p.lexer
	ok, err := l.match('"')
	if err != nil || !ok {
		return "", false, noEOF(err)
	}
	s, err := l.scanString()
	if err != nil {
		return "", false, p.errorf("string literal: %s", err)
	}
	ok, err = l.match('"')
	if err != nil {
		return "", false, err
	}
	if !ok {
		return "", false, p.error("mismatched string quotes")
	}
	return s, true, nil
}

func (p *Parser) matchNone() (*ast.None, error) {
	l := p.lexer
	if lookahead, err := l.peek(4); err != nil || lookahead == "" || lookahead[:4] != "none" {
		return nil, noEOF(err)
	}
	l.skip(4)
	return &ast.None{
		Kind: "None",
	}, nil
}

func (p *Parser) matchRecord() (*ast.Record, error) {
	l := p.lexer
	if ok, err := l.match('{'); !ok || err != nil {
		return nil, noEOF(err)
	}
	fields, err := p.matchFields()
	if err != nil {
		return nil, err
	}
	ok, err := l.match('}')
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, p.error("mismatched braces while parsing record type")
	}
	return &ast.Record{
		Kind:   "Record",
		Fields: fields,
	}, nil
}

func (p *Parser) matchFields() ([]ast.Field, error) {
	l := p.lexer
	var fields []ast.Field
	seen := make(map[string]struct{})
	for {
		field, err := p.matchField()
		if err != nil {
			return nil, err
		}
		if field == nil {
			break
		}
		if _, ok := seen[field.Name]; !ok {
			fields = append(fields, *field)
		}
		seen[field.Name] = struct{}{}
		ok, err := l.match(',')
		if err != nil {
			return nil, err
		}
		if !ok {
			break
		}
	}
	return fields, nil
}

func (p *Parser) matchField() (*ast.Field, error) {
	l := p.lexer
	name, ok, err := p.matchSymbol()
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, nil
	}
	var opt bool
	opt, err = l.match('?')
	if err != nil {
		return nil, err
	}
	ok, err = l.match(':')
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, p.errorf("no type name found for field %q", name)
	}
	val, err := p.matchValue()
	if val == nil && err == nil {
		err = p.lexerError()
	}
	if err != nil {
		return nil, err
	}
	if isNone(val) && !opt && !isOption(val) {
		return nil, p.errorf("none cannot appear in non-optional field %q", name)
	}
	return &ast.Field{
		Name:  name,
		Value: val,
		Opt:   opt,
	}, nil
}

func isNone(val ast.Value) bool {
	switch val := val.(type) {
	case *ast.None:
		return true
	case *ast.Fusion:
		_, ok := val.Value.(*ast.None)
		return ok
	}
	return false
}

func isOption(val ast.Value) bool {
	switch val := val.(type) {
	case *ast.Primitive:
		fmt.Printf("%#v\n", val)
	case *ast.None:
		return isOptionType(val.Type)
	case *ast.Fusion:
		return isOption(val.Value)
	}
	return false
}

func isOptionType(typ ast.Type) bool {
	u, ok := typ.(*ast.TypeUnion)
	return ok && slices.ContainsFunc(u.Types, func(t ast.Type) bool {
		p, ok := t.(*ast.TypePrimitive)
		return ok && p.Name == "none"
	})
}

func (p *Parser) matchSymbol() (string, bool, error) {
	s, ok, err := p.matchString()
	if err != nil {
		return "", false, noEOF(err)
	}
	if ok {
		return s, true, nil
	}
	s, err = p.matchIdentifier()
	if err != nil || s == "" {
		return "", false, err
	}
	return s, true, nil
}

func (p *Parser) matchArray() (*ast.Array, error) {
	l := p.lexer
	if ok, err := l.match('['); !ok || err != nil {
		return nil, noEOF(err)
	}
	vals, err := p.matchValueList()
	if err != nil {
		return nil, err
	}
	ok, err := l.match(']')
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, p.error("mismatched brackets while parsing array type")
	}
	return &ast.Array{
		Kind:     "Array",
		Elements: vals,
	}, nil
}

func (p *Parser) matchValueList() ([]ast.Value, error) {
	l := p.lexer
	var vals []ast.Value
	for {
		val, err := p.matchValue()
		if err != nil {
			return nil, err
		}
		if val == nil {
			break
		}
		vals = append(vals, val)
		ok, err := l.match(',')
		if err != nil {
			return nil, err
		}
		if !ok {
			break
		}
	}
	return vals, nil
}

func (p *Parser) matchSetOrMap() (ast.Value, error) {
	l := p.lexer
	lookahead, err := l.peek(4)
	if err != nil {
		return nil, noEOF(err)
	}
	switch lookahead {
	case "set[":
		l.skip(4)
		vals, err := p.matchValueList()
		if err != nil {
			return nil, err
		}
		ok, err := l.match(']')
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, p.error("mismatched set value brackets")
		}
		return &ast.Set{
			Kind:     "Set",
			Elements: vals,
		}, nil
	case "map{":
		l.skip(4)
		entries, err := p.matchMapEntries()
		if err != nil {
			return nil, err
		}
		ok, err := l.match('}')
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, p.error("mismatched map value brackets")
		}
		return &ast.Map{
			Kind:    "Map",
			Entries: entries,
		}, nil
	default:
		return nil, nil
	}
}

func (p *Parser) matchMapEntries() ([]ast.Entry, error) {
	var entries []ast.Entry
	for {
		entry, err := p.parseEntry()
		if err != nil {
			return nil, err
		}
		if entry == nil {
			break
		}
		entries = append(entries, *entry)
		ok, err := p.lexer.match(',')
		if err != nil {
			return nil, err
		}
		if !ok {
			break
		}
	}
	return entries, nil
}

func (p *Parser) parseEntry() (*ast.Entry, error) {
	key, err := p.matchValue()
	if err != nil {
		return nil, err
	}
	if key == nil {
		// no match
		return nil, nil
	}
	ok, err := p.lexer.match(':')
	if err != nil {

		return nil, err
	}
	if !ok {
		return nil, p.error("no colon found after map key while parsing map entry")
	}
	val, err := p.ParseValue()
	if err != nil {
		return nil, err
	}
	return &ast.Entry{
		Key:   key,
		Value: val,
	}, nil
}

func (p *Parser) matchError(name string) (*ast.Error, error) {
	if name != "error" {
		return nil, nil
	}
	l := p.lexer
	if ok, err := l.match('('); !ok || err != nil {
		return nil, noEOF(err)
	}
	val, err := p.matchValue()
	if err != nil {
		return nil, noEOF(err)
	}
	if ok, err := l.match(')'); !ok || err != nil {
		return nil, noEOF(err)
	}
	return &ast.Error{
		Kind:  "Error",
		Value: val,
	}, nil
}

func (p *Parser) matchTypeDecl(keyword string) (*ast.TypeDecl, error) {
	if keyword != "type" {
		return nil, nil
	}
	name, ok, err := p.matchSymbol()
	if err != nil {
		if err == io.EOF {
			return nil, errors.New("incomplete type definition at EOF")
		}
		return nil, err
	}
	if !ok {
		return nil, nil
	}
	l := p.lexer
	if ok, err := l.match('='); !ok || err != nil {
		if err == io.EOF {
			return nil, errors.New("incomplete type definition at EOF")
		}
		return nil, err
	}
	typ, err := p.matchType()
	if err != nil {
		if err == io.EOF {
			return nil, errors.New("incomplete type definition at EOF")
		}
		return nil, err
	}
	return &ast.TypeDecl{
		Kind: "TypeDecl",
		Name: &ast.ID{Name: name},
		Type: typ,
	}, nil
}

func (p *Parser) matchFusion(name string) (*ast.Fusion, error) {
	if name != "fusion" {
		return nil, nil
	}
	l := p.lexer
	if ok, err := l.match('('); !ok || err != nil {
		return nil, noEOF(err)
	}
	val, err := p.matchValue()
	if err != nil {
		return nil, noEOF(err)
	}
	if ok, err := l.match(','); !ok || err != nil {
		return nil, noEOF(err)
	}
	tv, err := p.matchTypeValue()
	if err != nil {
		return nil, noEOF(err)
	}
	if ok, err := l.match(')'); !ok || err != nil {
		return nil, noEOF(err)
	}
	return &ast.Fusion{
		Kind:  "Fusion",
		Value: val,
		Type:  tv,
	}, nil
}

func (p *Parser) matchSome(name string) (*ast.Some, error) {
	if name != "some" {
		return nil, nil
	}
	l := p.lexer
	if ok, err := l.match('('); !ok || err != nil {
		return nil, noEOF(err)
	}
	val, err := p.matchValue()
	if err != nil {
		return nil, noEOF(err)
	}
	if ok, err := l.match(')'); !ok || err != nil {
		return nil, noEOF(err)
	}
	return &ast.Some{
		Kind:  "Some",
		Value: val,
	}, nil
}

func (p *Parser) matchTypeValue() (*ast.TypeValue, error) {
	l := p.lexer
	if ok, err := l.match('<'); !ok || err != nil {
		return nil, noEOF(err)
	}
	typ, err := p.parseType()
	if err != nil {
		return nil, err
	}
	ok, err := l.match('>')
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, p.error("mismatched parentheses while parsing type value")
	}
	return &ast.TypeValue{
		Kind:  "TypeValue",
		Value: typ,
	}, nil
}

func ParsePrimitive(typeText, valText string) (super.Value, error) {
	typ := super.LookupPrimitive(typeText)
	if typ == nil {
		return super.Null, fmt.Errorf("no such type: %s", typeText)
	}
	var b scode.Builder
	if err := BuildPrimitive(&b, Primitive{typ: typ, text: valText}); err != nil {
		return super.Null, err
	}
	it := b.Bytes().Iter()
	return super.NewValue(typ, it.Next()), nil
}
