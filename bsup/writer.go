package bsup

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"runtime/debug"

	"github.com/superdb/super"
	"github.com/superdb/super/scode"
	"github.com/superdb/super/sio"
	"github.com/superdb/super/vector"
	"github.com/superdb/super/vector/vbuild"
	"github.com/superdb/super/vector/vio"
)

var maxFrameSize uint32 = 120_000

// XXX a future PR will wire in compress / thresh options to command line.
// XXX Rows is the key flag we need for the rows writer.
type WriterOpts struct {
	Compress bool
	// FrameThresh is the minimum frame size in uncompressed bytes.
	FrameThresh int
	Rows        bool
}

// ColumnWriter implements the vio.Pusher interface. A Pusher creates a vector
// BSUP object from a stream of vector.Any.
type ColumnWriter struct {
	writer  io.WriteCloser
	dynamic *vbuild.DynamicBuilder
	sctx    *super.Context
	fuser   fuser
	size    int64
}

var _ vio.Pusher = (*ColumnWriter)(nil)

func NewColumnWriter(w io.WriteCloser) *ColumnWriter {
	sctx := super.NewContext()
	return &ColumnWriter{
		writer:  w,
		dynamic: vbuild.NewDynamicBuilder(),
		sctx:    sctx,
		fuser:   newFuser(sctx),
	}
}

func NewWriterWithOpts(w io.WriteCloser, opt WriterOpts) vio.PushCloser {
	if opt.Rows {
		return NewRowWriter(w)
	}
	return NewColumnWriter(w)
}

func (c *ColumnWriter) Close() error {
	firstErr := c.pushFrame(false)
	if firstErr == nil {
		_, firstErr = writeFooter(c.writer, uint64(c.size), c.fuser.typeBytes())
	}
	if err := c.writer.Close(); err != nil && firstErr == nil {
		firstErr = err
	}
	return firstErr
}

func (c *ColumnWriter) Push(vec vector.Any) error {
	if vec.Len() != 0 {
		if _, ok := vec.(*vector.Control); ok {
			fmt.Println("CTRL")
			debug.PrintStack()
		}
		if _, ok := vec.(*vector.Labeled); ok {
			fmt.Println("LABEL")
			debug.PrintStack()
		}
		//fmt.Println("VEC", vector.Format(vec))
		if d, ok := vec.(*vector.Dynamic); ok {
			fmt.Println("TAGS", d.Tags)
		}
		c.dynamic.Write(vec)
		if c.dynamic.Len() >= maxFrameSize {
			return c.pushFrame(false)
		}
	}
	return nil
}

func (c *ColumnWriter) WriteControl(val super.Value) error {
	if err := c.pushFrame(false); err != nil {
		return err
	}
	builder := vector.NewValueBuilder(val.Type())
	builder.Write(val.Bytes())
	c.Push(builder.Build(c.sctx))
	return c.pushFrame(true)
}

