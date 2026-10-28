package cast_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdb/super"
	"github.com/superdb/super/runtime/expr/cast"
	"github.com/superdb/super/sup"
	"github.com/superdb/super/vector"
)

// TestCastVectorShapes checks that the cast of a value doesn't depend on how
// the value is represented, i.e., that casting a vector of a single value
// gives the same result as casting that value within a dynamic vector, a
// view, a const, or a record field.  (The ztests in runtime/ztests/expr/cast
// cover the cast semantics.)
func TestCastVectorShapes(t *testing.T) {
	values := []string{
		`null`, `none`, `1`, `"2"`, `"x"`, `"a"`, `1.5`, `true`, `10.0.0.1`, `<int64>`,
		`{a:1}`, `{a:"1"}`, `{a:"x"}`, `{a:null}`, `{a:1,b:"x"}`, `{b:"y",c:3}`,
		`{a:{b:1}}`, `{a:[1,2]}`, `{a:[1,"x"]}`, `{}`,
		`{a:1,b:none::int64}`, `{a:some(1)::option(int64)}`,
		`{a:missing::(int64|missing)}`, `{a:"1"::(string|missing)}`,
		`{type:"push",ref:"main"}`, `{type:"star",action:"x"}`,
		`{type:"pr",num:1,ref:"r"}`, `{type:"pr",num:2}`, `{foo:1}`,
		`[]`, `[1]`, `[1,2]`, `["1","2"]`, `[1,"a",2]`, `[1,1.5]`, `[1,"x",null]`,
		`[{a:1},{a:"2"}]`, `[[1],["2"]]`, `[none]`, `[1,none]`,
		`set[1,2]`, `set["1",1]`, `set[]`,
		`map{"a":1}`, `map{"a":1,"b":"2","c":"x"}`, `map{}`, `map{1:"a"}`,
		`foo:"a"`, `rec:{a:1}`, `[foo:"a"]`, `error("e")`, `error({a:1})`,
		`1::(int64|string)`, `"s"::(int64|string)`, `{a:1}::({a:int64}|string)`,
	}
	types := []string{
		`int64`, `string`, `float64`, `bool`, `ip`, `null`, `type`,
		`option(int64)`, `option(string)`, `option({a:int64})`, `option([int64])`,
		`{a:int64}`, `{a:string}`, `{a:option(int64)}`, `{a:int64,b:option(string)}`,
		`{a:{b:string}}`, `{a:[string]}`, `{}`, `{b:string,a:int64}`,
		`[int64]`, `[string]`, `[option(int64)]`, `[{a:int64}]`, `[[string]]`,
		`set[int64]`, `set[string]`,
		`map{string:int64}`, `map{string:string}`,
		`int64|string`, `string|int64`, `int64|missing`, `{a:int64}|missing`,
		`int64|none`, `{a:int64}|string`, `{a:int64|missing}`, `{a:string|missing}`, `foo:string`, `rec:{a:string}`, `rec:{a:int64}`,
		`error(string)`, `error({a:string})`,
		`{type:string,ref:string}|{type:string,action:string,repo:option(string)}|{type:string,num:int64,ref:option(string)}`,
		`{a:int64}|{a:string}`, `{a:int64}|{b:string,c:int64}`, `[int64]|[string]`,
		`decimal(10,2)`, `fusion(all)`, `enum(x,a)`, `[enum(x,a)]`,
	}
	sctx := super.NewContext()
	var vals []super.Value
	b := vector.NewDynamicValueBuilder()
	for _, s := range values {
		val := parseValue(t, sctx, s)
		vals = append(vals, val)
		b.Write(val)
	}
	vec := b.Build(sctx)
	// Also cast a view of the values (in reverse order), each value as a
	// const, and the values as a field of a record.
	n := uint32(len(vals))
	reverse := make([]uint32, n)
	for i := range reverse {
		reverse[i] = n - 1 - uint32(i)
	}
	view := vector.Pick(vec, reverse)
	// The record field test uses the values that can be members of a union.
	var recIndex []int
	rb := vector.NewDynamicValueBuilder()
	for i, val := range vals {
		if _, ok := val.Type().(*super.TypeUnion); !ok && !val.IsError() && !val.IsMissing() {
			recIndex = append(recIndex, i)
			rb.Write(val)
		}
	}
	field := vector.NewUnionFromDynamic(sctx, rb.Build(sctx).(*vector.Dynamic))
	recType := sctx.MustLookupTypeRecord([]super.Field{super.NewField("x", field.Type())})
	rec := vector.NewRecord(recType, []vector.Any{field}, field.Len())
	for _, s := range types {
		typ := parseType(t, sctx, s)
		expected := make([]string, n)
		for i, val := range vals {
			out := castVec(sctx, singleton(sctx, val), typ)
			expected[i] = sup.FormatValue(vector.ValueAt(nil, out, 0))
		}
		check := func(what string, out vector.Any, slot func(uint32) uint32) {
			require.EqualValues(t, n, out.Len(), s)
			for i := range n {
				actual := sup.FormatValue(vector.ValueAt(nil, out, i))
				if e := expected[slot(i)]; e != actual {
					t.Errorf("cast(%s, <%s>) (%s):\n  single: %s\n  got:    %s", values[slot(i)], s, what, e, actual)
				}
			}
		}
		identity := func(i uint32) uint32 { return i }
		check("dynamic", castVec(sctx, vec, typ), identity)
		check("view", castVec(sctx, view, typ), func(i uint32) uint32 { return n - 1 - i })
		for i, val := range vals {
			c := vector.NewConst(singleton(sctx, val), 3)
			out := castVec(sctx, c, typ)
			for k := range uint32(3) {
				actual := sup.FormatValue(vector.ValueAt(nil, out, k))
				if e := expected[i]; e != actual {
					t.Errorf("cast(%s, <%s>) (const):\n  single: %s\n  got:    %s", values[i], s, e, actual)
				}
			}
		}
		// Cast {x:val} to {x:typ} and check field x.
		if _, ok := typ.(*super.TypeOption); ok {
			continue
		}
		toRec := sctx.MustLookupTypeRecord([]super.Field{super.NewField("x", typ)})
		out := castVec(sctx, rec, toRec)
		for k, i := range recIndex {
			val := vals[i]
			v := vector.ValueAt(nil, out, uint32(k))
			if v.IsError() {
				continue
			}
			field := v.Deref("x")
			if field == nil {
				t.Errorf("cast({x:%s}, <{x:%s}>): no field x in %s", values[i], s, sup.FormatValue(v))
				continue
			}
			if val.IsNull() || isNone(val) {
				// These cast differently inside a union.
				continue
			}
			if e, a := expected[i], sup.FormatValue(*field); e != a {
				t.Errorf("cast({x:%s}, <{x:%s}>):\n  single: %s\n  got:    %s", values[i], s, e, a)
			}
		}
	}
}

