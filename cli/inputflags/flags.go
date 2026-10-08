package inputflags

import (
	"errors"
	"flag"

	"github.com/superdb/super/sio/anyio"
)

type Flags struct {
	Dynamic    bool
	ReaderOpts anyio.ReaderOpts
	Static     bool
}

func (f *Flags) SetFlags(fs *flag.FlagSet) {
	opts := &f.ReaderOpts
	opts.CSV.Delim = ','
	fs.Func("csv.delim", `CSV field delimiter (default ",")`, func(s string) error {
		if len(s) != 1 {
			return errors.New("CSV field delimiter must be exactly one character")
		}
		opts.CSV.Delim = rune(s[0])
		return nil

	})
	fs.BoolVar(&f.Dynamic, "dynamic", false, "disable static type checking of inputs")
	fs.StringVar(&opts.Format, "i", "auto", "format of input data [auto,arrows,bsup,csv,json,line,parquet,sup,tsv,zeek]")
	fs.BoolVar(&f.Static, "static", false, "force static type checking of inputs")
}

// Init is called after flags have been parsed.
func (f *Flags) Init() error {
	if f.Dynamic && f.Static {
		return errors.New("-static and -dynamic flags cannot both be enabled")
	}
	return nil
}
