package function

import (
	"bytes"

	"github.com/superdb/super"
	"github.com/superdb/super/runtime/expr"
	"github.com/superdb/super/scode"
	"github.com/superdb/super/sio"
	"github.com/superdb/super/sio/jsonio"
	"github.com/superdb/super/vector"
)

// JSONStringify formats each value as JSON text like the JSON output format.
type JSONStringify struct {
	sctx   *super.Context
	buf    bytes.Buffer
	writer *jsonio.Writer
}

func newJSONStringify(sctx *super.Context) *JSONStringify {
	j := &JSONStringify{sctx: sctx}
	j.writer = jsonio.NewWriter(sio.NopCloser(&j.buf), jsonio.WriterOpts{NoColor: true})
	return j
}

func (j *JSONStringify) Call(args ...vector.Any) vector.Any {
	if vec, ok := expr.CatchFirst(j.sctx, expr.CatchAll, args, "json_stringify"); ok {
		return vec
	}
	vec := args[0]
	out := vector.NewStringEmpty(vec.Len())
	var b scode.Builder
	for i := range vec.Len() {
		j.buf.Reset()
		if err := j.writer.Write(vector.ValueAt(&b, vec, i)); err != nil {
			return vector.NewWrappedError(j.sctx, "json_stringify: "+err.Error(), vec)
		}
		out.Append(string(bytes.TrimSuffix(j.buf.Bytes(), []byte{'\n'})))
	}
	return out
}
