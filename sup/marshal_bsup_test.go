package sup_test

import (
	"bytes"
	"errors"
	"math"
	"net/netip"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/superdb/super"
	"github.com/superdb/super/bsup"
	"github.com/superdb/super/pkg/nano"
	"github.com/superdb/super/sio"
	"github.com/superdb/super/sio/bsupio"
	"github.com/superdb/super/sup"
	"github.com/x448/float16"
)

func boomerang(t *testing.T, in any, out any) {
	sctx := super.NewContext()
	rec, err := super.NewMarshaler(sctx).Marshal(in)
	require.NoError(t, err)
	var buf bytes.Buffer
	zw := bsup.NewRowWriter(sio.NopCloser(&buf))
	err = zw.Write(rec)
	require.NoError(t, err)
	require.NoError(t, zw.Close())
	zr, err := bsupio.NewValueReader(t.Context(), sctx, &buf)
	require.NoError(t, err)
	val, err := zr.Read()
	require.NoError(t, err)
	err = super.Unmarshal(*val, out)
	require.NoError(t, err)
}

func TestMarshalBSUP(t *testing.T) {
	type S2 struct {
		Field2 string `super:"f2"`
		Field3 int
	}
	type S1 struct {
		Field1  string
		Sub1    S2
		PField1 *bool
	}
	rec, err := super.NewMarshaler(super.NewContext()).Marshal(S1{
		Field1: "value1",
		Sub1: S2{
			Field2: "value2",
			Field3: -1,
		},
	})
	require.NoError(t, err)
	require.NotNil(t, rec)
	assert.Equal(t, `{Field1:"value1",Sub1:{f2:"value2",Field3:-1},PField1:null}`, sup.FormatValue(rec))
}

func TestMarshalMap(t *testing.T) {
	type s struct {
		Name string
		Map  map[string]int
	}
	cases := []s{
		{Name: "nil", Map: nil},
		{Name: "empty", Map: map[string]int{}},
		{Name: "nonempty", Map: map[string]int{"a": 1, "b": 2}},
	}
	for _, c := range cases {
		t.Run(c.Name, func(t *testing.T) {
			var v s
			boomerang(t, c, &v)
			assert.Equal(t, c, v)
		})
	}
}

type BSUPThing struct {
	A string `super:"a"`
	B int
}

type BSUPThings struct {
	Things []BSUPThing
}

func TestMarshalSlice(t *testing.T) {
	t.Skip() // skipping until we fix marshal to use named types for interfaces
	m := super.NewMarshaler(super.NewContext())
	m.Decorate(super.StyleSimple)

	s := []BSUPThing{{"hello", 123}, {"world", 0}}
	r := BSUPThings{s}
	rec, err := m.Marshal(r)
	require.NoError(t, err)
	require.NotNil(t, rec)
	assert.Equal(t, `{Things:[{a:"hello",B:123}::=BSUPThing,{a:"world",B:0}::BSUPThing]}::=BSUPThings`, sup.FormatValue(rec))

	empty := []BSUPThing{}
	r2 := BSUPThings{empty}
	rec2, err := m.Marshal(r2)
	require.NoError(t, err)
	require.NotNil(t, rec2)
	assert.Equal(t, "{Things:[]::[BSUPThing={a:string,B:int64}]}::=BSUPThings", sup.FormatValue(rec2))

	rec3, err := m.Marshal(BSUPThings{nil})
	require.NoError(t, err)
	require.NotNil(t, rec3)
	assert.Equal(t, "{Things:null}::=BSUPThings", sup.FormatValue(rec3))

}

func TestMarshalNilSlice(t *testing.T) {
	type TestNilSlice struct {
		Name  string
		Slice []string
	}
	t1 := TestNilSlice{Name: "test"}
	expected := TestNilSlice{Name: "test", Slice: []string{}}
	var t2 TestNilSlice
	boomerang(t, t1, &t2)
	assert.Equal(t, expected, t2)
}

func TestMarshalEmptySlice(t *testing.T) {
	type TestNilSlice struct {
		Name  string
		Slice []string
	}
	t1 := TestNilSlice{Name: "test", Slice: []string{}}
	var t2 TestNilSlice
	boomerang(t, t1, &t2)
	assert.Equal(t, t1, t2)
}

func TestMarshalTime(t *testing.T) {
	type TestTime struct {
		Ts nano.Ts
	}
	t1 := TestTime{Ts: nano.Now()}
	var t2 TestTime
	boomerang(t, t1, &t2)
	assert.Equal(t, t1, t2)
}

