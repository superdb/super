package function

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/superdb/super"
	"github.com/superdb/super/runtime/expr"
	"github.com/superdb/super/scode"
	"github.com/superdb/super/vector"
)

// DedupType returns a map of type names to types like the argument of sum_type
// in which each unnamed record type that appears more than once as the type
// of a field with the same name is replaced by a named type, whose name is the
// field name in CamelCase, e.g., the type of field "home_address" becomes
// HomeAddress.
type DedupType struct {
	sctx *super.Context
}

func (d *DedupType) Call(args ...vector.Any) vector.Any {
	vec := args[0]
	if vec, ok := expr.Catch(d.sctx, expr.CatchAll, vec, "dedup_type"); ok {
		return vec
	}
	typ, ok := super.TypeUnder(vec.Type()).(*super.TypeMap)
	if !ok || super.TypeUnder(typ.KeyType) != super.TypeString || super.TypeUnder(typ.ValType) != super.TypeType {
		return vector.NewWrappedError(d.sctx, "dedup_type: argument is not a map of strings to types", vec)
	}
	var sb scode.Builder
	if c, ok := vec.(*vector.Const); ok {
		return vector.NewConstFromValue(d.sctx, d.call(&sb, c, 0), c.Len())
	}
	b := vector.NewDynamicValueBuilder()
	for i := range vec.Len() {
		b.Write(d.call(&sb, vec, i))
	}
	return b.Build(d.sctx)
}

// call returns the deduplicated map for the map at slot of vec.
func (d *DedupType) call(sb *scode.Builder, vec vector.Any, slot uint32) super.Value {
	val := vector.ValueAt(sb, vec, slot)
	var keys []scode.Bytes
	var types []super.Type
	for it := val.ContainerIter(); !it.Done(); {
		keys = append(keys, it.Next())
		typ, err := d.sctx.LookupByValue(it.Next())
		if err != nil {
			panic(err)
		}
		types = append(types, typ)
	}
	dedup := newDeduper(d.sctx)
	for _, typ := range types {
		dedup.count(typ)
	}
	dedup.chooseNames()
	var b scode.Builder
	for i, typ := range types {
		b.Append(keys[i])
		b.Append(d.sctx.LookupTypeValue(dedup.rewrite(typ)).Bytes())
	}
	mapType := d.sctx.LookupTypeMap(super.TypeString, super.TypeType)
	return super.NewValue(mapType, b.Bytes())
}

// candidate is an unnamed record type that's the type of a field (or the type
// of an optional field) whose name in CamelCase is name.
type candidate struct {
	name string
	typ  *super.TypeRecord
}

type deduper struct {
	sctx   *super.Context
	counts map[candidate]int
	// names holds the candidates that are replaced by a named type.
	names map[candidate]struct{}
	// rewritten memoizes rewrite.
	rewritten map[super.Type]super.Type
}

func newDeduper(sctx *super.Context) *deduper {
	return &deduper{
		sctx:      sctx,
		counts:    make(map[candidate]int),
		names:     make(map[candidate]struct{}),
		rewritten: make(map[super.Type]super.Type),
	}
}

// count counts the candidates in typ.  A named type is a barrier since its
// body can't change without changing what the name means.
func (d *deduper) count(typ super.Type) {
	switch typ := typ.(type) {
	case *super.TypeRecord:
		for _, f := range typ.Fields {
			if c, ok := fieldCandidate(f); ok {
				d.counts[c]++
			}
			d.count(f.Type)
		}
	case *super.TypeArray:
		d.count(typ.Type)
	case *super.TypeSet:
		d.count(typ.Type)
	case *super.TypeMap:
		d.count(typ.KeyType)
		d.count(typ.ValType)
	case *super.TypeUnion:
		for _, typ := range typ.Types {
			d.count(typ)
		}
	case *super.TypeOption:
		d.count(typ.Type)
	case *super.TypeError:
		d.count(typ.Type)
	case *super.TypeFusion:
		d.count(typ.Type)
	}
}

