package spill

import (
	"bufio"
	"context"
	"os"

	"github.com/superdb/super"
	"github.com/superdb/super/bsup"
	"github.com/superdb/super/pkg/bufwriter"
	"github.com/superdb/super/pkg/fs"
	"github.com/superdb/super/sio"
	"github.com/superdb/super/sio/bsupio"
)

// File provides a means to write a sequence of Super values to temporary
// storage then read them back.  This is used for processing large batches of
// data that do not fit in memory and/or cannot be shuffled to a peer worker,
// but can be processed in multiple passes.  File implements sio.Reader and
// sio.Writer.
type File struct {
	reader sio.Reader
	writer *bsup.RowWriter
	file   *os.File
}

// NewFile returns a File.  Records should be written to File via the sio.Writer
// interface, followed by a call to the Rewind method, followed by reading
// records via the sio.Reader interface.
func NewFile(f *os.File) *File {
	return &File{
		writer: bsup.NewRowWriter(bufwriter.New(sio.NopCloser(f))),
		file:   f,
	}
}

func NewFileWithPath(path string) (*File, error) {
	f, err := fs.Create(path)
	if err != nil {
		return nil, err
	}
	return NewFile(f), nil
}

func (f *File) Rewind(ctx context.Context, sctx *super.Context) error {
	// Close the writer to flush any pending output but since we
	// wrapped the file in a sio.NopCloser, the file will stay open.
	if err := f.writer.Close(); err != nil {
		return err
	}
	f.writer = nil
	if _, err := f.file.Seek(0, 0); err != nil {
		return err
	}
	var err error
	f.reader, err = bsupio.NewValueReader(ctx, sctx, bufio.NewReader(f.file))
	return err
}

// CloseAndRemove closes and removes the underlying file.
func (r *File) CloseAndRemove() error {
	err := r.file.Close()
	if rmErr := os.Remove(r.file.Name()); err == nil {
		err = rmErr
	}
	return err
}

func (f *File) Size() (int64, error) {
	info, err := f.file.Stat()
	if err != nil {
		return 0, err
	}
	return info.Size(), nil
}
