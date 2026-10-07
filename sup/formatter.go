package sup

import (
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/superdb/super"
	"github.com/superdb/super/pkg/terminal/color"
	"github.com/superdb/super/scode"
)

type StreamFormatter struct {
	formatter
	decls map[string]struct{}
}

func NewStreamFormatter(pretty int, colorDisabled bool) *StreamFormatter {
	return &StreamFormatter{
		formatter: *newFormatter(pretty, colorDisabled),
		decls:     make(map[string]struct{}),
	}
}

func (s *StreamFormatter) FormatValue(val super.Value) string {
	s.builder.Reset()
	if s.formatTypeDecls(val.Type()) {
		s.formatTypeValueDecls(val)
	}
	s.formatValueAndDecorate(val.Type(), val.Bytes())
	return s.builder.String()
}

// Emit type declarations for each named type in typ that has not been
// previously emitted.  Also, returns true if there are any type types
// so that the type values can be subsequently parsed for named types.
func (s *StreamFormatter) formatTypeDecls(typ super.Type) bool {
	var hasTypeVal bool
	switch typ := typ.(type) {
	case *super.TypeOfType:
		return true
	case *super.TypeNamed:
		if _, ok := s.decls[typ.Name]; !ok {
			s.decls[typ.Name] = struct{}{}
			hasTypeVal = s.formatTypeDecls(typ.Type)
			s.formatTypeDecl(typ)
		}
	case *super.TypeRecord:
		for _, field := range typ.Fields {
			if s.formatTypeDecls(field.Type) {
				hasTypeVal = true
			}
		}
	case *super.TypeArray:
		return s.formatTypeDecls(typ.Type)
	case *super.TypeSet:
		return s.formatTypeDecls(typ.Type)
	case *super.TypeUnion:
		for _, typ := range typ.Types {
			if s.formatTypeDecls(typ) {
				hasTypeVal = true
			}
		}
	case *super.TypeMap:
		return s.formatTypeDecls(typ.KeyType) || s.formatTypeDecls(typ.ValType)
	case *super.TypeError:
		return s.formatTypeDecls(typ.Type)
	case *super.TypeFusion:
		return s.formatTypeDecls(typ.Type)
	case *super.TypeOption:
		return s.formatTypeDecls(typ.Type)
	}
	return hasTypeVal
}

func (s *StreamFormatter) formatTypeValueDecls(val super.Value) {
	val.Walk(func(typ super.Type, body scode.Bytes) error {
		if super.TypeUnder(typ) == super.TypeType {
			typ, err := s.sctx().LookupByValue(body)
			if err != nil {
				panic(err) //XXX
			}
			s.formatTypeDecls(typ)
		}
		return nil
	})
}

func (s *StreamFormatter) formatTypeDecl(typ *super.TypeNamed) {
	var space string
	if s.tab != 0 {
		space = " "
	}
	s.buildf("type %s%s=%s", QuotedName(typ.Name), space, space)
	s.formatType(0, typ.Type, false)
	s.build("\n")
}

type formatter struct {
	formatterT
	sctx_   *super.Context
	implied map[super.Type]bool
}

type formatterT struct {
	tab           int
	newline       string
	builder       strings.Builder
	colors        color.Stack
	colorDisabled bool
}

func newFormatter(pretty int, colorDisabled bool) *formatter {
	return &formatter{
		implied:    make(map[super.Type]bool),
		formatterT: *newFormatterT(pretty, colorDisabled),
	}
}

func newFormatterT(pretty int, colorDisabled bool) *formatterT {
	var newline string
	if pretty > 0 {
		newline = "\n"
	}
	return &formatterT{
		tab:           pretty,
		newline:       newline,
		colorDisabled: colorDisabled,
	}
}

func (f *formatter) FormatValue(val super.Value) string {
	f.builder.Reset()
	f.formatValueAndDecorate(val.Type(), val.Bytes())
	return f.builder.String()
}

func (f *formatter) sctx() *super.Context {
	if f.sctx_ == nil {
		f.sctx_ = super.NewContext()
	}
	return f.sctx_
}

func (f *formatter) hasName(typ super.Type) bool {
	return f.nameOf(typ) != ""
}