// pushFrame encodes all of the vectors received so far and flushes a serialized
// ColumnFrame to the writer.  All the state is flushed and reset and a new frame
// will begin on the next Push (except for the SuperFooter size and fusion type).
func (c *ColumnWriter) pushFrame(oob bool) error {
	vec := c.dynamic.BuildDynamic()
	if vec.Len() == 0 {
		return nil
	}
	// Compute the fused type for just the ColumnFrame.  This type is fed into
	// the SuperFrame fuser below so we have a type for each ColumnFrame and a type
	// for the SuperFrame.  The ColumnFrame types will useful for future frame pruning.
	var fusedTypeBytes []byte
	var fusedType super.Type
	if !oob {
		fusedType = fuse(c.fuser.sctx, vec)
		fusedTypeBytes = c.fuser.sctx.LookupTypeValue(fusedType).Bytes()
	}
	enc := NewDynamicEncoder(vec)
	root, dataSectionSize, err := enc.Encode()
	if err != nil {
		return fmt.Errorf("system error: could not encode BSUP metadata: %w", err)
	}
	var metaData bytes.Buffer
	rowWriter := NewRowWriter(sio.NopCloser(&metaData))
	cctx := enc.cctx
	m := super.NewMarshaler(cctx.local)
	m.Decorate(super.StyleSimple)
	for id := range len(cctx.metas) {
		val, err := m.Marshal(cctx.Lookup(ID(id)))
		if err != nil {
			return fmt.Errorf("could not marshal BSUP metadata: %w", err)
		}
		if err := rowWriter.Write(val); err != nil {
			return fmt.Errorf("could not write BSUP metadata: %w", err)
		}
	}
	metaDataSize, err := rowWriter.EndSuperFrame()
	if err != nil {
		return err
	}
	typedefs := cctx.typedefs.Bytes()
	header := newColumnHeader(oob, root, uint64(metaDataSize), uint64(len(typedefs)), uint64(len(fusedTypeBytes)), dataSectionSize)
	if _, err := c.writer.Write(header.Serialize()); err != nil {
		return fmt.Errorf("system error: could not write BSUP header: %w", err)
	}
	if _, err := c.writer.Write(metaData.Bytes()); err != nil {
		return fmt.Errorf("system error: could not write BSUP metadata section: %w", err)
	}
	if _, err := c.writer.Write(typedefs); err != nil {
		return fmt.Errorf("system error: could not write BSUP typedefs: %w", err)
	}
	if _, err := c.writer.Write(fusedTypeBytes); err != nil {
		return fmt.Errorf("system error: could not write BSUP fused type: %w", err)
	}
	// Data section
	if err := enc.Emit(c.writer); err != nil {
		return fmt.Errorf("system error: could not write BSUP data section: %w", err)
	}
	// Update SuperFrame state a create new builder so we start fresh
	// for the next ColumnFrame.
	c.size += int64(header.FrameSize)
	if fusedType != nil {
		c.fuser.fuse(fusedType)
	}
	c.dynamic = vbuild.NewDynamicBuilder()
	return nil
}

func fuse(sctx *super.Context, dynamic *vector.Dynamic) super.Type {
	fuser := super.NewFuser(sctx, true)
	for _, vec := range dynamic.Values {
		typ, err := sctx.TranslateType(vec.Type())
		if err != nil {
			panic(err)
		}
		fuser.Fuse(typ)
	}
	return fuser.Type()
}

func writeFooter(w io.Writer, size uint64, fusedTypeBytes []byte) (uint64, error) {
	if size == 0 {
		return 0, nil
	}
	footer := newSuperFooter(size, uint64(len(fusedTypeBytes)))
	if _, err := w.Write(footer.Serialize()); err != nil {
		return 0, err
	}
	if _, err := w.Write(fusedTypeBytes); err != nil {
		return 0, err
	}
	var trailer [8]byte
	binary.LittleEndian.PutUint64(trailer[:], footer.FooterSize)
	if _, err := w.Write(trailer[:]); err != nil {
		return 0, err
	}
	return footer.Size(), nil
}

// RowWriter implements both vio.Pusher and sio.Writer. A Pusher creates a super frame
// in rows format from a stream of vector.Any.
type RowWriter struct {
	writer     io.WriteCloser
	typedefs   *super.TypeDefs
	sctx       *super.Context
	fuser      fuser
	superfuser fuser
	size       uint64
	bytes      []byte
	len        uint32
}

var _ vio.Pusher = (*RowWriter)(nil)
var _ sio.Writer = (*RowWriter)(nil)

func NewRowWriter(w io.WriteCloser) *RowWriter {
	sctx := super.NewContext()
	return &RowWriter{
		writer:     w,
		typedefs:   super.NewTypeDefs(),
		sctx:       sctx,
		fuser:      newFuser(sctx),
		superfuser: newFuser(sctx),
	}
}

func (r *RowWriter) Position() int64 {
	return int64(r.size)
}