type TestIP struct {
	Addr netip.Addr
}

func TestIPType(t *testing.T) {
	s := TestIP{Addr: netip.MustParseAddr("192.168.1.1")}
	sctx := super.NewContext()
	m := super.NewMarshaler(sctx)
	rec, err := m.Marshal(s)
	require.NoError(t, err)
	require.NotNil(t, rec)
	assert.Equal(t, "{Addr:192.168.1.1}", sup.FormatValue(rec))

	var tip TestIP
	err = super.Unmarshal(rec, &tip)
	require.NoError(t, err)
	require.Equal(t, s, tip)
}

func TestUnmarshalRecord(t *testing.T) {
	type T3 struct {
		T3f1 int32
		T3f2 float32
	}
	type T2 struct {
		T2f1 T3
		T2f2 string
	}
	type T1 struct {
		T1f1 *T2 `super:"top"`
	}
	v1 := T1{
		T1f1: &T2{T2f1: T3{T3f1: 1, T3f2: 1.0}, T2f2: "t2f2-string1"},
	}
	sctx := super.NewContext()
	rec, err := super.NewMarshaler(sctx).Marshal(v1)
	require.NoError(t, err)
	require.NotNil(t, rec)

	const expected = `{top:{T2f1:{T3f1:1::int32,T3f2:1.::float32},T2f2:"t2f2-string1"}}`
	require.Equal(t, expected, sup.FormatValue(rec))

	val := sup.MustParseValue(sctx, expected)
	var v2 T1
	err = super.Unmarshal(val, &v2)
	require.NoError(t, err)
	require.Equal(t, v1, v2)

	type T4 struct {
		T4f1 *T2 `super:"top"`
	}
	var v3 *T4
	err = super.Unmarshal(rec, &v3)
	require.NoError(t, err)
	require.NotNil(t, v3)
	require.NotNil(t, v3.T4f1)
	require.Equal(t, *v1.T1f1, *v3.T4f1)
}

func TestUnmarshalNull(t *testing.T) {
	t.Run("slice", func(t *testing.T) {
		slice := []int{1}
		require.NoError(t, super.Unmarshal(super.Null, &slice))
		assert.Nil(t, slice)
		slice = []int{1}
		val := sup.MustParseValue(super.NewContext(), "null::(null|[int64])")
		require.NoError(t, super.Unmarshal(val, &slice))
		assert.Nil(t, slice)
		buf := []byte("testing")
		require.NoError(t, super.Unmarshal(super.Null, &buf))
		assert.Nil(t, buf)
		buf = []byte("testing")
		val = sup.MustParseValue(super.NewContext(), "null::(null|bytes)")
		require.NoError(t, super.Unmarshal(val, &buf))
		assert.Nil(t, buf)
	})
	t.Run("primitive", func(t *testing.T) {
		integer := -1
		assert.EqualError(t, super.Unmarshal(super.Null, &integer), "incompatible type translation: Super type null, Go type int, Go kind int")
		intptr := &integer
		assert.NoError(t, super.Unmarshal(super.Null, &intptr))
		assert.Nil(t, intptr)
	})
	t.Run("map", func(t *testing.T) {
		m := map[string]string{"key": "value"}
		require.NoError(t, super.Unmarshal(super.Null, &m))
		assert.Nil(t, m)
		m = map[string]string{"key": "value"}
		val := sup.MustParseValue(super.NewContext(), "null::(null|map{string:string})")
		require.NoError(t, super.Unmarshal(val, &m))
		assert.Nil(t, m)
	})
	t.Run("struct", func(t *testing.T) {
		type testobj struct {
			Val int
		}
		var obj struct {
			Test *testobj `super:"test"`
		}
		val := sup.MustParseValue(super.NewContext(), "{test:null::(null|{Val:int64})}")
		require.NoError(t, super.Unmarshal(val, &obj))
		require.Nil(t, obj.Test)
		var slice struct {
			Test []string `super:"test"`
		}
		slice.Test = []string{"1"}
		val = sup.MustParseValue(super.NewContext(), "{test:null}")
		require.NoError(t, super.Unmarshal(val, &slice))
		require.Nil(t, slice.Test)
	})
}

