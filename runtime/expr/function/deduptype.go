package function

import (
	"fmt"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/superdb/super"
	"github.com/superdb/super/runtime/expr"
	"github.com/superdb/super/scode"
	"github.com/superdb/super/vector"
)

// DedupType returns a map of type names to types like the argument of sum_type
// in which each unnamed record type that appears more than once within the
// map's types is replaced by a named type shared by its appearances.  The name
// is derived from the names of the fields with that type (see deriveName).
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
	dedup := newDeduper(d.sctx)
	for it := val.ContainerIter(); !it.Done(); {
		key := it.Next()
		typ, err := d.sctx.LookupByValue(it.Next())
		if err != nil {
			panic(err)
		}
		keys = append(keys, key)
		types = append(types, typ)
		// A map entry's type isn't a candidate itself (it's named by the
		// entry's key, e.g., by sum_type) but the types within it are.
		dedup.walkInner(typ, camelCase(super.DecodeString(key)))
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

type deduper struct {
	sctx *super.Context
	// order holds the unnamed record types in the order first seen.
	order []*super.TypeRecord
	recs  map[*super.TypeRecord]*recInfo
	// rewritten memoizes rewrite.
	rewritten map[super.Type]super.Type
	// used holds the names given to types by this deduper.
	used map[string]super.Type
}

type recInfo struct {
	count int
	// names holds the names of the fields with this type (in CamelCase and
	// in the singular for an element of an array, set, or map), in the
	// order first seen, and counts their appearances.
	names  []string
	counts map[string]int
	// base and version give the name chosen for the type if it's shared,
	// which is base if version is zero and base followed by "v" and version
	// otherwise.
	base    string
	version int
}

func newDeduper(sctx *super.Context) *deduper {
	return &deduper{
		sctx:      sctx,
		recs:      make(map[*super.TypeRecord]*recInfo),
		rewritten: make(map[super.Type]super.Type),
		used:      make(map[string]super.Type),
	}
}

// walk records an appearance of typ as the type of a field named name (or the
// element type of a field named name if plural is true) and walks the types
// within typ.  The body of a record type is walked only the first time the
// type is seen since, if the type is seen again, it's replaced by a named type
// whose body appears once.  A named type is a barrier since its body can't
// change without changing what the name means.
func (d *deduper) walk(typ super.Type, name string, plural bool) {
	if rec, ok := typ.(*super.TypeRecord); ok && len(rec.Fields) > 0 {
		if plural {
			name = singular(name)
		}
		info, ok := d.recs[rec]
		if !ok {
			info = &recInfo{counts: make(map[string]int)}
			d.recs[rec] = info
			d.order = append(d.order, rec)
		}
		info.count++
		if name != "" {
			if info.counts[name] == 0 {
				info.names = append(info.names, name)
			}
			info.counts[name]++
		}
		if ok {
			return
		}
	}
	d.walkInnerPlural(typ, name, plural)
}

func (d *deduper) walkInner(typ super.Type, name string) {
	d.walkInnerPlural(typ, name, false)
}

func (d *deduper) walkInnerPlural(typ super.Type, name string, plural bool) {
	switch typ := typ.(type) {
	case *super.TypeRecord:
		for _, f := range typ.Fields {
			d.walk(f.Type, camelCase(f.Name), false)
		}
	case *super.TypeArray:
		d.walk(typ.Type, name, true)
	case *super.TypeSet:
		d.walk(typ.Type, name, true)
	case *super.TypeMap:
		d.walk(typ.KeyType, name, true)
		d.walk(typ.ValType, name, true)
	case *super.TypeUnion:
		for _, typ := range typ.Types {
			d.walk(typ, name, plural)
		}
	case *super.TypeOption:
		d.walk(typ.Type, name, plural)
	case *super.TypeError:
		d.walk(typ.Type, name, plural)
	case *super.TypeFusion:
		d.walk(typ.Type, name, plural)
	}
}

// chooseNames chooses a name for each record type that's seen more than once.
// Types whose derived names are the same are versioned with suffixes v1, v2,
// and so on in the order first seen so each type gets a unique name.
func (d *deduper) chooseNames() {
	var byName []string
	groups := make(map[string][]*super.TypeRecord)
	for _, rec := range d.order {
		info := d.recs[rec]
		if info.count < 2 {
			continue
		}
		name := deriveName(info.names, info.counts)
		if name == "" {
			continue
		}
		if _, ok := groups[name]; !ok {
			byName = append(byName, name)
		}
		groups[name] = append(groups[name], rec)
	}
	for _, name := range byName {
		recs := groups[name]
		for i, rec := range recs {
			info := d.recs[rec]
			info.base = name
			if len(recs) > 1 {
				info.version = i + 1
			}
		}
	}
}

func versionName(base string, version int) string {
	if version == 0 {
		return base
	}
	return fmt.Sprintf("%sv%d", base, version)
}

// deriveName derives a type name from the names of the fields with the type:
// the name if there's only one, else the words the names end with if they end
// with the same words (e.g., Address for HomeAddress and WorkAddress), else the
// most frequent name, else the names that are tied for most frequent joined in
// alphabetical order (up to three of them).
func deriveName(names []string, counts map[string]int) string {
	switch len(names) {
	case 0:
		return ""
	case 1:
		return names[0]
	}
	if suffix := commonSuffix(names); suffix != "" {
		return suffix
	}
	var most []string
	var max int
	for _, name := range names {
		switch n := counts[name]; {
		case n > max:
			max = n
			most = []string{name}
		case n == max:
			most = append(most, name)
		}
	}
	slices.Sort(most)
	if len(most) > 3 {
		most = most[:1]
	}
	return strings.Join(most, "")
}

// commonSuffix returns the CamelCase words that all of names end with.
func commonSuffix(names []string) string {
	suffix := camelWords(names[0])
	for _, name := range names[1:] {
		words := camelWords(name)
		n := 0
		for n < len(suffix) && n < len(words) && suffix[len(suffix)-1-n] == words[len(words)-1-n] {
			n++
		}
		suffix = suffix[len(suffix)-n:]
	}
	return strings.Join(suffix, "")
}

// camelWords splits a CamelCase name into its words, where a word begins at
// an upper case letter.
func camelWords(name string) []string {
	var words []string
	start := 0
	for i, r := range name {
		if i > start && unicode.IsUpper(r) {
			words = append(words, name[start:i])
			start = i
		}
	}
	return append(words, name[start:])
}

// rewrite returns typ with each shared record type replaced by its named type.
func (d *deduper) rewrite(typ super.Type) super.Type {
	if out, ok := d.rewritten[typ]; ok {
		return out
	}
	out := typ
	switch typ := typ.(type) {
	case *super.TypeRecord:
		fields := make([]super.Field, 0, len(typ.Fields))
		for _, f := range typ.Fields {
			fields = append(fields, super.NewField(f.Name, d.rewrite(f.Type)))
		}
		out = d.sctx.MustLookupTypeRecord(fields)
		if info := d.recs[typ]; info != nil && info.base != "" {
			if named := d.bind(info.base, info.version, out); named != nil {
				out = named
			}
		}
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

// bind binds the name given by base and version to typ.  If the name is
// already used for a different type, whether by this deduper or in the
// query's type context, the next free version is used instead (e.g., Userv3
// if Userv2 is taken).
func (d *deduper) bind(base string, version int, typ super.Type) *super.TypeNamed {
	for range 1000 {
		name := versionName(base, version)
		if t, ok := d.used[name]; !ok || t == typ {
			if named, err := d.sctx.LookupTypeNamed(name, typ); err == nil {
				d.used[name] = typ
				return named
			}
		}
		version++
	}
	return nil
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

// singular returns the singular of the last word of a CamelCase name using a
// few English rules, e.g., Commits is Commit and Entries is Entry.
func singular(name string) string {
	lower := strings.ToLower(name)
	switch {
	case strings.HasSuffix(lower, "ies") && len(name) > 3:
		return name[:len(name)-3] + "y"
	case strings.HasSuffix(lower, "sses"), strings.HasSuffix(lower, "uses"), strings.HasSuffix(lower, "xes"),
		strings.HasSuffix(lower, "ches"), strings.HasSuffix(lower, "shes"):
		return name[:len(name)-2]
	case strings.HasSuffix(lower, "s") && !strings.HasSuffix(lower, "ss") &&
		!strings.HasSuffix(lower, "us") && !strings.HasSuffix(lower, "is") && len(name) > 1:
		return name[:len(name)-1]
	}
	return name
}
