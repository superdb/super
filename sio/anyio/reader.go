package anyio

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/superdb/super"
	"github.com/superdb/super/bsup"
	"github.com/superdb/super/sbuf"
	"github.com/superdb/super/sio"
	"github.com/superdb/super/sio/arrowio"
	"github.com/superdb/super/sio/bsupio"
	"github.com/superdb/super/sio/csvio"
	"github.com/superdb/super/sio/jsonio"
	"github.com/superdb/super/sio/parquetio"
	"github.com/superdb/super/sio/supio"
	"github.com/superdb/super/sio/zeekio"
	"github.com/superdb/super/vector/vio"
)

type ReaderOpts struct {
	Format            string
	Pushdown          sbuf.Pushdown
	ConcurrentReaders int
	CSV               csvio.ReaderOpts
}

func NewReader(ctx context.Context, sctx *super.Context, r io.Reader, opts ReaderOpts) (vio.Puller, error) {
	if opts.ConcurrentReaders == 0 {
		opts.ConcurrentReaders = 1
	}
	if opts.Format != "" && opts.Format != "auto" {
		return lookupReader(ctx, sctx, r, opts)
	}

	track := NewTrack(r)

	bsupErr := bsup.Probe(track)
	if bsupErr == nil {
		return bsupio.NewReader(ctx, sctx, track.Reader(), opts.Pushdown, opts.ConcurrentReaders)
	}
	bsupErr = fmt.Errorf("bsup: %w", bsupErr)
	track.Reset()

	parquetErr := isParquetStream(ctx, track)
	if parquetErr == nil {
		return parquetio.NewReader(ctx, sctx, track.Reader(), opts.Pushdown, opts.ConcurrentReaders)
	}
	parquetErr = fmt.Errorf("parquet: %w", parquetErr)
	track.Reset()

	arrowsErr := isArrowStream(track)
	if arrowsErr == nil {
		r, err := arrowio.NewReader(sctx, track.Reader())
		return newVioPuller(sctx, r), err
	}
	arrowsErr = fmt.Errorf("arrows: %w", arrowsErr)
	track.Reset()

	zeekErr := match(zeekio.NewReader(super.NewContext(), track), "zeek", 1)
	if zeekErr == nil {
		return newVioPuller(sctx, zeekio.NewReader(sctx, track.Reader())), nil
	}
	track.Reset()

	// JSON comes before SUP because the JSON reader is faster than the
	// SUP reader.  The number of values wanted is greater than one for the
	// sake of tests.
	jsonErr := isJSONStream(track, 10)
	if jsonErr == nil {
		return jsonio.NewReader(ctx, sctx, track.Reader(), opts.Pushdown, opts.ConcurrentReaders), nil
	}
	jsonErr = fmt.Errorf("json: %w", jsonErr)
	track.Reset()

	supErr := match(supio.NewReader(super.NewContext(), track), "sup", 1)
	if supErr == nil {
		return newVioPuller(sctx, supio.NewReader(sctx, track.Reader())), nil
	}
	track.Reset()

	csvErr := isCSVStream(track, ',', "csv")
	if csvErr == nil {
		return newVioPuller(sctx, csvio.NewReader(sctx, track.Reader(), csvio.ReaderOpts{Delim: ','})), nil
	}
	track.Reset()

	tsvErr := isCSVStream(track, '\t', "tsv")
	if tsvErr == nil {
		return newVioPuller(sctx, csvio.NewReader(sctx, track.Reader(), csvio.ReaderOpts{Delim: '\t'})), nil
	}
	track.Reset()

	lineErr := errors.New("line: auto-detection not supported")
	return nil, joinErrs([]error{
		arrowsErr,
		bsupErr,
		csvErr,
		jsonErr,
		lineErr,
		parquetErr,
		supErr,
		tsvErr,
		zeekErr,
	})
}

func isArrowStream(track *Track) error {
	// Streams created by Arrow 0.15.0 or later begin with a 4-byte
	// continuation indicator (0xffffffff) followed by a 4-byte
	// little-endian schema message length.  Older streams begin with the
	// length.
	buf := make([]byte, 4)
	if _, err := io.ReadFull(track, buf); err != nil {
		return err
	}
	if string(buf) == "\xff\xff\xff\xff" {
		// This looks like a continuation indicator.  Skip it.
		if _, err := io.ReadFull(track, buf); err != nil {
			return err
		}
	}
	if binary.LittleEndian.Uint32(buf) > 1048576 {
		// Prevent arrowio.NewReader from attempting to read an
		// unreasonable amount.
		return errors.New("schema message length exceeds 1 MiB")
	}
	track.Reset()
	zrc, err := arrowio.NewReader(super.NewContext(), track)
	if err != nil {
		return err
	}
	defer zrc.Close()
	_, err = zrc.Read()
	return err
}

func isCSVStream(track *Track, delim rune, name string) error {
	if line, err := bufio.NewReader(track).ReadSlice('\n'); err != nil {
		return fmt.Errorf("%s: line 1: %w", name, err)
	} else if !bytes.ContainsRune(line, delim) {
		return fmt.Errorf("%s: line 1: delimiter %q not found", name, delim)
	}
	track.Reset()
	return match(csvio.NewReader(super.NewContext(), track, csvio.ReaderOpts{Delim: delim}), name, 1)
}

func isJSONStream(track *Track, want int) error {
	r := jsonio.NewValReader(track)
	for range want {
		if _, err := r.Next(); err != nil {
			if errors.Is(err, io.EOF) {
				err = nil
			}
			return err
		}
	}
	return nil
}

func isParquetStream(ctx context.Context, track *Track) error {
	// a parquet stream starts with a 4-byte magic: PAR1 or PARE. If we find
	// this we probably have a parquet but to be sure we'll have to read the
	// entire stream till EOF and then check the footer.
	var buf [4]byte
	if _, err := io.ReadFull(track, buf[:]); err != nil {
		return err
	}
	if s := string(buf[:]); s != "PAR1" && s != "PARE" {
		return errors.New("invalid header")
	}
	if track.recorder != nil {
		track.Reset()
		b, err := io.ReadAll(track)
		if err != nil {
			return err
		}
		*track = *NewTrack(bytes.NewReader(b))
	}
	_, err := parquetio.NewReader(ctx, super.NewContext(), track.Reader(), nil, 1)
	return err
}

func joinErrs(errs []error) error {
	var b strings.Builder
	b.WriteString("format detection error")
	for _, e := range errs {
		b.WriteString("\n\t" + e.Error())
	}
	return errors.New(b.String())
}

func match(r sio.Reader, name string, want int) error {
	for range want {
		val, err := r.Read()
		if err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		if val == nil {
			return nil
		}
	}
	return nil
}