func (f *formatter) nameOf(typ super.Type) string {
	var name string
	if named, ok := typ.(*super.TypeNamed); ok {
		name = named.Name
	}
	return name
}

func (f *formatter) formatValueAndDecorate(typ super.Type, bytes scode.Bytes) {
	known := f.hasName(typ)
	f.formatValue(0, typ, bytes, known, false, false)
	f.decorate(typ, false, isOptionNone(typ, bytes), 0)
}

func isOptionNone(typ super.Type, bytes scode.Bytes) bool {
	if typ, ok := typ.(*super.TypeOption); ok {
		typ, _ := typ.Decode(bytes)
		return super.TypeUnder(typ) == super.TypeNone
	}
	return false
}

func (f *formatter) formatValue(indent int, typ super.Type, bytes scode.Bytes, parentKnown, decorate, isOptField bool) {
	known := parentKnown || f.hasName(typ)
	var empty, isOptionNone bool
	switch t := typ.(type) {
	default:
		f.startColorPrimitive(typ)
		formatPrimitive(&f.builder, typ, bytes)
		f.endColor()
	case *super.TypeNamed:
		f.formatValue(indent, t.Type, bytes, known, false, isOptField)
	case *super.TypeRecord:
		f.formatRecord(indent, t, bytes, known)
	case *super.TypeArray:
		empty = f.formatElems(indent, "[", "]", t.Type, super.NewValue(t, bytes), known)
	case *super.TypeSet:
		empty = f.formatElems(indent, "set[", "]", t.Type, super.NewValue(t, bytes), known)
	case *super.TypeUnion:
		f.formatUnion(indent, t, bytes)
	case *super.TypeMap:
		empty = f.formatMap(indent, t, bytes, known)
	case *super.TypeEnum:
		f.build("\"")
		f.build(t.Symbols[super.DecodeUint(bytes)])
		f.build("\"")
	case *super.TypeError:
		f.startColor(color.Red)
		f.build("error")
		f.endColor()
		f.build("(")
		f.formatValue(indent, t.Type, bytes, known, true, false)
		f.build(")")
	case *super.TypeFusion:
		it := bytes.Iter()
		if super.IsTypeAny(t) {
			bytes := it.Next()
			typ, err := f.sctx().LookupByValue(it.Next())
			if err != nil {
				panic(err)
			}
			f.formatValue(indent, typ, bytes, false, true, isOptField)
			f.startColor(color.Gray(200))
			f.build("::any")
			f.endColor()
			return
		}
		f.startColor(color.Green)
		f.build("fusion")
		f.endColor()
		f.build("(")
		f.formatValue(indent, t.Type, it.Next(), known, true, false)
		f.build(",")
		f.formatTypeValue(indent, it.Next())
		f.build(")")
		// We don't need to decorate a fusion value because
		// its type is always implied by its value.
		return
	case *super.TypeOption:
		typ, bytes := t.Decode(bytes)
		if typ == super.TypeNone {
			f.build("none")
			isOptionNone = true
		} else {
			if !isOptField {
				f.build("some(")
			}
			f.formatValue(indent, t.Type, bytes, known, true, false)
			if !isOptField {
				f.build(")")
			}
		}
	case *super.TypeOfType:
		f.startColor(color.Gray(200))
		f.formatTypeValue(indent, bytes)
		f.endColor()
	}
	if decorate && !parentKnown {
		f.decorate(typ, empty, isOptionNone, indent)
	}
}

func (f *formatter) formatTypeValue(indent int, bytes scode.Bytes) {
	typ, err := f.sctx().LookupByValue(bytes)
	if err != nil {
		panic(err)
	}
	f.startColor(color.Gray(160))
	if isShortType(typ) {
		f.build("<")
		f.formatType(indent, typ, false)
		f.build(">")
	} else {
		f.build("<")
		f.build(f.newline)
		indent += f.tab
		f.indent(indent, "")
		f.formatType(indent, typ, false)
		f.indent(indent-f.tab, ">")
	}
	f.endColor()
}