func TestUnmarshalSlice(t *testing.T) {
	type T1 struct {
		T1f1 []bool
	}
	v1 := T1{
		T1f1: []bool{true, false, true},
	}
	sctx := super.NewContext()
	rec, err := super.NewMarshaler(sctx).Marshal(v1)
	require.NoError(t, err)
	require.NotNil(t, rec)

	var v2 T1
	err = super.Unmarshal(rec, &v2)
	require.NoError(t, err)
	require.Equal(t, v1, v2)

	type T2 struct {
		Field1 []*int
	}
	intp := func(x int) *int { return &x }
	v3 := T2{
		Field1: []*int{intp(1), intp(2)},
	}
	sctx = super.NewContext()
	rec, err = super.NewMarshaler(sctx).Marshal(v3)
	require.NoError(t, err)
	require.NotNil(t, rec)

	var v4 T2
	err = super.Unmarshal(rec, &v4)
	require.NoError(t, err)
	require.Equal(t, v1, v2)
}

type testMarshaler string

var _ super.CustomMarshaler = (*testMarshaler)(nil)

func (m testMarshaler) MarshalSuper(mc *super.Marshaler) (super.Type, error) {
	return mc.MarshalValue("marshal-" + string(m))
}

func (m *testMarshaler) UnmarshalSuper(mc *super.Unmarshaler, val super.Value) error {
	var s string
	if err := mc.Unmarshal(val, &s); err != nil {
		return err
	}
	ss := strings.Split(s, "-")
	if len(ss) != 2 && ss[0] != "marshal" {
		return errors.New("bad value")
	}
	*m = testMarshaler(ss[1])
	return nil
}

func TestMarshalInterface(t *testing.T) {
	type rectype struct {
		M1 *testMarshaler
		M2 testMarshaler
	}
	m1 := testMarshaler("m1")
	r1 := rectype{M1: &m1, M2: testMarshaler("m2")}
	rec, err := super.NewMarshaler(super.NewContext()).Marshal(r1)
	require.NoError(t, err)
	require.NotNil(t, rec)
	assert.Equal(t, `{M1:"marshal-m1",M2:"marshal-m2"}`, sup.FormatValue(rec))

	var r2 rectype
	err = super.Unmarshal(rec, &r2)
	require.NoError(t, err)
	assert.Equal(t, "m1", string(*r2.M1))
	assert.Equal(t, "m2", string(r2.M2))
}

func TestMarshalArray(t *testing.T) {
	type rectype struct {
		A1 [2]int8
		A2 *[2]string
		A3 [][2]byte
	}
	a2 := &[2]string{"foo", "bar"}
	r1 := rectype{A1: [2]int8{1, 2}, A2: a2} // A3 left as nil
	rec, err := super.NewMarshaler(super.NewContext()).Marshal(r1)
	require.NoError(t, err)
	require.NotNil(t, rec)
	const expected = `{A1:[1::int8,2::int8],A2:["foo","bar"],A3:[]::[bytes]}`
	assert.Equal(t, expected, sup.FormatValue(rec))

	var r2 rectype
	err = super.Unmarshal(rec, &r2)
	require.NoError(t, err)
	assert.Equal(t, r1.A1, r2.A1)
	assert.Equal(t, *r2.A2, *r2.A2)
	assert.Len(t, r2.A3, 0)
}

func TestNumbers(t *testing.T) {
	type rectype struct {
		I    int
		I8   int8
		I16  int16
		I32  int32
		I64  int64
		U    uint
		UI8  uint8
		UI16 uint16
		UI32 uint32
		UI64 uint64
		F16  float16.Float16
		F32  float32
		F64  float64
	}
	r1 := rectype{
		I:    math.MinInt64,
		I8:   math.MinInt8,
		I16:  math.MinInt16,
		I32:  math.MinInt32,
		I64:  math.MinInt64,
		U:    math.MaxUint64,
		UI8:  math.MaxUint8,
		UI16: math.MaxUint16,
		UI32: math.MaxUint32,
		UI64: math.MaxUint64,
		F16:  float16.Fromfloat32(65504.0),
		F32:  math.MaxFloat32,
		F64:  math.MaxFloat64,
	}
	rec, err := super.NewMarshaler(super.NewContext()).Marshal(r1)
	require.NoError(t, err)
	require.NotNil(t, rec)
	const expected = "{I:-9223372036854775808,I8:-128::int8,I16:-32768::int16,I32:-2147483648::int32,I64:-9223372036854775808,U:18446744073709551615::uint64,UI8:255::uint8,UI16:65535::uint16,UI32:4294967295::uint32,UI64:18446744073709551615::uint64,F16:65504.::float16,F32:3.4028235e+38::float32,F64:1.7976931348623157e+308}"
	assert.Equal(t, expected, sup.FormatValue(rec))

	var r2 rectype
	err = super.Unmarshal(rec, &r2)
	require.NoError(t, err)
	assert.Equal(t, r1, r2)
}

