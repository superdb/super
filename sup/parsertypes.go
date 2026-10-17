package sup

import (
	"errors"
	"unicode"

	"github.com/superdb/super"
	"github.com/superdb/super/compiler/ast"
)

func (p *Parser) parseType() (ast.Type, error) {
	typ, err := p.matchType()
	if typ == nil && err == nil {
		err = p.error("couldn't parse type")
	}
	return typ, err
}

func (p *Parser) matchType() (ast.Type, error) {
	typ, err := p.matchTypeComponent()
	if err != nil {
		return nil, err
	}
	if ok, _ := p.lexer.match('|'); ok {
		return p.matchTypeUnion(typ)
	}
	return typ, nil
}

func (p *Parser) matchTypeComponent() (ast.Type, error) {
	if typ, err := p.matchTypeRecord(); typ != nil || err != nil {
		return typ, err
	}
	if typ, err := p.matchTypeArray(); typ != nil || err != nil {
		return typ, err
	}
	if typ, err := p.matchTypeSetOrMap(); typ != nil || err != nil {
		return typ, err
	}
	if typ, err := p.matchTypeParens(); typ != nil || err != nil {
		return typ, err
	}
	if typ, err := p.matchTypeName(); typ != nil || err != nil {
		return typ, err
	}
	// no match
	return nil, nil
}

func (p *Parser) matchIdentifier() (string, error) {
	l := p.lexer
	if err := l.skipSpace(); err != nil {
		return "", err
	}
	r, _, err := l.peekRune()
	if err != nil || !idChar(r) {
		return "", err
	}
	return l.scanIdentifier()
}

func (p *Parser) matchTypeName() (ast.Type, error) {
	l := p.lexer
	if err := l.skipSpace(); err != nil {
		return nil, err
	}
	r, _, err := l.peekRune()
	if err != nil {
		return nil, err
	}
	if !(idChar(r) || unicode.IsDigit(r) || r == '"') {
		return nil, nil
	}
	name, err := l.scanTypeName()
	if err != nil {
		return nil, err
	}
	if name == "error" {
		return p.matchTypeErrorBody()
	}
	if name == "fusion" {
		return p.matchTypeFusionBody()
	}
	if name == "option" {
		return p.matchTypeOptionBody()
	}
	if name == "enum" {
		return p.matchTypeEnumBody()
	}
	if name == "any" {
		return &ast.TypeFusion{
			Kind: "TypeFusion",
			Type: &ast.TypePrimitive{Kind: "TypePrimitive", Name: "all"},
		}, nil
	}
	if t := super.LookupPrimitive(name); t != nil {
		return &ast.TypePrimitive{Kind: "TypePrimitive", Name: name}, nil
	}
	return &ast.TypeRef{Kind: "TypeRef", Name: name}, nil
}

func (p *Parser) matchTypeRecord() (*ast.TypeRecord, error) {
	l := p.lexer
	if ok, err := l.match('{'); !ok || err != nil {
		return nil, err
	}
	var fields []ast.TypeField
	for {
		field, err := p.matchTypeField()
		if err != nil {
			return nil, err
		}
		if field == nil {
			break
		}
		fields = append(fields, *field)
		ok, err := l.match(',')
		if err != nil {
			return nil, err
		}
		if !ok {
			break
		}
	}
	ok, err := l.match('}')
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, p.error("mismatched braces while parsing record type")
	}
	return &ast.TypeRecord{
		Kind:   "TypeRecord",
		Fields: fields,
	}, nil
}

func (p *Parser) matchTypeField() (*ast.TypeField, error) {
	l := p.lexer
	symbol, ok, err := p.matchSymbol()
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
		return nil, p.errorf("no type name found for field %q", symbol)
	}
	typ, err := p.parseType()
	if err != nil {
		return nil, err
	}
	return &ast.TypeField{
		Name: symbol,
		Type: typ,
		Opt:  opt,
	}, nil
}

func (p *Parser) matchTypeArray() (*ast.TypeArray, error) {
	l := p.lexer
	if ok, err := l.match('['); !ok || err != nil {
		return nil, err
	}
	typ, err := p.matchType()
	if err != nil {
		return nil, err
	}
	if typ == nil {
		typ = &ast.TypePrimitive{Kind: "TypePrimitive", Name: "missing"}
	}
	ok, err := l.match(']')
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, p.error("mismatched brackets while parsing array type")
	}
	return &ast.TypeArray{
		Kind: "TypeArray",
		Type: typ,
	}, nil
}