func isShortType(typ super.Type) bool {
	if named, ok := typ.(*super.TypeNamed); ok {
		return len(named.Name) < 20
	}
	if super.IsPrimitiveType(typ) {
		return true
	}
	switch typ := typ.(type) {
	case *super.TypeRecord:
		if len(typ.Fields) <= 4 {
			for _, f := range typ.Fields {
				if !isShortType(f.Type) || len(f.Name) > 12 {
					return false
				}
			}
			return true
		}
	case *super.TypeArray:
		return isShortType(typ.Type)
	case *super.TypeSet:
		return isShortType(typ.Type)
	case *super.TypeMap:
		return isShortType(typ.KeyType) && isShortType(typ.ValType)
	case *super.TypeUnion:
		if len(typ.Types) <= 6 {
			for _, t := range typ.Types {
				if !isShortType(t) {
					return false
				}
			}
			return true
		}
	case *super.TypeError:
		return isShortType(typ.Type)
	case *super.TypeFusion:
		return isShortType(typ.Type)
	case *super.TypeOption:
		return isShortType(typ.Type)
	}
	return false
}

func (f *formatter) decorate(typ super.Type, empty, isTypedNone bool, indent int) {
	if (!empty && f.isImplied(typ)) || (empty && innerNone(typ)) || (super.IsOptionType(typ) && !f.hasName(typ) && !isTypedNone) {
		return
	}
	f.startColor(color.Gray(200))
	defer f.endColor()
	if name := f.nameOf(typ); name != "" {
		f.buildf("::%s", quoteHexyString(QuotedTypeName(name)))
		return
	}
	if !empty && SelfDescribing(typ) && !isTypedNone {
		return
	}
	f.build("::")
	if isTypedNone {
		// For nicer syntax, typed nones are decorated with the type under the
		// option type instead of the outer option type as a decoration on a none
		// value implies an option type.
		optionType := typ.(*super.TypeOption)
		if _, ok := optionType.Type.(*super.TypeUnion); ok {
			// To distinguish an option union decorator from a union with a pure none,
			// we wrap the union with the option detail, as in option(T1|T2...)
			// This only applies to none values because of the implied option()
			// when decorating a none value.  We know the union will be wrapped in
			// parens so we can simply emit "option" here to get "option(T1|...)"".
			f.build("option")
		}
		typ = optionType.Type
	}
	f.formatType(indent, typ, true)
}

func innerNone(typ super.Type) bool {
	switch typ := typ.(type) {
	case *super.TypeSet:
		return typ.Type == super.TypeNone
	case *super.TypeArray:
		return typ.Type == super.TypeNone
	case *super.TypeMap:
		return typ.KeyType == super.TypeNone && typ.ValType == super.TypeNone
	}
	return false
}

func (f *formatter) isImplied(typ super.Type) bool {
	implied, ok := f.implied[typ]
	if !ok {
		implied = Implied(typ)
		f.implied[typ] = implied
	}
	return implied
}

func (f *formatter) formatRecord(indent int, typ *super.TypeRecord, bytes scode.Bytes, known bool) {
	f.build("{")
	if len(typ.Fields) == 0 {
		f.build("}")
		return
	}
	indent += f.tab
	sep := f.newline
	it := bytes.Iter()
	for _, field := range typ.Fields {
		f.build(sep)
		f.startColor(color.Blue)
		f.indent(indent, QuotedName(field.Name))
		isOptField := super.IsOptionType(field.Type)
		if isOptField {
			f.build("?")
		}
		f.endColor()
		f.build(":")
		if f.tab > 0 {
			f.build(" ")
		}
		elem := it.Next()
		f.formatValue(indent, field.Type, elem, known, true, isOptField)
		sep = "," + f.newline
	}
	f.build(f.newline)
	f.indent(indent-f.tab, "}")
}

