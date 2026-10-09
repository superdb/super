package expr_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/superdb/super"
	"github.com/superdb/super/compiler"
	"github.com/superdb/super/compiler/dag"
	"github.com/superdb/super/compiler/parser"
	"github.com/superdb/super/compiler/rungen"
	"github.com/superdb/super/runtime"
	"github.com/superdb/super/runtime/exec"
	"github.com/superdb/super/runtime/expr"
	"github.com/superdb/super/sup"
	"github.com/superdb/super/vector"
)

type testcase struct {
	filter   string
	expected bool
}

func runCases(t *testing.T, record string, cases []testcase) {
	t.Helper()
	runCasesHelper(t, record, cases, false)
}

func runCasesExpectBufferFilterFalsePositives(t *testing.T, record string, cases []testcase) {
	t.Helper()
	runCasesHelper(t, record, cases, true)
}

func filter(sctx *super.Context, this super.Value, e expr.Evaluator) bool {
	if e == nil {
		return true
	}
	b := vector.NewDynamicValueBuilder()
	b.Write(this)
	vec := b.Build(sctx)
	out := e.Eval(vec)
	if out.Len() != 1 {
		panic(out)
	}
	val := vector.ValueAt(nil, out, 0)
	if val.Type() == super.TypeBool && val.Bool() {
		return true
	}
	return false
}

func runCasesHelper(t *testing.T, record string, cases []testcase, expectBufferFilterFalsePositives bool) {
	t.Helper()

	sctx := super.NewContext()
	rec, err := sup.ParseValue(sctx, record)
	require.NoError(t, err, "record: %q", record)

	rctx := runtime.NewContext(t.Context(), sctx)
	for _, c := range cases {
		t.Run(c.filter, func(t *testing.T) {
			t.Helper()
			ast, err := parser.ParseText(c.filter)
			require.NoError(t, err, "filter: %q", c.filter)
			env := &exec.Environment{}
			main, err := compiler.Analyze(rctx, ast, env, true)
			require.NoError(t, err, "filter: %q", c.filter)
			err = compiler.Optimize(rctx, main, env, 0)
			require.NoError(t, err, "filter: %q", c.filter)
			_, _, builder, err := compiler.BuildWithBuilder(rctx, main, env)
			require.NoError(t, err, "filter: %q", c.filter)
			filterOp, ok := main.Body[1].(*dag.FilterOp)
			require.True(t, ok)
			filterMaker := rungen.NewPushdown(builder, filterOp.Expr)
			f, err := filterMaker.DataFilter()
			assert.NoError(t, err, "filter: %q", c.filter)
			if f != nil {
				assert.Equal(t, c.expected, filter(sctx, rec, f),
					"filter: %q\nrecord: %s", c.filter, sup.FormatValue(rec))
			}
			// XXX in a subsequent PR, we will bring this test back when we
			// add BufferFilter to vio.Pushdown
			/*
				bf, err := filterMaker.BSUPFilter()
				assert.NoError(t, err, "filter: %q", c.filter)
				if bf != nil {
					expected := expectBufferFilterFalsePositives || c.expected
					buf := rec.Bytes()
					assert.Equal(t, expected, bf.Eval(sctx, buf),
						"filter: %q\nvalues:%s\nbuffer:\n%s", c.filter, sup.FormatValue(rec), hex.Dump(buf))
				}
			*/
		})
	}
}