// chooseNames selects the candidates that appear more than once.  If two
// such candidates with different types want the same name, neither is named
// since there's no principled way to choose between them.
func (d *deduper) chooseNames() {
	byName := make(map[string][]candidate)
	for c, n := range d.counts {
		if n > 1 {
			byName[c.name] = append(byName[c.name], c)
		}
	}
	for _, cands := range byName {
		if len(cands) == 1 {
			d.names[cands[0]] = struct{}{}
		}
	}
}

// rewrite returns typ with each chosen candidate replaced by its named type.
func (d *deduper) rewrite(typ super.Type) super.Type {
	if out, ok := d.rewritten[typ]; ok {
		return out
	}
	out := typ
	switch typ := typ.(type) {
	case *super.TypeRecord:
		fields := make([]super.Field, 0, len(typ.Fields))
		for _, f := range typ.Fields {
			fields = append(fields, super.NewField(f.Name, d.rewriteField(f)))
		}
		out = d.sctx.MustLookupTypeRecord(fields)
	case *super.TypeArray:
		out = d.sctx.LookupTypeArray(d.rewrite(typ.Type))
	case *super.TypeSet:
		out = d.sctx.LookupTypeSet(d.rewrite(typ.Type))
	case *super.TypeMap:
		out = d.sctx.LookupTypeMap(d.rewrite(typ.KeyType), d.rewrite(typ.ValType))
	case *super.TypeUnion:
		types := make([]super.Type, 0, len(typ.Types))
		for _, typ := range typ.Types {
			types = append(types, d.rewrite(typ))
		}
		out = d.sctx.MustLookupTypeUnion(types)
	case *super.TypeOption:
		out = d.sctx.LookupTypeOption(d.rewrite(typ.Type))
	case *super.TypeError:
		out = d.sctx.LookupTypeError(d.rewrite(typ.Type))
	case *super.TypeFusion:
		if fusion, err := d.sctx.LookupTypeFusion(d.rewrite(typ.Type)); err == nil {
			out = fusion
		}
	}
	d.rewritten[typ] = out
	return out
}

// rewriteField returns the rewritten type of field f, which is a named type
// (or an option of a named type) if f's type is a chosen candidate.
func (d *deduper) rewriteField(f super.Field) super.Type {
	c, ok := fieldCandidate(f)
	if !ok {
		return d.rewrite(f.Type)
	}
	if _, ok := d.names[c]; !ok {
		return d.rewrite(f.Type)
	}
	// If c.name is already bound to a different type, LookupTypeNamed
	// fails and the candidate is left as is.
	named, err := d.sctx.LookupTypeNamed(c.name, d.rewrite(c.typ))
	if err != nil {
		return d.rewrite(f.Type)
	}
	if _, ok := f.Type.(*super.TypeOption); ok {
		return d.sctx.LookupTypeOption(named)
	}
	return named
}

// fieldCandidate returns the candidate for field f if its type is an unnamed
// record type or an option of one (i.e., an optional field).
func fieldCandidate(f super.Field) (candidate, bool) {
	typ := f.Type
	if option, ok := typ.(*super.TypeOption); ok {
		typ = option.Type
	}
	rec, ok := typ.(*super.TypeRecord)
	if !ok {
		return candidate{}, false
	}
	name := camelCase(f.Name)
	if name == "" {
		return candidate{}, false
	}
	return candidate{name, rec}, true
}

// camelCase returns name in CamelCase, where the words of name are separated
// by underscores or other characters that aren't letters or digits, e.g.,
// "home_address" is HomeAddress.
func camelCase(name string) string {
	var b strings.Builder
	for _, word := range strings.FieldsFunc(name, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	}) {
		r, n := utf8.DecodeRuneInString(word)
		b.WriteRune(unicode.ToUpper(r))
		b.WriteString(word[n:])
	}
	return b.String()
}