func (f *formatter) formatElems(indent int, open, close string, inner super.Type, val super.Value, known bool) bool {
	f.build(open)
	n, ok := val.ContainerLength()
	if !ok {
		panic(val.Type())
	}
	if n == 0 {
		f.build(close)
		return true
	}
	indent += f.tab
	sep := f.newline
	it := val.ContainerIter()
	elems := newElemBuilder(inner)
	for !it.Done() {
		f.build(sep)
		f.indent(indent, "")
		typ, b := elems.add(it.Next())
		f.formatValue(indent, typ, b, known, true, false)
		sep = "," + f.newline
	}
	f.build(f.newline)
	f.indent(indent-f.tab, close)
	if elems.needsDecoration() {
		// If we haven't seen all the types in the union, print the decorator
		// so the fullness of the union is persevered.
		f.decorate(val.Type(), true, false, indent)
	}
	return false
}

type elemHelper struct {
	typ   super.Type
	union *super.TypeUnion
	seen  map[super.Type]struct{}
}

func newElemBuilder(typ super.Type) *elemHelper {
	union, _ := super.TypeUnder(typ).(*super.TypeUnion)
	return &elemHelper{typ: typ, union: union, seen: make(map[super.Type]struct{})}
}

func (e *elemHelper) add(b scode.Bytes) (super.Type, scode.Bytes) {
	if e.union == nil {
		return e.typ, b
	}
	typ, b := e.union.Untag(b)
	if _, ok := e.seen[typ]; !ok {
		e.seen[typ] = struct{}{}
	}
	return typ, b
}

func (e *elemHelper) needsDecoration() bool {
	return e.union != nil && (super.IsTypeNamed(e.typ) || len(e.seen) < len(e.union.Types))
}

func (f *formatter) formatUnion(indent int, union *super.TypeUnion, bytes scode.Bytes) {
	typ, bytes := union.Untag(bytes)
	// XXX For now, we always decorate a union value so that
	// we can determine the tag from the value's explicit type.
	// We can later optimize this so we only print the decorator if its
	// ambigous with another type (e.g., int8 and int16 vs a union of int8 and string).
	// Let's do this after we have the parser working and capable of this
	// disambiguation.  See issue #1764.
	// In other words, just because we known the union's type doesn't mean
	// we know the type of a particular value of that union.
	const known = false
	f.formatValue(indent, typ, bytes, known, true, false)
}

func (f *formatter) formatMap(indent int, typ *super.TypeMap, bytes scode.Bytes, known bool) bool {
	empty := true
	f.build("map{")
	indent += f.tab
	sep := f.newline
	keyElems := newElemBuilder(typ.KeyType)
	valElems := newElemBuilder(typ.ValType)
	for it := bytes.Iter(); !it.Done(); {
		keyBytes := it.Next()
		empty = false
		f.build(sep)
		f.indent(indent, "")
		var keyType super.Type
		keyType, keyBytes = keyElems.add(keyBytes)
		f.formatValue(indent, keyType, keyBytes, known, true, false)
		if super.TypeUnder(keyType) == super.TypeIP && len(keyBytes) == 16 {
			// To avoid ambiguity, whitespace must separate an IPv6
			// map key from the colon that follows it.
			f.build(" ")
		}
		f.build(":")
		if f.tab > 0 {
			f.build(" ")
		}
		valType, valBytes := valElems.add(it.Next())
		f.formatValue(indent, valType, valBytes, known, true, false)
		sep = "," + f.newline
	}
	f.build(f.newline)
	f.indent(indent-f.tab, "}")
	if keyElems.needsDecoration() || valElems.needsDecoration() {
		f.decorate(typ, true, false, indent)
	}
	return empty
}

