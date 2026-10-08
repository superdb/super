package vector_test

import (
	"bytes"
	"encoding/binary"
	"math/rand"
	"testing"

	"github.com/superdb/super"
	"github.com/superdb/super/bsup"
	"github.com/superdb/super/fuzz"
)

func FuzzQuery(f *testing.F) {
	f.Add([]byte("values f1\x00"))
	f.Add([]byte("values f1, f2\x00"))
	f.Add([]byte("f1 == null\x00"))
	f.Add([]byte("f1 == null | values f2\x00"))
	f.Fuzz(func(t *testing.T, b []byte) {
		bytesReader := bytes.NewReader(b)
		querySource := fuzz.GenAscii(bytesReader)
		sctx := super.NewContext()
		types := fuzz.GenTypes(bytesReader, sctx, 3)
		values := fuzz.GenValues(bytesReader, sctx, types)

		// Debug
		//for i := range values {
		//    t.Logf("value: in[%v].Bytes()=%v", i, values[i].Bytes())
		//    t.Logf("value: in[%v]=%v", i, sup.String(&values[i]))
		//}

		var bsupRowsBuf bytes.Buffer
		fuzz.WriteBSUPRows(t, values, &bsupRowsBuf)
		resultBSUPRows := fuzz.RunQueryBSUP(t, &bsupRowsBuf, querySource)

		var bsupBuf bytes.Buffer
		fuzz.WriteBSUP(t, sctx, values, &bsupBuf)
		resultBSUP := fuzz.RunQueryBSUP(t, &bsupBuf, querySource)

		fuzz.CompareValues(t, resultBSUPRows, resultBSUP)
	})
}

const N = 10000000

func BenchmarkReadBSUPRows(b *testing.B) {
	rand := rand.New(rand.NewSource(42))
	valuesIn := make([]super.Value, N)
	for i := range valuesIn {
		valuesIn[i] = super.NewInt64(rand.Int63n(N))
	}
	var buf bytes.Buffer
	fuzz.WriteBSUPRows(b, valuesIn, &buf)
	bs := buf.Bytes()

	for b.Loop() {
		valuesOut, err := fuzz.ReadBSUPRows(b.Context(), super.NewContext(), bs)
		if err != nil {
			panic(err)
		}
		if super.DecodeInt(valuesIn[N-1].Bytes()) != super.DecodeInt(valuesOut[N-1].Bytes()) {
			panic("oh no")
		}
	}
}

func BenchmarkReadBSUP(b *testing.B) {
	rand := rand.New(rand.NewSource(42))
	valuesIn := make([]super.Value, N)
	for i := range valuesIn {
		valuesIn[i] = super.NewValue(super.TypeInt64, super.EncodeInt(int64(rand.Intn(N))))
	}
	sctx := super.NewContext()
	var buf bytes.Buffer
	fuzz.WriteBSUP(b, sctx, valuesIn, &buf)
	bs := buf.Bytes()

	for b.Loop() {
		bytesReader := bytes.NewReader(bs)
		fit := bsup.NewSeekable(sctx, bytesReader)
		frame, err := fit.Next()
		if err != nil {
			panic(err)
		}
		_ = frame
		// TODO Expose a cheap way to get values out of vectors.
		//if intsIn[N-1] != intsOut[N-1] {
		//    panic("oh no")
		//}
	}
}

func BenchmarkReadVarint(b *testing.B) {
	rand := rand.New(rand.NewSource(42))
	intsIn := make([]int64, N)
	for i := range intsIn {
		intsIn[i] = int64(rand.Intn(N))
	}
	var bs []byte
	for _, int := range intsIn {
		bs = binary.AppendVarint(bs, int)
	}

	for b.Loop() {
		bs := bs
		intsOut := make([]int64, N)
		for i := range intsOut {
			value, n := binary.Varint(bs)
			if n <= 0 {
				panic("oh no")
			}
			bs = bs[n:]
			intsOut[i] = value
		}
		if intsIn[N-1] != intsOut[N-1] {
			panic("oh no")
		}
	}
}