func TestCustomRecord(t *testing.T) {
	vals := []any{
		BSUPThing{"hello", 123},
		99,
	}
	m := super.NewMarshaler(super.NewContext())
	rec, err := m.MarshalCustom([]string{"foo", "bar"}, vals)
	require.NoError(t, err)
	assert.Equal(t, `{foo:{a:"hello",B:123},bar:99}`, sup.FormatValue(rec))

	vals = []any{
		BSUPThing{"hello", 123},
		nil,
	}
	rec, err = m.MarshalCustom([]string{"foo", "bar"}, vals)
	require.NoError(t, err)
	assert.Equal(t, `{foo:{a:"hello",B:123},bar:null}`, sup.FormatValue(rec))
}

type ThingTwo struct {
	C string `super:"c"`
}

type ThingaMaBob interface {
	Who() string
}

func (t *BSUPThing) Who() string { return t.A }
func (t *ThingTwo) Who() string  { return t.C }

func Make(which int) ThingaMaBob {
	if which == 1 {
		return &BSUPThing{A: "It's a thing one"}
	}
	if which == 2 {
		return &ThingTwo{"It's a thing two"}
	}
	return nil
}

type Rolls []int

func TestInterfaceBSUPMarshal(t *testing.T) {
	t1 := Make(2)
	m := super.NewMarshaler(super.NewContext())
	m.Decorate(super.StylePackage)
	zv, err := m.Marshal(t1)
	require.NoError(t, err)
	assert.Equal(t, `sup_test.ThingTwo`, sup.String(zv.Type()))

	m.Decorate(super.StyleSimple)
	rolls := Rolls{1, 2, 3}
	zv, err = m.Marshal(rolls)
	require.NoError(t, err)
	assert.Equal(t, "Rolls", sup.String(zv.Type()))

	m.Decorate(super.StyleFull)
	zv, err = m.Marshal(rolls)
	require.NoError(t, err)
	assert.Equal(t, `"github.com/superdb/super/sup_test.Rolls"`, sup.String(zv.Type()))

	plain := []int32{1, 2, 3}
	zv, err = m.Marshal(plain)
	require.NoError(t, err)
	assert.Equal(t, "[int32]", sup.String(zv.Type()))
}

func TestInterfaceUnmarshal(t *testing.T) {
	t1 := Make(1)
	m := super.NewMarshaler(super.NewContext())
	m.Decorate(super.StylePackage)
	zv, err := m.Marshal(t1)
	require.NoError(t, err)
	assert.Equal(t, `sup_test.BSUPThing`, sup.String(zv.Type()))

	u := super.NewUnmarshaler()
	u.Bind(BSUPThing{}, ThingTwo{})
	var thing ThingaMaBob
	require.NoError(t, err)
	err = u.Unmarshal(zv, &thing)
	require.NoError(t, err)
	assert.Equal(t, "It's a thing one", thing.Who())

	var thingI any
	err = u.Unmarshal(zv, &thingI)
	require.NoError(t, err, sup.String(zv))
	actualThing, ok := thingI.(*BSUPThing)
	assert.Equal(t, true, ok)
	assert.Equal(t, t1, actualThing)

	u2 := super.NewUnmarshaler()
	var genericThing any
	err = u2.Unmarshal(zv, &genericThing)
	require.Error(t, err)
	assert.Equal(t, `unmarshaling records into interface value requires type binding`, err.Error())
}

func TestBindings(t *testing.T) {
	t1 := Make(1)
	m := super.NewMarshaler(super.NewContext())
	m.NamedBindings([]super.Binding{
		{Name: "SpecialThingOne", Template: &BSUPThing{}},
		{Name: "SpecialThingTwo", Template: &ThingTwo{}},
	})
	zv, err := m.Marshal(t1)
	require.NoError(t, err)
	assert.Equal(t, "SpecialThingOne", sup.String(zv.Type()))

	u := super.NewUnmarshaler()
	u.NamedBindings([]super.Binding{
		{Name: "SpecialThingOne", Template: &BSUPThing{}},
		{Name: "SpecialThingTwo", Template: &ThingTwo{}},
	})
	var thing ThingaMaBob
	require.NoError(t, err)
	err = u.Unmarshal(zv, &thing)
	require.NoError(t, err)
	assert.Equal(t, "It's a thing one", thing.Who())
}

