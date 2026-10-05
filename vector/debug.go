package vector

import (
	"fmt"
	"io"
	"reflect"
	"slices"
	"strings"

	"github.com/superdb/super/scode"
	"github.com/superdb/super/sup"
)

func FormatValues(vec Any) string {
	var b strings.Builder
	for i := range vec.Len() {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(sup.FormatValue(ValueAt(nil, vec, i)))
	}
	return b.String()
}

func PrintSlot(vec Any, slot uint32) {
	fmt.Println(sup.FormatValue(ValueAt(scode.NewBuilder(), vec, slot)))
}

func Println(a ...any) {
	a = slices.Clone(a)
	for i, v := range a {
		if vec, ok := v.(Any); ok {
			a[i] = Format(vec)
		}
	}
	fmt.Println(a...)
}

func Format(vec Any) string {
	var b strings.Builder
	write(&b, vec, "", "")
	return b.String()
}

func write(w io.Writer, vec Any, indent, prefix string) {
	if vec == nil {
		io.WriteString(w, "<nil>")
		return
	}
	if ctrl, ok := vec.(*Control); ok && false {
		io.WriteString(w, "Control")
		indent += "   "
		write(w, ctrl.Any, indent, prefix)
		return
	}
	if labeled, ok := vec.(*Labeled); ok {
		io.WriteString(w, "Labeled ")
		io.WriteString(w, labeled.Label)
		indent += "   "
		write(w, labeled.Any, indent, prefix)
		return
	}

	_, goType, _ := strings.Cut(reflect.TypeOf(vec).String(), ".")
	var typ string
	if _, ok := vec.(*Dynamic); !ok {
		typ = " type=" + sup.FormatType(vec.Type())
	}
	prefix = ""
	fmt.Fprintf(w, "%s%s%s%s len=%d", indent, prefix, goType, typ, vec.Len())
	indent += "   "
	switch vec := vec.(type) {
	case *Uint:
		fmt.Fprintf(w, " values=%v", vec.Values)
	case *Int:
		fmt.Fprintf(w, " values=%v", vec.Values)
	case *Float:
		fmt.Fprintf(w, " values=%v", vec.Values)
	case *Bool:
		fmt.Fprintf(w, " bits=%v", vec.Bits)
	case *Bytes:
		fmt.Fprintf(w, " offs=%v bytes=%v", vec.table.offsets, vec.table.bytes)
	case *String:
		fmt.Fprintf(w, " offsets=%v bytes=%s", vec.table.offsets, vec.table.bytes)
	case *IP:
		fmt.Fprintf(w, " values=%v", vec.Values)
	case *Net:
		fmt.Fprintf(w, " values=%v", vec.Values)
	case *TypeValue:
	case *Null:
	case *None:
	case *Empty:

	case *Record:
		for k, f := range vec.Fields {
			fmt.Fprintln(w)
			write(w, f, indent, fmt.Sprintf("fields[%d]=", k))
		}
	case *Array:
		fmt.Fprintf(w, " offsets=%v\n", vec.Offsets)
		write(w, vec.Values, indent, "values=")
	case *Set:
		fmt.Fprintf(w, " offsets=%v\n", vec.Offsets)
		write(w, vec.Values, indent, "values=")
	case *Map:
		fmt.Fprintf(w, " offsets=%v\n", vec.Offsets)
		write(w, vec.Keys, indent, "keys=")
		fmt.Fprintln(w)
		write(w, vec.Values, indent, "values=")
	case *Union:
		fmt.Fprintf(w, " tags=%v", vec.Tags())
		for k, v := range vec.Values() {
			fmt.Fprintln(w)
			write(w, v, indent, fmt.Sprintf("values[%d]=", k))
		}
	case *Error:
		fmt.Fprintln(w)
		write(w, vec.Vals, indent, "vals=")
	case *Enum:
		fmt.Fprintln(w)
		write(w, vec.Uint, indent, "uint=")
	case *Named:
		fmt.Fprintln(w)
		write(w, vec.Any, indent, " any=")

	case *Const:
		fmt.Fprintln(w)
		write(w, vec.Any, indent, "any=")
	case *Dict:
		fmt.Fprintf(w, " index=%v\n", vec.Index)
		write(w, vec.Any, indent, "any=")
	case *Dynamic:
		fmt.Fprintf(w, " tags=%v", vec.Tags)
		for k, v := range vec.Values {
			fmt.Fprintln(w)
			write(w, v, indent, fmt.Sprintf("values[%d]=", k))
		}
	case *Fusion:
		fmt.Fprintf(w, " subtype=%s\n", "?" /* sup.FormatType(val.SubTypes) */)
		write(w, vec.Values, indent, "values=")
	case *Option:
		fmt.Fprintf(w, " style=%s\n", optionStyle(vec.Any))
		write(w, vec.Any, indent, "any=")
	case *View:
		fmt.Fprintf(w, " index=%v\n", vec.Index)
		write(w, vec.Any, indent, "any=")
	default:
		fmt.Fprintf(w, " unknown %#v", vec)
	}
}

func optionStyle(vec Any) string {
	switch vec.(type) {
	case *None:
		return "none"
	case *Dynamic:
		return "both"
	default:
		return "some"
	}
}