func isNone(val super.Value) bool {
	return super.TypeUnder(val.Type()) == super.TypeNone
}

func castVec(sctx *super.Context, vec vector.Any, typ super.Type) vector.Any {
	return vector.Apply(vector.ApplyRipUnions, func(vecs ...vector.Any) vector.Any {
		if vecs[0].Kind() == vector.KindError {
			return vecs[0]
		}
		return cast.Cast(sctx, vecs[0], typ)
	}, vec)
}

func singleton(sctx *super.Context, val super.Value) vector.Any {
	b := vector.NewValueBuilder(val.Type())
	b.Write(val.Bytes())
	return b.Build(sctx)
}

// parseValue parses a SUP value with an optional "name:" prefix denoting a
// named type (since SUP no longer has embedded type definitions).  "[name:"
// denotes an array of the named type.
func parseValue(t *testing.T, sctx *super.Context, s string) super.Value {
	if name, rest, ok := strings.Cut(s, ":"); ok && isName(name) {
		val := parseValue(t, sctx, rest)
		return super.NewValue(named(t, sctx, name, val.Type()), val.Bytes())
	}
	if name, rest, ok := strings.Cut(strings.TrimPrefix(s, "["), ":"); ok && strings.HasPrefix(s, "[") && isName(name) {
		val := parseValue(t, sctx, "["+rest)
		elem := named(t, sctx, name, super.InnerType(val.Type()))
		return super.NewValue(sctx.LookupTypeArray(elem), val.Bytes())
	}
	val, err := sup.ParseValue(sctx, s)
	require.NoError(t, err, s)
	return val
}

func parseType(t *testing.T, sctx *super.Context, s string) super.Type {
	if name, rest, ok := strings.Cut(s, ":"); ok && isName(name) {
		return named(t, sctx, name, parseType(t, sctx, rest))
	}
	typ, err := sup.ParseType(sctx, s)
	require.NoError(t, err, s)
	return typ
}

func isName(s string) bool {
	return s == "foo" || s == "rec"
}

func named(t *testing.T, sctx *super.Context, name string, typ super.Type) super.Type {
	// Named types may be redefined so use a fresh name per underlying type.
	named, err := sctx.LookupTypeNamed(name+"_"+sup.FormatType(typ), typ)
	require.NoError(t, err)
	return named
}