func (r *RowWriter) Push(vec vector.Any) error {
	r.fuser.fuseVec(vec)
	r.superfuser.fuseVec(vec)
	var b scode.Builder
	if d, ok := vec.(*vector.Dynamic); ok {
		for slot := range vec.Len() {
			b.Truncate()
			vec.Serialize(&b, slot)
			r.serialize(d.TypeOf(slot), b.Bytes().Body())
		}
	} else {
		typ := vec.Type()
		for slot := range vec.Len() {
			b.Truncate()
			vec.Serialize(&b, slot)
			r.serialize(typ, b.Bytes().Body())
		}
	}
	r.len += vec.Len()
	if r.len >= maxFrameSize {
		return r.pushFrame()
	}
	return nil
}

func (r *RowWriter) Write(val super.Value) error {
	typ := val.Type()
	r.serialize(typ, val.Bytes())
	r.fuser.fuse(typ)
	r.superfuser.fuse(typ)
	r.len++
	if r.len >= maxFrameSize {
		return r.pushFrame()
	}
	return nil
}

func (r *RowWriter) serialize(ext super.Type, bytes []byte) {
	id := r.typedefs.LookupType(ext)
	r.bytes = binary.AppendUvarint(r.bytes, uint64(id))
	r.bytes = binary.AppendUvarint(r.bytes, uint64(len(bytes)))
	r.bytes = append(r.bytes, bytes...)
}

func (r *RowWriter) pushFrame() error {
	if len(r.bytes) == 0 {
		return nil
	}
	// Compute the fused type for just the ColumnFrame.  This type is fed into
	// the SuperFrame fuser below so we have a type for each ColumnFrame and a type
	// for the SuperFrame.  The ColumnFrame types will useful for future frame pruning.
	fusedTypeBytes := r.fuser.typeBytes()
	typedefs := r.typedefs.Bytes()
	header := newRowHeader(false, uint64(len(typedefs)), uint64(len(fusedTypeBytes)), uint64(len(r.bytes)))
	if _, err := r.writer.Write(header.Serialize()); err != nil {
		return fmt.Errorf("could not write BSUP header: %w", err)
	}
	if _, err := r.writer.Write(fusedTypeBytes); err != nil {
		return fmt.Errorf("could not write BSUP fused type: %w", err)
	}
	if _, err := r.writer.Write(typedefs); err != nil {
		return fmt.Errorf("could not write BSUP typedefs: %w", err)
	}
	if _, err := r.writer.Write(r.bytes); err != nil {
		return fmt.Errorf("could not write BSUP data: %w", err)
	}
	// Update SuperFrame state a create new builder so we start fresh
	// for the next ColumnFrame.
	r.size += header.FrameSize
	r.fuser.reset()
	r.bytes = r.bytes[:0]
	r.len = 0
	r.typedefs.Reset()
	return nil
}

func (r *RowWriter) Close() error {
	_, firstErr := r.EndSuperFrame()
	if err := r.writer.Close(); err != nil && firstErr == nil {
		firstErr = err
	}
	return firstErr
}

func (r *RowWriter) EndSuperFrame() (uint64, error) {
	err := r.pushFrame()
	if err == nil {
		var n uint64
		n, err = writeFooter(r.writer, r.size, r.superfuser.typeBytes())
		r.size += n
	}
	return r.size, err
}

type fuser struct {
	sctx  *super.Context
	fuser *super.Fuser
}

func newFuser(sctx *super.Context) fuser {
	return fuser{
		sctx:  sctx,
		fuser: super.NewFuser(sctx, true),
	}
}

func (f fuser) fuse(typ super.Type) {
	typ, err := f.sctx.TranslateType(typ)
	if err != nil {
		panic(err)
	}
	f.fuser.Fuse(typ)
}

func (f fuser) fuseVec(vec vector.Any) {
	if vec.Len() == 0 {
		return
	}
	if d, ok := vec.(*vector.Dynamic); ok {
		for _, vec := range d.Values {
			f.fuseVec(vec)
		}
	} else {
		f.fuse(vec.Type())
	}
}

func (f fuser) typeBytes() []byte {
	typ := f.fuser.Type()
	if typ == nil {
		return nil
	}
	return f.sctx.LookupTypeValue(typ).Bytes()
}

func (f *fuser) reset() {
	f.fuser = super.NewFuser(f.sctx, true)
}
