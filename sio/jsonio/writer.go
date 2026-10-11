package jsonio

import (
	"bufio"
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/superdb/super"
	"github.com/superdb/super/pkg/nano"
	"github.com/superdb/super/pkg/terminal/color"
	"github.com/superdb/super/sbuf"
	"github.com/superdb/super/scode"
	"github.com/superdb/super/sup"
	"github.com/superdb/super/vector"
)

var (
	boolColor   = []byte("\x1b[1m")
	fieldColor  = []byte("\x1b[34;1m")
	nullColor   = []byte("\x1b[2m")
	numberColor = []byte("\x1b[36m")
	puncColor   = []byte{} // no color
	stringColor = []byte("\x1b[32m")
)

type Writer struct {
	io.Closer
	writer *bufio.Writer
	tab    int

	// Use json.Encoder for primitive Values. Have to use
	// json.Encoder instead of json.Marshal because it's
	// the only way to turn off HTML escaping.
	primEnc *json.Encoder
	primBuf bytes.Buffer
}

type WriterOpts struct {
	Pretty int
}

func NewWriter(writer io.WriteCloser, opts WriterOpts) *Writer {
	w := &Writer{
		Closer: writer,
		writer: bufio.NewWriter(writer),
		tab:    opts.Pretty,
	}
	w.primEnc = json.NewEncoder(&w.primBuf)
	w.primEnc.SetEscapeHTML(false)
	return w
}

func (w *Writer) Push(vec vector.Any) error {
	return sbuf.WriteVec(w, vec)
}

func (w *Writer) Write(val super.Value) error {
	if val.Deoption().IsNone() {
		return nil
	}
	// writeAny doesn't return an error because any error that occurs will be
	// surfaced with w.writer.Flush is called.
	w.writeAny(0, val)
	w.writer.WriteByte('\n')
	return w.writer.Flush()
}

func (w *Writer) writeAny(tab int, val super.Value) {
	val = val.Under()
	if val.IsNull() || val.Type().ID() == super.IDNone {
		w.writeColor([]byte("null"), nullColor)
		return
	}
	if val.Type().ID() < super.IDTypeComplex {
		w.writePrimitive(val)
		return
	}
	switch typ := val.Type().(type) {
	case *super.TypeRecord:
		w.writeRecord(tab, typ, val.Bytes())
	case *super.TypeArray:
		w.writeArray(tab, typ.Type, val.Bytes())
	case *super.TypeSet:
		w.writeArray(tab, typ.Type, val.Bytes())
	case *super.TypeMap:
		w.writeMap(tab, typ, val.Bytes())
	case *super.TypeEnum:
		w.writeEnum(typ, val.Bytes())
	case *super.TypeError:
		w.writeError(tab, typ, val.Bytes())
	case *super.TypeOption:
		w.writeAny(tab, val.Deoption())
	default:
		panic(fmt.Sprintf("unsupported type: %s", sup.FormatType(typ)))
	}
}

func (w *Writer) writeRecord(tab int, typ *super.TypeRecord, bytes scode.Bytes) {
	tab += w.tab
	w.punc('{')
	if len(bytes) == 0 {
		w.punc('}')
		return
	}
	it := bytes.Iter()
	var sep byte
	for _, f := range typ.Fields {
		elem := it.Next()
		if super.IsNone(f.Type, elem) {
			continue
		}
		if sep != 0 {
			w.punc(sep)
		}
		w.writeEntry(tab, f.Name, super.NewValue(f.Type, elem))
		sep = ','
	}
	w.newline()
	w.indent(tab - w.tab)
	w.punc('}')
}

func (w *Writer) writeArray(tab int, typ super.Type, bytes scode.Bytes) {
	tab += w.tab
	w.punc('[')
	if len(bytes) == 0 {
		w.punc(']')
		return
	}
	it := bytes.Iter()
	for i := 0; !it.Done(); i++ {
		if i != 0 {
			w.punc(',')
		}
		w.newline()
		w.indent(tab)
		w.writeAny(tab, super.NewValue(typ, it.Next()))
	}
	w.newline()
	w.indent(tab - w.tab)
	w.punc(']')
}