func TestEmptyInterface(t *testing.T) {
	zv, err := super.Marshal(super.NewContext(), int8(123))
	require.NoError(t, err)
	assert.Equal(t, "int8", sup.String(zv.Type()))

	var v any
	err = super.Unmarshal(zv, &v)
	require.NoError(t, err)
	i, ok := v.(int8)
	assert.Equal(t, true, ok)
	assert.Equal(t, int8(123), i)

	var actual int8
	err = super.Unmarshal(zv, &actual)
	require.NoError(t, err)
	assert.Equal(t, int8(123), actual)
}

type CustomInt8 int8

func TestNamedNormal(t *testing.T) {
	t1 := CustomInt8(88)
	m := super.NewMarshaler(super.NewContext())
	m.Decorate(super.StyleSimple)

	zv, err := m.Marshal(t1)
	require.NoError(t, err)
	assert.Equal(t, "CustomInt8", sup.String(zv.Type()))

	var actual CustomInt8
	u := super.NewUnmarshaler()
	u.Bind(CustomInt8(0))
	err = u.Unmarshal(zv, &actual)
	require.NoError(t, err)
	assert.Equal(t, t1, actual)

	var actualI any
	err = u.Unmarshal(zv, &actualI)
	require.NoError(t, err)
	cast, ok := actualI.(CustomInt8)
	assert.Equal(t, true, ok)
	assert.Equal(t, t1, cast)
}

type EmbeddedA struct {
	A ThingaMaBob
}

type EmbeddedB struct {
	A any
}

func TestEmbeddedInterface(t *testing.T) {
	t1 := &EmbeddedA{
		A: Make(1),
	}
	m := super.NewMarshaler(super.NewContext())
	m.Decorate(super.StyleSimple)
	zv, err := m.Marshal(t1)
	require.NoError(t, err)
	assert.Equal(t, "EmbeddedA", sup.String(zv.Type()))

	u := super.NewUnmarshaler()
	u.Bind(BSUPThing{}, ThingTwo{})
	var actual EmbeddedA
	require.NoError(t, err)
	err = u.Unmarshal(zv, &actual)
	require.NoError(t, err)
	assert.Equal(t, "It's a thing one", actual.A.Who())

	var actualB EmbeddedB
	require.NoError(t, err)
	err = u.Unmarshal(zv, &actualB)
	require.NoError(t, err)
	thingB, ok := actualB.A.(*BSUPThing)
	assert.Equal(t, true, ok)
	assert.Equal(t, "It's a thing one", thingB.Who())
}

func TestMultipleSuperValues(t *testing.T) {
	t.Skip()
	bytes := []byte("foo")
	u := super.NewUnmarshaler()
	var foo super.Value
	err := u.Unmarshal(super.NewValue(super.TypeString, bytes), &foo)
	require.NoError(t, err)
	// clobber bytes slice
	copy(bytes, []byte("bar"))
	var bar super.Value
	err = u.Unmarshal(super.NewValue(super.TypeString, bytes), &bar)
	require.NoError(t, err)
	assert.Equal(t, "foo", string(foo.Bytes()))
	assert.Equal(t, "bar", string(bar.Bytes()))
}

func TestSuperValues(t *testing.T) {
	t.Skip() // doesn't work like this anymore
	test := func(t *testing.T, name, s string, v any) {
		t.Run(name, func(t *testing.T) {
			val := sup.MustParseValue(super.NewContext(), s)
			err := super.Unmarshal(val, v)
			require.NoError(t, err)
			val, err = super.Marshal(super.NewContext(), v)
			require.NoError(t, err)
			assert.Equal(t, s, sup.FormatValue(val))
		})
	}
	var testptr struct {
		Value *super.Value `super:"value"`
	}
	t.Run("pointer", func(t *testing.T) {
		test(t, "string", "{value:\"foo\"}", &testptr)
		test(t, "typed-null", "{value:null}", &testptr)
		test(t, "null", "{value:null}", &testptr)
		test(t, "record", "{value:{foo:1,bar:\"baz\"}}", &testptr)
	})
	var teststruct struct {
		Value super.Value `super:"value"`
	}
	t.Run("struct", func(t *testing.T) {
		test(t, "string", "{value:\"foo\"}", &teststruct)
		test(t, "typed-null", "{value:null}", &teststruct)
		test(t, "null", "{value:null}", &teststruct)
		test(t, "record", "{value:{foo:1,bar:\"baz\"}}", &teststruct)
	})
}
