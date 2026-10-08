package dbio

import (
	"bytes"
	"fmt"
	"io"
	"strings"
	"uuid"

	"github.com/superdb/super"
	"github.com/superdb/super/db"
	"github.com/superdb/super/db/commits"
	"github.com/superdb/super/db/data"
	"github.com/superdb/super/db/pools"
	"github.com/superdb/super/dbid"
	"github.com/superdb/super/pkg/charm"
	"github.com/superdb/super/pkg/terminal/color"
	"github.com/superdb/super/pkg/units"
	"github.com/superdb/super/runtime/sam/op/meta"
	"github.com/superdb/super/sbuf"
	"github.com/superdb/super/sup"
	"github.com/superdb/super/vector"
)

type WriterOpts struct {
	Head string
}

type Writer struct {
	writer   io.WriteCloser
	sup      *sup.StreamFormatter
	commits  table
	branches map[uuid.UUID][]string
	width    int
	colors   color.Stack
	headID   uuid.UUID
	headName string
}

func NewWriter(w io.WriteCloser, opts WriterOpts) *Writer {
	writer := &Writer{
		writer:   w,
		sup:      sup.NewStreamFormatter(0, false),
		commits:  make(table),
		branches: make(map[uuid.UUID][]string),
		width:    80, //XXX
	}
	// If head is an ID, we assume its detached and format accordingly.
	// If it's name, we'll print "HEAD -> branch" in the branch name listing
	// if we encounter that name.
	if headID, err := dbid.ParseID(opts.Head); err == nil {
		writer.headID = headID
	} else {
		writer.headName = opts.Head
	}
	return writer
}

func (w *Writer) Push(vec vector.Any) error {
	return sbuf.WriteVec(w, vec)
}

func (w *Writer) Write(rec super.Value) error {
	var v any
	if err := unmarshaler.Unmarshal(rec, &v); err != nil {
		return w.WriteSUP(rec)
	}
	var b bytes.Buffer
	w.formatValue(w.commits, &b, v, w.width, &w.colors)
	_, err := w.writer.Write(b.Bytes())
	return err
}

func (w *Writer) Close() error {
	return w.writer.Close()
}

func (w *Writer) WriteSUP(val super.Value) error {
	if _, err := io.WriteString(w.writer, w.sup.FormatValue(val)); err != nil {
		return err
	}
	_, err := io.WriteString(w.writer, "\n")
	return err
}

func (w *Writer) formatValue(t table, b *bytes.Buffer, v any, width int, colors *color.Stack) {
	switch v := v.(type) {
	case *pools.Config:
		formatPoolConfig(b, v)
	case *db.BranchMeta:
		formatBranchMeta(b, v, w.headID, w.headName, colors)
	case data.Object:
		formatDataObject(b, &v, "", 0)
	case *data.Object:
		formatDataObject(b, v, "", 0)
	case meta.Partition:
		formatPartition(b, v)
	case *commits.Commit:
		branches := w.branches[v.ID]
		t.formatCommit(b, v, branches, w.headName, w.headID, width, colors)
	case *db.BranchTip:
		w.branches[v.Commit] = append(w.branches[v.Commit], v.Name)
	default:
		if action, ok := v.(commits.Action); ok {
			t.append(action)
			return
		}
		b.WriteString(fmt.Sprintf("database format: unknown type: %T\n", v))
	}
}

func formatPoolConfig(b *bytes.Buffer, p *pools.Config) {
	b.WriteString(p.Name)
	b.WriteByte(' ')
	b.WriteString(p.ID.String())
	b.WriteString(" key ")
	b.WriteString(p.SortKeys.Primary().Path.String())
	b.WriteString(" order ")
	b.WriteString(p.SortKeys.Primary().Order.String())
	b.WriteByte('\n')
}

func formatBranchMeta(b *bytes.Buffer, p *db.BranchMeta, headID uuid.UUID, headName string, colors *color.Stack) {
	b.WriteString(p.Pool.Name)
	b.WriteByte('@')
	b.WriteString(p.Branch.Name)
	b.WriteByte(' ')
	colors.Start(b, color.GrayYellow)
	b.WriteString("commit ")
	b.WriteString(p.Branch.Commit.String())
	if headID == p.Branch.Commit || headName == p.Branch.Name {
		b.WriteString(" (")
		colors.Start(b, color.Turqoise)
		b.WriteString(color.Embolden("HEAD"))
		colors.End(b)
		b.WriteByte(')')
	}
	colors.End(b)
	b.WriteByte('\n')
}

func tab(b *bytes.Buffer, indent int) {
	for range indent {
		b.WriteByte(' ')
	}
}

func formatDataObject(b *bytes.Buffer, object *data.Object, prefix string, indent int) {
	tab(b, indent)
	if prefix != "" {
		b.WriteString(prefix)
		b.WriteByte(' ')
	}
	b.WriteString(object.ID.String())
	objectSize := units.Bytes(object.Size).Abbrev()
	b.WriteString(fmt.Sprintf(" %s bytes %d records", objectSize, object.Count))
	b.WriteString("\n  ")
	tab(b, indent)
	b.WriteString(" min ")
	b.WriteString(sup.String(object.Min))
	b.WriteString(" max ")
	b.WriteString(sup.String(object.Max))
	b.WriteByte('\n')
}

func formatPartition(b *bytes.Buffer, p meta.Partition) {
	b.WriteString("min ")
	b.WriteString(sup.String(p.Min))
	b.WriteString(" max ")
	b.WriteString(sup.String(p.Max))
	b.WriteByte('\n')
	for _, o := range p.Objects {
		formatDataObject(b, o, "", 2)
	}
}

type table map[uuid.UUID][]commits.Action

func (t table) append(a commits.Action) {
	id := a.CommitID()
	t[id] = append(t[id], a)
}

func (t table) formatCommit(b *bytes.Buffer, commit *commits.Commit, branches []string, headName string, headID uuid.UUID, width int, colors *color.Stack) {
	id := commit.CommitID()
	colors.Start(b, color.GrayYellow)
	b.WriteString("commit ")
	b.WriteString(id.String())
	if len(branches) > 0 {
		b.WriteString(" (")
		for k, name := range branches {
			if k != 0 {
				b.WriteString(", ")
			}
			if name == headName {
				colors.Start(b, color.Turqoise)
				b.WriteString(color.Embolden("HEAD -> "))
				colors.End(b)
			}
			colors.Start(b, color.Green)
			b.WriteString(name)
			colors.End(b)
		}
		b.WriteString(")")
	} else if commit.ID == headID {
		b.WriteString(" (")
		colors.Start(b, color.Blue)
		b.WriteString("HEAD")
		colors.End(b)
		b.WriteString(")")
	}
	colors.End(b)
	b.WriteString("\nAuthor: ")
	b.WriteString(commit.Author)
	b.WriteString("\nDate:   ")
	b.WriteString(commit.Date.String())
	b.WriteString("\n\n")
	if commit.Message != "" {
		s := charm.FormatParagraph(commit.Message, "    ", width)
		s = strings.TrimRight(s, " \n") + "\n\n"
		b.WriteString(s)
	}
}