func formatPrimitive(b *strings.Builder, typ super.Type, bytes scode.Bytes) {
	switch typ := typ.(type) {
	case *super.TypeOfUint8, *super.TypeOfUint16, *super.TypeOfUint32, *super.TypeOfUint64:
		b.WriteString(strconv.FormatUint(super.DecodeUint(bytes), 10))
	case *super.TypeOfInt8, *super.TypeOfInt16, *super.TypeOfInt32, *super.TypeOfInt64:
		b.WriteString(strconv.FormatInt(super.DecodeInt(bytes), 10))
	case *super.TypeOfDuration:
		b.WriteString(super.DecodeDuration(bytes).String())
	case *super.TypeOfTime:
		b.WriteString(super.DecodeTime(bytes).Time().Format(time.RFC3339Nano))
	case *super.TypeOfFloat16:
		f := super.DecodeFloat16(bytes)
		if f == float32(int64(f)) {
			b.WriteString(fmt.Sprintf("%d.", int64(f)))
		} else {
			b.WriteString(strconv.FormatFloat(float64(f), 'g', -1, 32))
		}
	case *super.TypeOfFloat32:
		f := super.DecodeFloat32(bytes)
		if f == float32(int64(f)) {
			b.WriteString(fmt.Sprintf("%d.", int64(f)))
		} else {
			b.WriteString(strconv.FormatFloat(float64(f), 'g', -1, 32))
		}
	case *super.TypeOfFloat64:
		f := super.DecodeFloat64(bytes)
		if f == float64(int64(f)) {
			b.WriteString(fmt.Sprintf("%d.", int64(f)))
		} else {
			b.WriteString(strconv.FormatFloat(f, 'g', -1, 64))
		}
	case *super.TypeOfBool:
		if super.DecodeBool(bytes) {
			b.WriteString("true")
		} else {
			b.WriteString("false")
		}
	case *super.TypeOfBytes:
		b.WriteString("0x")
		b.WriteString(hex.EncodeToString(bytes))
	case *super.TypeOfString:
		b.WriteString(QuotedString(string(bytes)))
	case *super.TypeOfIP:
		b.WriteString(super.DecodeIP(bytes).String())
	case *super.TypeOfNet:
		b.WriteString(super.DecodeNet(bytes).String())
	case *super.TypeOfType:
		b.WriteString(FormatTypeValue(bytes))
	case *super.TypeOfNull:
		b.WriteString("null")
	case *super.TypeOfNone:
		b.WriteString("none")
	case *super.TypeOfAll:
		// Write out all values as byte encoded as they only place
		// they may appear is inside of a fusion(all), which includes
		// the type to go with the bytes as the fusion subtype.
		b.WriteString("0x")
		b.WriteString(hex.EncodeToString(bytes))
	default:
		panic(fmt.Sprintf("%#v\n", typ))
	}
}

// formatType builds typ as a type string with any needed
// typedefs for named types that have not been previously defined,
// or whose name is redefined to a different type.
// These typedefs use the embedded syntax (name=type-string).
// Typedefs handled by decorators are handled in decorate().
// The routine re-enters the type formatter with a fresh builder by
// invoking push()/pop().
func (f *formatterT) formatType(indent int, typ super.Type, parens bool) {
	if super.TypeID(typ) < super.IDTypeComplex {
		f.build(super.PrimitiveName(typ))
		return
	}
	switch typ := typ.(type) {
	case *super.TypeNamed:
		f.build(QuotedName(typ.Name))
	case *super.TypeRecord:
		f.formatTypeRecord(indent, typ)
	case *super.TypeArray:
		f.build("[")
		f.formatType(indent, typ.Type, false)
		f.build("]")
	case *super.TypeSet:
		f.build("set[")
		f.formatType(indent, typ.Type, false)
		f.build("]")
	case *super.TypeMap:
		f.build("map{")
		newline := f.newline
		tab := f.tab
		indent += tab
		if super.IsPrimitiveType(typ.KeyType) && super.IsPrimitiveType(typ.ValType) {
			tab = 0
			newline = ""
			indent = 0
		}
		f.build(newline)
		f.indent(indent, "")
		f.formatType(indent, typ.KeyType, false)
		f.build(":")
		if tab > 0 {
			f.build(" ")
		}
		f.formatType(indent, typ.ValType, false)
		f.build(newline)
		if newline != "" {
			f.indent(indent-tab, "}")
		} else {
			f.build("}")
		}
	case *super.TypeUnion:
		f.formatTypeUnion(indent, typ, parens)
	case *super.TypeEnum:
		f.formatTypeEnum(typ)
	case *super.TypeError:
		f.build("error(")
		f.formatType(indent, typ.Type, false)
		f.build(")")
	case *super.TypeFusion:
		if super.IsTypeAny(typ) {
			f.build("any")
		} else {
			f.build("fusion(")
			f.formatType(indent, typ.Type, false)
			f.build(")")
		}
	case *super.TypeOption:
		f.build("option(")
		f.formatType(indent, typ.Type, false)
		f.build(")")
	default:
		panic(typ)
	}
}

