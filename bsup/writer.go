package bsup

import (
	"encoding/binary"
	"fmt"
	"io"

	"github.com/superdb/super"
	"github.com/superdb/super/scode"
	"github.com/superdb/super/sio"
	"github.com/superdb/super/vector"
	"github.com/superdb/super/vector/vbuild"
	"github.com/superdb/super/vector/vio"
)

type WriterOpts struct {
	Compress bool
	FrameCap uint64
	Rows     bool
}

const DefaultFrameCap = 20_000

// ColumnWriter implements the vio.Pusher interface. A Pusher creates a vector
// BSUP object from a stream of vector.Any.
type ColumnWriter struct {
	writer     io.WriteCloser
	dynamic    *vbuild.DynamicBuilder
	sctx       *super.Context
	fuser      fuser
	size       uint64
	ctrl       *RowWriter
	framecap   uint64
	frameStats [][]ColumnStats
	frameOffs  []uint64
}

var _ vio.Pusher = (*ColumnWriter)(nil)

func NewColumnWriter(w io.WriteCloser) *ColumnWriter {
	return NewColumnWriterWithCap(w, DefaultFrameCap)
}

func NewColumnWriterWithCap(w io.WriteCloser, frameCap uint64) *ColumnWriter {
	sctx := super.NewContext()
	return &ColumnWriter{
		writer:   w,
		dynamic:  vbuild.NewDynamicBuilder(),
		sctx:     sctx,
		fuser:    newFuser(sctx),
		framecap: frameCap,
	}
}

func NewWriterWithOpts(w io.WriteCloser, opt WriterOpts) vio.PushCloser {
	if opt.Rows {
		writer := NewRowWriter(w)
		writer.framecap = uint32(opt.FrameCap)
		return writer
	}
	writer := NewColumnWriter(w)
	writer.framecap = opt.FrameCap
	return writer
}

func (c *ColumnWriter) Close() error {
	firstErr := c.pushFrame()
	if firstErr == nil {
		statsBytes := encodeStats(c.frameOffs, c.frameStats)
		_, firstErr = writeFooter(c.writer, c.size, c.fuser.typeBytes(), statsBytes)
	}
	if err := c.writer.Close(); err != nil && firstErr == nil {
		firstErr = err
	}
	return firstErr
}

func (c *ColumnWriter) WriteControl(val super.Value) error {
	if err := c.pushFrame(); err != nil {
		return err
	}
	if c.ctrl == nil {
		c.ctrl = NewRowWriter(sio.NopCloser(c.writer))
	}
	// The control frame is part of the current super frame, so its size
	// counts toward the super frame size and the offsets of later frames.
	size := c.ctrl.size
	err := c.ctrl.write(val, true)
	c.size += c.ctrl.size - size
	return err
}

// WriteSuperFrame writes the entire BSUP columns file from a single vector.
// It should be called only once without any calls to Push.
func (c *ColumnWriter) WriteSuperFrame(vec vector.Any) (uint64, error) {
	c.Push(vec)
	err := c.pushFrame()
	return c.size, err
}

// FusedType returns the fused type of all values written so far (the type
// whose encoding is written into the SuperFooter), or nil if nothing has been
// written.  The type is in the writer's own type context.  Call it after
// WriteSuperFrame or the final Push to get the type of the whole object.
func (c *ColumnWriter) FusedType() super.Type {
	return c.fuser.fuser.FusedType()
}

func (c *ColumnWriter) Push(vec vector.Any) error {
	if vec.Len() != 0 {
		c.dynamic.Write(vec)
		if c.dynamic.Len() >= uint32(c.framecap) {
			return c.pushFrame()
		}
	}
	return nil
}

// pushFrame encodes all of the vectors received so far and flushes a serialized
// ColumnFrame to the writer.  All the state is flushed and reset and a new frame
// will begin on the next Push (except for the SuperFooter size and fusion type).
func (c *ColumnWriter) pushFrame() error {
	vec := c.dynamic.BuildDynamic()
	if vec.Len() == 0 {
		return nil
	}
	enc := NewDynamicEncoder(vec)
	encoder := enc.typedefs.Encoder()
	// Compute the fused type for just the ColumnFrame.  This type is fed into
	// the SuperFrame fuser below so we have a type for each ColumnFrame and a type
	// for the SuperFrame.  The ColumnFrame types will be useful for future frame pruning.
	fusedType := c.fuser.fuseVecsStandalone(vec.Values)
	fusedTypeID := encoder.LookupType(fusedType)
	vtypes, locators, dataSectionSize, err := enc.Encode()
	if err != nil {
		return fmt.Errorf("system error: could not encode BSUP vectors: %w", err)
	}
	typedefs := encoder.Bytes()
	c.frameOffs = append(c.frameOffs, c.size)
	c.frameStats = append(c.frameStats, enc.Stats())
	header := newColumnHeader(fusedTypeID, uint64(len(vtypes)), uint64(len(locators)), uint64(len(typedefs)), dataSectionSize)
	if _, err := c.writer.Write(header.Serialize()); err != nil {
		return fmt.Errorf("system error: could not write BSUP header: %w", err)
	}
	for _, section := range [][]byte{vtypes, locators, typedefs} {
		if _, err := c.writer.Write(section); err != nil {
			return fmt.Errorf("system error: could not write BSUP column frame: %w", err)
		}
	}
	// Data section, whose segments must be where they were located.
	w := &countingWriter{writer: c.writer}
	if err := enc.Emit(w); err != nil {
		return fmt.Errorf("system error: could not write BSUP data section: %w", err)
	}
	if w.n != dataSectionSize {
		return fmt.Errorf("system error: BSUP data section is %d bytes but its segments were located in %d bytes", w.n, dataSectionSize)
	}
	// Update SuperFrame state a create new builder so we start fresh
	// for the next ColumnFrame.
	c.size += header.FrameSize
	if fusedType != nil {
		c.fuser.fuse(fusedType)
	}
	c.dynamic = vbuild.NewDynamicBuilder()
	return nil
}