func (w *Writer) writeMap(tab int, typ *super.TypeMap, bytes scode.Bytes) {
	tab += w.tab
	w.punc('{')
	if len(bytes) == 0 {
		w.punc('}')
		return
	}
	it := bytes.Iter()
	for i := 0; !it.Done(); i++ {
		if i != 0 {
			w.punc(',')
		}
		key := mapKey(typ.KeyType, it.Next())
		w.writeEntry(tab, key, super.NewValue(typ.ValType, it.Next()))
	}
	w.newline()
	w.indent(tab - w.tab)
	w.punc('}')
}

func mapKey(typ super.Type, b scode.Bytes) string {
	val := super.NewValue(typ, b)
	switch val.Type().Kind() {
	case super.PrimitiveKind:
		if val.Type().ID() == super.IDString {
			// Don't quote strings.
			return val.AsString()
		}
		return sup.FormatPrimitive(val.Type(), val.Bytes())
	case super.UnionKind:
		// Untagged, decorated SUP so
		// map{0:1,0(uint64):2,0(=t):3,"0":4} gets unique keys.
		typ, bytes := typ.(*super.TypeUnion).Untag(b)
		return sup.FormatValue(super.NewValue(typ, bytes))
	case super.EnumKind:
		return convertEnum(typ.(*super.TypeEnum), b)
	default:
		return sup.FormatValue(val)
	}
}

func (w *Writer) writeEnum(typ *super.TypeEnum, bytes scode.Bytes) {
	w.writeColor(w.marshalJSON(convertEnum(typ, bytes)), stringColor)
}

func convertEnum(typ *super.TypeEnum, bytes scode.Bytes) string {
	if k := int(super.DecodeUint(bytes)); k < len(typ.Symbols) {
		return typ.Symbols[k]
	}
	return "<bad enum>"
}

func (w *Writer) writeError(tab int, typ *super.TypeError, bytes scode.Bytes) {
	tab += w.tab
	w.punc('{')
	w.writeEntry(tab, "error", super.NewValue(typ.Type, bytes))
	w.newline()
	w.indent(tab - w.tab)
	w.punc('}')
}

func (w *Writer) writeEntry(tab int, name string, val super.Value) {
	w.newline()
	w.indent(tab)
	w.writeColor(w.marshalJSON(name), fieldColor)
	w.punc(':')
	if w.tab != 0 {
		w.writer.WriteByte(' ')
	}
	w.writeAny(tab, val)
}

func (w *Writer) writePrimitive(val super.Value) {
	var v any
	c := stringColor
	switch id := val.Type().ID(); {
	case id == super.IDDuration:
		v = nano.Duration(val.Int()).String()
	case id == super.IDTime:
		v = nano.Ts(val.Int()).Time().Format(time.RFC3339Nano)
	case super.IsSigned(id):
		v, c = val.Int(), numberColor
	case super.IsUnsigned(id):
		v, c = val.Uint(), numberColor
	case super.IsFloat(id):
		v, c = val.Float(), numberColor
	case id == super.IDBool:
		v, c = val.AsBool(), boolColor
	case id == super.IDBytes:
		v = "0x" + hex.EncodeToString(val.Bytes())
	case id == super.IDString:
		v = val.AsString()
	case id == super.IDIP:
		v = super.DecodeIP(val.Bytes()).String()
	case id == super.IDNet:
		v = super.DecodeNet(val.Bytes()).String()
	case id == super.IDType:
		v = sup.FormatValue(val)
	default:
		panic(fmt.Sprintf("unsupported id=%d", id))
	}
	w.writeColor(w.marshalJSON(v), c)
}

func (w *Writer) marshalJSON(v any) []byte {
	w.primBuf.Reset()
	if err := w.primEnc.Encode(v); err != nil {
		panic(err)
	}
	return bytes.TrimSpace(w.primBuf.Bytes())
}

func (w *Writer) punc(b byte) {
	w.writeColor([]byte{b}, puncColor)
}

func (w *Writer) writeColor(b []byte, code []byte) {
	if color.Enabled {
		w.writer.Write(code)
		defer w.writer.WriteString(color.Reset.String())
	}
	w.writer.Write(b)
}

func (w *Writer) newline() {
	if w.tab > 0 {
		w.writer.WriteByte('\n')
	}
}

func (w *Writer) indent(tab int) {
	w.writer.Write(bytes.Repeat([]byte(" "), tab))
}
