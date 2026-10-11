package queryflags

import (
	"flag"
	"fmt"
	"os"

	"github.com/superdb/super"
	"github.com/superdb/super/compiler/srcfiles"
	"github.com/superdb/super/sup"
	"github.com/superdb/super/vector/vio"
)

type QueryTextFlags struct {
	Query     []srcfiles.Input
	includes  FileInput
	dashCArgs PlainInput
}

type Flags struct {
	Stats bool
	QueryTextFlags
}

func (q *QueryTextFlags) SetFlags(fs *flag.FlagSet) {
	q.includes.inputs = &q.Query
	q.dashCArgs.inputs = &q.Query
	fs.Var(&q.dashCArgs, "c", "query text (may be used multiple times)")
	fs.Var(&q.includes, "I", "source file containing query text (may be used multiple times)")
}

func (f *Flags) SetFlags(fs *flag.FlagSet) {
	fs.BoolVar(&f.Stats, "stats", false, "display query stats on stderr")
	f.QueryTextFlags.SetFlags(fs)
}

func (f *Flags) PrintStats(stats vio.Progress) {
	if f.Stats {
		val, err := super.Marshal(super.NewContext(), stats)
		var out string
		if err != nil {
			out = fmt.Sprintf("error marshaling stats: %s", err)
		} else {
			out = sup.FormatValue(val)
		}
		fmt.Fprintln(os.Stderr, out)
	}
}

type PlainInput struct {
	inputs *[]srcfiles.Input
}

func (p *PlainInput) Set(value string) error {
	*p.inputs = append(*p.inputs, &srcfiles.PlainInput{Text: value})
	return nil
}

func (PlainInput) String() string {
	return ""
}

type FileInput struct {
	inputs *[]srcfiles.Input
}

func (f *FileInput) Set(value string) error {
	*f.inputs = append(*f.inputs, &srcfiles.FileInput{Name: value})
	return nil
}

func (FileInput) String() string {
	return ""
}
