package sio_test

//  This is really a system test dressed up as a unit test.

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/superdb/super"
	"github.com/superdb/super/bsup"
	"github.com/superdb/super/sbuf"
	"github.com/superdb/super/sio"
	"github.com/superdb/super/sio/bsupio"
	"github.com/superdb/super/sio/supio"
)

type Output struct {
	bytes.Buffer
}

func (o *Output) Close() error {
	return nil
}

// Send logs to SUP reader -> BSUP writer -> BSUP reader -> SUP writer.
func boomerang(t *testing.T, logs string, compress bool) {
	in := []byte(strings.TrimSpace(logs) + "\n")
	supSrc := supio.NewReader(super.NewContext(), bytes.NewReader(in))
	var rawBSUP Output
	rawDst := bsup.NewRowWriter(&rawBSUP)
	require.NoError(t, sio.Copy(rawDst, supSrc))
	require.NoError(t, rawDst.Close())

	var out Output
	rawSrc, err := bsupio.NewReader(t.Context(), super.NewContext(), &rawBSUP, nil, 1)
	require.NoError(t, err)
	supDst := supio.NewWriter(&out, supio.WriterOpts{})
	err = sio.Copy(supDst, sbuf.PullerReader(sbuf.NewMaterializer(rawSrc)))
	if assert.NoError(t, err) {
		assert.Equal(t, in, out.Bytes())
	}
}

const sup1 = `
{foo:set["\"test\""]}
{foo:set["\"testtest\""]}
`

const sup2 = `{foo:{bar:"test"}}`

const sup3 = "{foo:set[null]}"

const sup4 = `{foo:"-"}`

const sup5 = `{foo:"[",bar:"[-]"}`

// Make sure we handle null fields and empty sets.
const sup6 = "{id:{a:null,s:set[]::set[string]}}"

// Make sure we handle empty and null sets.
const sup7 = `{a:"foo",b:set[]::set[string],c:null::(null|set[string])}`

// recursive record with null set and empty set
const sup8 = `
{id:{a:null,s:set[]::set[string]}}
{id:{a:null,s:null::(null|set[string])}}
{id:null::(null|{a:string,s:set[string]})}
`

// generate some really big strings
func supBig() string {
	return fmt.Sprintf(`{f0:"%s",f1:"%s",f2:"%s",f3:"%s"}`,
		"aaaa", strings.Repeat("b", 400), strings.Repeat("c", 30000), "dd")
}

func TestRaw(t *testing.T) {
	boomerang(t, sup1, false)
	boomerang(t, sup2, false)
	boomerang(t, sup3, false)
	boomerang(t, sup4, false)
	boomerang(t, sup5, false)
	boomerang(t, sup6, false)
	boomerang(t, sup7, false)
	boomerang(t, sup8, false)
	boomerang(t, supBig(), false)
}

func TestRawCompressed(t *testing.T) {
	boomerang(t, sup1, true)
	boomerang(t, sup2, true)
	boomerang(t, sup3, true)
	boomerang(t, sup4, true)
	boomerang(t, sup5, true)
	boomerang(t, sup6, true)
	boomerang(t, sup7, true)
	boomerang(t, sup8, true)
	boomerang(t, supBig(), true)
}

func TestNamed(t *testing.T) {
	const simple = `type ipaddr=ip
{foo:"bar",orig_h:127.0.0.1::ipaddr}`
	const multipleRecords = `
type ipaddr=ip
{foo:"bar",orig_h:127.0.0.1::ipaddr}
{foo:"bro",resp_h:127.0.0.1::ipaddr}
`
	const recordNamed = `
type myrec={host:ip}
{foo:{host:127.0.0.2}::myrec}
{foo:null::(myrec|null)}
`
	t.Run("BSUP", func(t *testing.T) {
		t.Run("simple", func(t *testing.T) {
			boomerang(t, simple, true)
		})
		t.Run("named-type-in-different-records", func(t *testing.T) {
			boomerang(t, multipleRecords, true)
		})
		t.Run("named-record-type", func(t *testing.T) {
			boomerang(t, recordNamed, true)
		})
	})
}