func (f *formatterT) formatTypeRecord(indent int, typ *super.TypeRecord) {
	f.build("{")
	sep := f.newline
	indent += f.tab
	for _, field := range typ.Fields {
		f.build(sep)
		f.indent(indent, QuotedName(field.Name))
		typ := field.Type
		if option, ok := typ.(*super.TypeOption); ok {
			f.build("?")
			typ = option.Type
		}
		f.build(":")
		if f.tab > 0 {
			f.build(" ")
		}
		f.formatType(indent, typ, false)
		sep = "," + f.newline
	}
	f.build(f.newline)
	f.indent(indent-f.tab, "}")
}

func (f *formatterT) formatTypeUnion(indent int, typ *super.TypeUnion, parens bool) {
	if isShortType(typ) {
		if parens {
			f.build("(")
		}
		sep := ""
		for _, typ := range typ.Types {
			f.build(sep)
			f.formatType(indent, typ, false)
			sep = "|"
		}
		if parens {
			f.build(")")
		}
		return
	}
	if parens || f.tab != 0 {
		f.build("(")
	}
	sep := f.newline
	indent += f.tab
	for _, typ := range typ.Types {
		f.build(sep)
		f.indent(indent, "")
		f.formatType(indent, typ, false)
		sep = "|" + f.newline
	}
	if parens || f.tab != 0 {
		f.build(f.newline)
		f.indent(indent-f.tab, ")")
	}
}

func (f *formatterT) formatTypeEnum(typ *super.TypeEnum) {
	f.build("enum(")
	for k, s := range typ.Symbols {
		if k > 0 {
			f.build(",")
		}
		f.buildf("%s", QuotedName(s))
	}
	f.build(")")
}

var colors = map[super.Type]color.Code{
	super.TypeString: color.Green,
	super.TypeType:   color.Orange,
}

func (f *formatterT) startColorPrimitive(typ super.Type) {
	if !f.colorDisabled {
		c, ok := colors[super.TypeUnder(typ)]
		if !ok {
			c = color.Reset
		}
		f.startColor(c)
	}
}

func (f *formatterT) startColor(code color.Code) {
	if !f.colorDisabled {
		f.colors.Start(&f.builder, code)
	}
}

func (f *formatterT) endColor() {
	if !f.colorDisabled {
		f.colors.End(&f.builder)
	}
}

func FormatType(typ super.Type) string {
	f := newFormatterT(0, true)
	f.formatType(0, typ, false)
	return f.builder.String()
}

func FormatPrimitive(typ super.Type, bytes scode.Bytes) string {
	var b strings.Builder
	formatPrimitive(&b, typ, bytes)
	return b.String()
}

func (f *formatterT) indent(tab int, s string) {
	for range tab {
		f.builder.WriteByte(' ')
	}
	f.build(s)
}

func (f *formatterT) build(s string) {
	f.builder.WriteString(s)
}

func (f *formatterT) buildf(s string, args ...any) {
	f.builder.WriteString(fmt.Sprintf(s, args...))
}

func FormatValue(val super.Value) string {
	return newFormatter(0, true).FormatValue(val)
}

func FormatValueWithTypes(val super.Value) string {
	return NewStreamFormatter(0, true).FormatValue(val)
}

func String(p any) string {
	if typ, ok := p.(super.Type); ok {
		return FormatType(typ)
	}
	switch val := p.(type) {
	case *super.Value:
		return FormatValue(*val)
	case super.Value:
		return FormatValue(val)
	default:
		panic(fmt.Sprintf("sup.String takes a super.Type or *super.Value: %T", val))
	}
}

func FormatTypeValue(tv scode.Bytes) string {
	f := newFormatter(0, true)
	f.formatTypeValue(0, tv)
	return f.builder.String()
}

func quoteHexyString(s string) string {
	if s == "" || len(s) > 4 {
		return s
	}
	for _, c := range s {
		if !((c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F') || (c >= '0' && c <= '9')) {
			return s
		}
	}
	return fmt.Sprintf("%q", s)
}