func TestFilters(t *testing.T) {
	t.Parallel()

	// Test set membership with "in"
	runCases(t, `{stringset:set["abc","xyz"]}`, []testcase{
		{"'abc' in stringset", true},
		{"'xyz' in stringset", true},
		{"'ab'in stringset", false},
		{"'abcd' in stringset", false},
	})

	// Test escaped strings inside a set
	runCases(t, `{stringset:set["a;b","xyz"]}`, []testcase{
		{"\"a;b\" in stringset", true},
		{"'a' in stringset", false},
		{"'b' in stringset", false},
		{"'xyz' in stringset", true},
	})

	// Test array membership with "in"
	runCases(t, `{stringvec:["abc","xyz"]}`, []testcase{
		{"'abc' in stringvec", true},
		{"'xyz' in stringvec", true},
		{"'ab' in stringvec", false},
		{"'abcd' in stringvec", false},
	})

	// Test membership in set of integers
	runCases(t, "{intset:set[1::int32,2::int32,3::int32]}", []testcase{
		{"2 in intset", true},
		{"4 in intset", false},
		{"'abc' in intset", false},
	})

	// Test membership in array of integers
	runCases(t, "{intvec:[1::int32,2::int32,3::int32]}", []testcase{
		{"2 in intvec", true},
		{"4 in intvec", false},
		{"'abc' in intvec", false},
	})

	// Test membership in set of ip addresses
	runCases(t, "{addrset:set[1.1.1.1,2.2.2.2]}", []testcase{
		{"1.1.1.1 in addrset", true},
		{"3.3.3.3 in addrset", false},
	})

	// Test membership and len() on array of ip addresses
	runCases(t, "{addrvec:[1.1.1.1,2.2.2.2]}", []testcase{
		{"1.1.1.1 in addrvec", true},
		{"3.3.3.3 in addrvec", false},
		{"len(addrvec) == 2", true},
		{"len(addrvec) == 3", false},
		{"len(addrvec) > 1", true},
		{"len(addrvec) >= 2", true},
		{"len(addrvec) < 5", true},
		{"len(addrvec) <= 2", true},
	})

	// Test comparing fields in nested records
	//
	// We expect false positives from BufferFilter here because it looks for
	// values without regard to field name, returning true as long as some
	// field matches the literal to the right of the equal sign.
	runCasesExpectBufferFilterFalsePositives(t, `{nested:{field:"test"}}`, []testcase{
		{`nested.field == "test"`, true},
		{`bogus.field == "test"`, false},
		{`nested.bogus == "test"`, false},
		//{"* = test", false},
	})

	// Test array of records
	runCases(t, "{nested:[{field:1::int32},{field:2}]}", []testcase{
		{"nested[0].field == 1", true},
		{"nested[1].field == 2", true},
		{"nested[0].field == 2", false},
		{"nested[2].field == 2", false},
		{"nested.field == 2", false},
	})

	// Test array inside a record
	runCases(t, "{nested:{vec:[1::int32,2::int32,3::int32]}}", []testcase{
		{"1 in nested.vec", true},
		{"2 in nested.vec", true},
		{"4 in nested.vec", false},
		{"nested.vec[0] == 1", true},
		{"nested.vec[1] == 1", false},
		{"1 in nested", true},
		{"?1", true},
	})

	// Test unicode string comparison.  The following two records
	// both have the string "Buenos días señor" but one uses
	// combining characters (e.g., plain n plus combining
	// tilde) and the other uses composed characters.  Test both
	// strings against queries written with both formats.
	runCases(t, `{s:"Buenos di\u0301as sen\u0303or"}`, []testcase{
		{`s == "Buenos di\u{0301}as sen\u{0303}or"`, true},
		{`s == "Buenos d\u{ed}as se\u{f1}or"`, true},
	})

	// There are two Unicode code points with a multibyte UTF-8 encoding
	// equivalent under Unicode simple case folding to code points with
	// single-byte UTF-8 encodings: U+017F LATIN SMALL LETTER LONG S is
	// equivalent to S and s, and U+212A KELVIN SIGN is equivalent to K and
	// k. The next two records ensure they're handled correctly.

	// Test U+017F LATIN SMALL LETTER LONG S.
	runCases(t, `{a:"\u017f"}`, []testcase{
		{`a == '\u017F'`, true},
		{`a == "S"`, false},
		{`a == "s"`, false},
		{`?\u017F`, true},
		{`?S`, false}, // Should be true; see https://github.com/superdb/super/issues/1207.
		{`?s`, false}, // Should be true; see https://github.com/superdb/super/issues/1207.
	})

	// Test U+212A KELVIN SIGN.
	runCases(t, `{a:"\u212a"}`, []testcase{
		{`a == '\u212A'`, true},
		{`a == "K"`, true}, // True because Unicode NFC replaces U+212A with U+004B.
		{`a == "k"`, false},
		{`?\u212A`, true},
		{`?K`, true},
		{`?k`, true},
	})

	// Test searching both fields and containers,
	// also test case-insensitive search.
	runCases(t, `{s:"hello",srec:{svec:["world","worldz","1.1.1.1"]}}`, []testcase{
		{"?hello", true},
		{"?worldz", true},
		{"?HELLO", true},
		{"?WoRlDZ", true},
		{"?1.1.1.1", true},
		{"?wor*", true},
	})

	// Test searching a record inside an array, record, set, and union.
	for _, c := range []struct {
		name   string
		record string
	}{
		{"array", `{a:[{i:123,s1:"456",s2:"hello"}]}`},
		{"record", `{r:{r2:{i:123,s1:"456",s2:"hello"}}}`},
		{"set", `{s:set[{i:123,s1:"456",s2:"hello"}]}`},
		{"union", `{u:{i:123,s1:"456",s2:"hello, world"}::(int64|{i:int64,s1:string,s2:string})}`},
	} {
		t.Run(c.name, func(t *testing.T) {
			runCases(t, c.record, []testcase{
				{"?123", true},
				{`?"123"`, false},
				{"?12", false},
				{"?456", true},
				{`?"456"`, true},
				{"?45", true},
				{"?hello", true},
			})
		})
	}

	// Test searching with subnet syntax
	runCases(t, "{addr:192.168.1.50}", []testcase{
		{"?192.168.0.0/16", true},
		{"?192.168.1.0/24", true},
		{"?10.0.0.0/8", false},
	})

	// Test time coercion
	runCases(t, "{ts:1970-01-01T00:00:01.001Z,ts2:2020-01-07T15:38:52Z,ts3:2020-01-07T15:38:53.01Z}", []testcase{
		{"ts<2000000000", true},
		{"ts==1001000000", true},
		{"ts<1002000000.0", true},
		{"ts<2000000000.0", true},
		{"ts2==1578411532000000000", true},
		{"ts3==1578411533000000000", false},
	})

	// Test that string search doesn't match non-string types:
	// The ASCII value of 'T' (0x54) is present inside the binary
	// encoding of 1.001.  But naked string search should not match.
	runCases(t, "{f:1.001}", []testcase{
		{"?T", false},
	})

	// Test integer conditions.  These are really testing 2 things:
	// 1. that the full range of values are correctly parsed
	// 2. that coercion to int64 works properly (in all the filters
	//    with integers on the RHS)
	// 3. that coercion to float64 works properly (in the filters
	//    with floats on the RHS)
	record := "{b:0::uint8,i16:-32768::int16,u16:0::uint16,i32:-2147483648::int32,u32:0::uint32,i64:-9223372036854775808,u64:0::uint64}"
	runCases(t, record, []testcase{
		{"b > -1", true},
		{"b == 0", true},
		{"b < 1", true},
		{"b > 1", false},
		{"b == 0.0", true},
		{"b < 0.5", true},

		{"i16 == -32768", true},
		{"i16 < 0", true},
		{"i16 > 0", false},
		{"i16 == -32768.0", true},
		{"i16 < 0.0", true},

		{"u16 > -1", true},
		{"u16 == 0", true},
		{"u16 < 1", true},
		{"u16 > 1", false},
		{"u16 == 0.0", true},
		{"u16 < 0.5", true},

		{"i32 == -2147483648", true},
		{"i32 < 0", true},
		{"i32 > 0", false},
		{"i32 == -2147483648.0", true},
		{"i32 < 0.5", true},

		{"u32 > -1", true},
		{"u32 == 0", true},
		{"u32 < 1", true},
		{"u32 > 1", false},
		{"u32 == 0.0", true},
		{"u32 < 0.5", true},

		{"i64 == -9223372036854775808", true},
		{"i64 < 0", true},
		{"i64 > 0", false},
		{"i64 < 0.0", true},
		// MinInt64 can't be represented precisely as a float64

		{"u64 > -1", true},
		{"u64 == 0", true},
		{"u64 < 1", true},
		{"u64 > 1", false},
		{"u64 == 0.0", true},
		{"u64 < 0.5", true},
	})

	record = "{b:255::uint8,i16:32767::int16,u16:65535::uint16,i32:2147483647::int32,u32:4294967295::uint32,i64:9223372036854775807,u64:18446744073709551615::uint64}"
	runCases(t, record, []testcase{
		{"b == 255", true},
		{"i16 == 32767", true},
		{"u16 == 65535", true},
		{"i32 == 2147483647", true},
		{"u32 == 4294967295", true},
		{"i64 == 9223372036854775807", true},
		// Can't represent large unsigned 64 bit values in SUP.
		// {"u64 = 18446744073709551615", true},
	})

	// Test comparisons with field of type port (can compare with
	// a port literal or an integer literal)
	runCases(t, "type port=uint16 {p:443::port}", []testcase{
		{"p == 443", true},
		{"p == 80", false},
	})

	runCases(t, `{s:"hello"}`, []testcase{
		{"s == 'hello'", true},
		{"s != 'hello'", false},

		// Also smoke test that globs work...
		{"grep('hell.*', s)", true},
		{"grep('^ell.*', s)", false},
		{"!grep('hell.*', s)", false},
		{"!grep('^ell.*', s)", true},
	})

	// Test ip comparisons
	runCases(t, "{a:192.168.1.50}", []testcase{
		{"a == 192.168.1.50", true},
		{"a == 50.1.168.192", false},
		{"a != 50.1.168.192", true},
		{"cidr_match(192.168.0.0/16, a)", true},
		{"a == 10.0.0.0/16", false},
		{"a != 192.168.0.0/16", true},
	})

	// Test comparisons with a named type
	runCases(t, "type myint=int32 {i:100::myint}", []testcase{
		{"i == 100", true},
		{"i > 0", true},
		{"i < 50", false},
	})

	// Test searching a null field
	runCases(t, "{rec:null}", []testcase{
		{"?rec", false},
	})

	// Test searching an empty top-level record
	runCases(t, "{}", []testcase{
		{"?empty", false},
	})

	// Test searching an empty nested record
	runCases(t, "{empty:{}}", []testcase{
		{"?empty", false},
	})

}

func TestBadFilter(t *testing.T) {
	t.Parallel()
	_, err := parser.ParseText(`s matches \xa8*`)
	require.Error(t, err)
}