func (p *Parser) matchTypeSetOrMap() (ast.Type, error) {
	l := p.lexer
	lookahead, err := l.peek(4)
	if err != nil {
		return nil, noEOF(err)
	}
	switch lookahead {
	case "set[":
		l.skip(4)
		inner, err := p.matchType()
		if err != nil {
			return nil, err
		}
		if inner == nil {
			inner = &ast.TypePrimitive{Kind: "TypePrimitive", Name: "all"}
		}
		ok, err := l.match(']')
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, p.error("mismatched set-brackets while parsing set type")
		}
		return &ast.TypeSet{
			Kind: "TypeSet",
			Type: inner,
		}, nil
	case "map{":
		l.skip(4)
		typ, err := p.parseTypeMap()
		if err != nil {
			return nil, err
		}
		ok, err := l.match('}')
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, p.error("mismatched set-brackets while parsing map type")
		}
		return typ, nil
	default:
		return nil, nil
	}
}

func (p *Parser) parseTypeMap() (*ast.TypeMap, error) {
	keyType, err := p.matchType()
	if err != nil {
		return nil, err
	}
	ok, err := p.lexer.match(':')
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, p.error("value type missing while parsing map type")
	}
	valType, err := p.parseType()
	if err != nil {
		return nil, err
	}
	return &ast.TypeMap{
		Kind:    "TypeMap",
		KeyType: keyType,
		ValType: valType,
	}, nil
}

func (p *Parser) matchTypeParens() (ast.Type, error) {
	l := p.lexer
	if ok, err := l.match('('); !ok || err != nil {
		return nil, err
	}
	typ, err := p.matchType()
	if err != nil {
		return nil, err
	}
	if ok, _ := l.match('='); ok {
		return nil, p.error("value embedded type declarations at '=' no longer supported")
	}

	ok, err := l.match(')')
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, p.error("mismatched parentheses while parsing parenthesized type")
	}
	return typ, nil
}

func (p *Parser) matchTypeBody(which string) (ast.Type, error) {
	l := p.lexer
	ok, err := l.match('(')
	if !ok {
		return nil, p.errorf("no opening parenthesis in %s type", which)
	}
	if err != nil {
		return nil, err
	}
	typ, err := p.matchType()
	if err != nil {
		return nil, err
	}
	ok, err = l.match(')')
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, p.errorf("mismatched parentheses while parsing %s type", which)
	}
	return typ, nil
}

func (p *Parser) matchTypeUnion(first ast.Type) (*ast.TypeUnion, error) {
	l := p.lexer
	var types []ast.Type
	if first != nil {
		types = append(types, first)
	}
	for {
		typ, err := p.matchTypeComponent()
		if err != nil {
			return nil, err
		}
		if typ == nil {
			break
		}
		types = append(types, typ)
		ok, err := l.match('|')
		if noEOF(err) != nil {
			return nil, err
		}
		if !ok {
			break
		}
	}
	if len(types) < 2 {
		if ok, _ := l.match(','); ok {
			return nil, errors.New("union components are separated by pipe symbol (|) not comma")
		}
		return nil, errors.New("union type must include two or more types")
	}
	return &ast.TypeUnion{
		Kind:  "TypeUnion",
		Types: types,
	}, nil
}

func (p *Parser) matchTypeEnumBody() (*ast.TypeEnum, error) {
	l := p.lexer
	if ok, err := l.match('('); !ok || err != nil {
		return nil, errors.New("no opening parenthesis in enum type")
	}
	fields, err := p.matchEnumSymbols()
	if err != nil {
		return nil, err
	}
	ok, err := l.match(')')
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, p.error("mismatched parentheses while parsing enum type")
	}
	return &ast.TypeEnum{
		Kind:    "TypeEnum",
		Symbols: fields,
	}, nil
}

func (p *Parser) matchEnumSymbols() ([]*ast.Text, error) {
	l := p.lexer
	var symbols []*ast.Text
	for {
		name, ok, err := p.matchSymbol()
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, nil
		}
		symbols = append(symbols, &ast.Text{Kind: "Text", Text: name})
		ok, err = l.match(',')
		if err != nil {
			return nil, err
		}
		if !ok {
			break
		}
	}
	return symbols, nil
}

func (p *Parser) matchTypeErrorBody() (*ast.TypeError, error) {
	typ, err := p.matchTypeBody("error")
	if err != nil {
		return nil, err
	}
	return &ast.TypeError{
		Kind: "TypeError",
		Type: typ,
	}, nil
}

func (p *Parser) matchTypeFusionBody() (*ast.TypeFusion, error) {
	typ, err := p.matchTypeBody("fusion")
	if err != nil {
		return nil, err
	}
	v := &ast.TypeFusion{
		Kind: "TypeFusion",
		Type: typ,
	}
	return v, nil
}

func (p *Parser) matchTypeOptionBody() (*ast.TypeOption, error) {
	typ, err := p.matchTypeBody("option")
	if err != nil {
		return nil, err
	}
	return &ast.TypeOption{
		Kind: "TypeOption",
		Type: typ,
	}, nil
}
