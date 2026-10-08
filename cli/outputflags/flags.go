package outputflags

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/superdb/super/cli/auto"
	"github.com/superdb/super/pkg/storage"
	"github.com/superdb/super/pkg/terminal"
	"github.com/superdb/super/pkg/terminal/color"
	"github.com/superdb/super/sbuf"
	"github.com/superdb/super/sio"
	"github.com/superdb/super/sio/anyio"
	"github.com/superdb/super/sio/emitter"
	"github.com/superdb/super/vector/vio"
)

type Flags struct {
	anyio.WriterOpts
	color        bool
	forceBinary  bool
	isFormatSet  bool
	jsonPretty   bool
	jsonShortcut bool
	outputFile   string
	pretty       int
	split        string
	splitSize    auto.Bytes
	supPretty    bool
	supShortcut  bool
	unbuffered   bool
}

func (f *Flags) Options() anyio.WriterOpts {
	return f.WriterOpts
}

func (f *Flags) setFlags(fs *flag.FlagSet) {
	fs.BoolVar(&f.color, "color", true, "enable/disable color formatting for -S and db text output")
	fs.BoolVar(&f.CSV.NoHeader, "noheader", false, "omit header for CSV and TSV output")
	fs.IntVar(&f.pretty, "pretty", 2,
		"tab size to pretty print JSON and Super JSON output (0 for newline-delimited output")
	fs.StringVar(&f.outputFile, "o", "", "write data to output file")
	fs.BoolVar(&f.BSUP.Rows, "rows", false, "output BSUP in row format instead of columns")
	fs.StringVar(&f.split, "split", "",
		"split output into one file per data type in this directory (but see -splitsize)")
	fs.Var(&f.splitSize, "splitsize",
		"if >0 and -split is set, split into files at least this big rather than by data type")
	fs.BoolVar(&f.unbuffered, "unbuffered", false, "disable output buffering")
}

func (f *Flags) SetFlags(fs *flag.FlagSet) {
	f.SetFormatFlags(fs)
	f.setFlags(fs)
}

func (f *Flags) SetFlagsWithFormat(fs *flag.FlagSet, format string) {
	f.setFlags(fs)
	f.Format = format
}

func (f *Flags) SetFormatFlags(fs *flag.FlagSet) {
	if f.Format == "" {
		f.Format = "bsup"
	}
	fUsage := fmt.Sprintf(
		"format for output data [arrows,bsup,csv,db,json,line,parquet,sup,table,tsv,zeek] (default %s)",
		f.Format)
	fs.Func("f", fUsage, func(s string) error {
		f.Format = s
		f.isFormatSet = true
		return nil
	})
	fs.BoolVar(&f.forceBinary, "B", false, "allow Super Binary to be sent to a terminal output")
	fs.BoolVar(&f.jsonPretty, "J", false, "use formatted JSON output independent of -f option")
	fs.BoolVar(&f.jsonShortcut, "j", false, "use line-oriented JSON output independent of -f option")
	fs.BoolVar(&f.SUPFusion, "fusion", false, "display fusion values (fusion values are otherwise auto-defused)")
	fs.BoolVar(&f.supPretty, "S", false, "use formatted Super JSON output independent of -f option")
	fs.BoolVar(&f.supShortcut, "s", false, "use line-oriented Super JSON output independent of -f option")
}

func (f *Flags) Init() error {
	f.JSON.Pretty, f.SUP.Pretty = f.pretty, f.pretty
	if f.jsonShortcut || f.jsonPretty {
		if f.isFormatSet || f.supShortcut || f.supPretty {
			return errors.New("cannot use -j or -J with -f, -s, or -S")
		}
		f.Format = "json"
		if !f.jsonPretty {
			f.JSON.Pretty = 0
		}
	} else if f.supShortcut || f.supPretty {
		if f.isFormatSet {
			return errors.New("cannot use -s or -S with -f")
		}
		f.Format = "sup"
		if !f.supPretty {
			f.SUP.Pretty = 0
		}
	} else if !f.isFormatSet {
		if fmt := sio.FormatFromPath(f.outputFile); fmt != "" {
			f.Format = fmt
		}
		if e := filepath.Ext(f.outputFile); e == ".jsonl" || e == ".ndjson" {
			f.JSON.Pretty = 0
		}
	}
	if f.outputFile == "-" {
		f.outputFile = ""
	}
	if f.outputFile == "" && f.split == "" && !f.isFormatSet &&
		isBinary(f.Format) && !f.forceBinary && terminal.IsTerminalFile(os.Stdout) {
		f.Format = "sup"
		f.SUP.Pretty = 0
	}
	if f.unbuffered {
		sbuf.PullerBatchValues = 1
	}
	return nil
}

func isBinary(fmt string) bool {
	return fmt == "arrows" || fmt == "bsup" || fmt == "parquet"
}

func (f *Flags) FileName() string {
	return f.outputFile
}

func (f *Flags) Open(ctx context.Context, engine storage.Engine) (vio.PushCloser, error) {
	if f.split != "" {
		dir, err := storage.ParseURI(f.split)
		if err != nil {
			return nil, fmt.Errorf("-split option: %w", err)
		}
		if size := f.splitSize.Bytes; size > 0 {
			return emitter.NewSizeSplitter(ctx, engine, dir, f.outputFile, f.unbuffered, f.WriterOpts, int64(size))
		}
		d, err := emitter.NewSplit(ctx, engine, dir, f.outputFile, f.unbuffered, f.WriterOpts)
		if err != nil {
			return nil, err
		}
		return d, nil
	}
	if f.outputFile == "" && f.color && terminal.IsTerminalFile(os.Stdout) {
		color.Enabled = true
	}
	w, err := emitter.NewFileFromPath(ctx, engine, f.outputFile, f.unbuffered, f.WriterOpts)
	if err != nil {
		return nil, err
	}
	return w, nil
}