// countingWriter counts the bytes written to writer.
type countingWriter struct {
	writer io.Writer
	n      uint64
}

func (c *countingWriter) Write(b []byte) (int, error) {
	n, err := c.writer.Write(b)
	c.n += uint64(n)
	return n, err
}

func writeFooter(w io.Writer, size uint64, fusedTypeBytes, statsBytes []byte) (uint64, error) {
	if size == 0 {
		return 0, nil
	}
	footer := newSuperFooter(size, uint64(len(fusedTypeBytes)), uint64(len(statsBytes)))
	if _, err := w.Write(footer.Serialize()); err != nil {
		return 0, err
	}
	if _, err := w.Write(fusedTypeBytes); err != nil {
		return 0, err
	}
	if _, err := w.Write(statsBytes); err != nil {
		return 0, err
	}
	var trailer [8]byte
	binary.LittleEndian.PutUint64(trailer[:], footer.Size())
	if _, err := w.Write(trailer[:]); err != nil {
		return 0, err
	}
	return footer.Size(), nil
}

// RowWriter implements both vio.Pusher and sio.Writer. A Pusher creates a super frame
// in rows format from a stream of vector.Any.
type RowWriter struct {
	writer     io.WriteCloser
	typedefs   *super.TypeDefsEncoder
	sctx       *super.Context
	fuser      fuser
	superfuser fuser
	size       uint64
	bytes      []byte
	len        uint32
	framecap   uint32
}

var _ vio.Pusher = (*RowWriter)(nil)
var _ sio.Writer = (*RowWriter)(nil)

func NewRowWriter(w io.WriteCloser) *RowWriter {
	sctx := super.NewContext()
	return &RowWriter{
		writer:     w,
		typedefs:   super.NewTypeDefsEncoder(),
		sctx:       sctx,
		fuser:      newFuser(sctx),
		superfuser: newFuser(sctx),
		framecap:   DefaultFrameCap,
	}
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
	if r.len >= r.framecap {
		return r.pushFrame(false)
	}
	return nil
}

func (r *RowWriter) Write(val super.Value) error {
	return r.write(val, false)
}

func (r *RowWriter) write(val super.Value, ctrl bool) error {
	typ := val.Type()
	r.serialize(typ, val.Bytes())
	r.fuser.fuse(typ)
	r.superfuser.fuse(typ)
	r.len++
	if r.len >= r.framecap || ctrl {
		return r.pushFrame(ctrl)
	}
	return nil
}

func (r *RowWriter) serialize(ext super.Type, bytes []byte) {
	id := r.typedefs.LookupType(ext)
	r.bytes = binary.AppendUvarint(r.bytes, uint64(id))
	r.bytes = binary.AppendUvarint(r.bytes, uint64(len(bytes)))
	r.bytes = append(r.bytes, bytes...)
}

func (r *RowWriter) pushFrame(ctrl bool) error {
	if len(r.bytes) == 0 {
		return nil
	}
	// Compute the fused type for just the ColumnFrame.  This type is fed into
	// the SuperFrame fuser below so we have a type for each ColumnFrame and a type
	// for the SuperFrame.  The ColumnFrame types will useful for future frame pruning.
	fusedTypeID := r.typedefs.LookupType(r.fuser.fuser.FusedType())
	typedefs := r.typedefs.Bytes()
	header := newRowHeader(fusedTypeID, uint64(len(typedefs)), uint64(len(r.bytes)))
	if _, err := r.writer.Write(header.Serialize(ctrl)); err != nil {
		return fmt.Errorf("could not write BSUP header: %w", err)
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
	err := r.pushFrame(false)
	if err == nil {
		var n uint64
		// Do not collects stats for rows.
		n, err = writeFooter(r.writer, r.size, r.superfuser.typeBytes(), nil)
		r.size += n
	}
	return r.size, err
}

type fuser struct {
	sctx   *super.Context
	fuser  *super.Fuser
	defs   *super.TypeDefsEncoder
	mapper *super.TypeDefsMapper
}

func newFuser(sctx *super.Context) fuser {
	defs := super.NewTypeDefsEncoder()
	return fuser{
		sctx:   sctx,
		fuser:  super.NewFuser(sctx),
		defs:   defs,
		mapper: super.NewTypeDefsMapper(sctx, &defs.TypeDefs),
	}
}

func (f fuser) fuse(typ super.Type) {
	f.fuser.Fuse(f.translate(typ))
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

func (f fuser) fuseVecsStandalone(vecs []vector.Any) super.Type {
	fuser := super.NewFuser(f.sctx)
	for _, vec := range vecs {
		fuser.Fuse(f.translate(vec.Type()))
	}
	return fuser.FusedType()
}

func (f fuser) translate(typ super.Type) super.Type {
	return f.mapper.LookupType(f.defs.LookupType(typ))
}

func (f fuser) typeBytes() []byte {
	typ := f.fuser.FusedType()
	if typ == nil {
		return nil
	}
	return f.sctx.LookupTypeValue(typ).Bytes()
}

func (f *fuser) reset() {
	f.fuser = super.NewFuser(f.sctx)
}
