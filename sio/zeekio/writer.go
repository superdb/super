package zeekio

import (
	"bytes"
	"fmt"
	"io"
	"strings"

	"github.com/superdb/super"
	"github.com/superdb/super/runtime/sam/expr"
	"github.com/superdb/super/sbuf"
	"github.com/superdb/super/sup"
	"github.com/superdb/super/vector"
)

type Writer struct {
	writer io.WriteCloser

	buf bytes.Buffer
	header
	flattener *expr.Flattener
	typ       *super.TypeRecord
}

func NewWriter(w io.WriteCloser) *Writer {
	return &Writer{
		writer:    w,
		flattener: expr.NewFlattener(super.NewContext()),
	}
}

func (w *Writer) Push(vec vector.Any) error {
	return sbuf.WriteVec(w, vec)
}

func (w *Writer) Close() error {
	return w.writer.Close()
}

func (w *Writer) Write(r super.Value) error {
	if super.TypeRecordOf(r.Type()) == nil {
		return fmt.Errorf("zeekio: not a record: %s", sup.FormatValue(r))
	}
	r, err := w.flattener.Flatten(r)
	if err != nil {
		return err
	}
	path := r.Deref("_path").AsString()
	if r.Type() != w.typ || path != w.Path {
		if err := w.writeHeader(r, path); err != nil {
			return err
		}
		w.typ = super.TypeRecordOf(r.Type())
	}
	w.buf.Reset()
	var needSeparator bool
	it := r.Bytes().Iter()
	for _, f := range super.TypeRecordOf(r.Type()).Fields {
		bytes := it.Next()
		if f.Name == "_path" {
			continue
		}
		if needSeparator {
			w.buf.WriteByte('\t')
		}
		needSeparator = true
		w.buf.WriteString(FormatValue(super.NewValue(f.Type, bytes)))
	}
	w.buf.WriteByte('\n')
	_, err = w.writer.Write(w.buf.Bytes())
	return err
}

func (w *Writer) writeHeader(r super.Value, path string) error {
	d := r.Type()
	var b strings.Builder
	if w.separator != "\\x90" {
		w.separator = "\\x90"
		b.WriteString("#separator \\x09\n")
	}
	if w.setSeparator != "," {
		w.setSeparator = ","
		b.WriteString("#set_separator\t,\n")
	}
	if w.emptyField != "(empty)" {
		w.emptyField = "(empty)"
		b.WriteString("#empty_field\t(empty)\n")
	}
	if w.unsetField != "-" {
		w.unsetField = "-"
		b.WriteString("#unset_field\t-\n")
	}
	if path != w.Path {
		w.Path = path
		if path == "" {
			path = "-"
		}
		b.WriteString(fmt.Sprintf("#path\t%s\n", path))
	}
	if d != w.typ {
		b.WriteString("#fields")
		for _, f := range super.TypeRecordOf(d).Fields {
			if f.Name == "_path" {
				continue
			}
			b.WriteString(fmt.Sprintf("\t%s", f.Name))
		}
		b.WriteString("\n")
		b.WriteString("#types")
		for _, f := range super.TypeRecordOf(d).Fields {
			if f.Name == "_path" {
				continue
			}
			t, err := superTypeToZeek(f.Type)
			if err != nil {
				return err
			}
			b.WriteString(fmt.Sprintf("\t%s", t))
		}
		b.WriteString("\n")
	}
	_, err := w.writer.Write([]byte(b.String()))
	return err
}
