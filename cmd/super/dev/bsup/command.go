package bsup

import (
	"bytes"
	"errors"
	"flag"
	"io"

	"github.com/superdb/super"
	"github.com/superdb/super/bsup"
	"github.com/superdb/super/cli/outputflags"
	"github.com/superdb/super/cmd/super/dev"
	"github.com/superdb/super/sbuf"
	"github.com/superdb/super/vector/vio"

	"github.com/superdb/super/pkg/charm"
	"github.com/superdb/super/pkg/storage"
)

var spec = &charm.Spec{
	Name:  "bsup",
	Usage: "bsup uri",
	Short: "dump BSUP metadata",
	Long: `
bsup decodes an input uri and emits the metadata sections in the format desired.`,
	New: New,
}

func init() {
	dev.Spec.Add(spec)
}

type Command struct {
	*dev.Command
	outputFlags outputflags.Flags
	fused       bool
	marshaler   *super.Marshaler
	vals        []super.Value
	writer      vio.Pusher
	reader      io.Reader
	sctx        *super.Context
}

func New(parent charm.Command, f *flag.FlagSet) (charm.Command, error) {
	c := &Command{Command: parent.(*dev.Command)}
	c.outputFlags.SetFlags(f)
	f.BoolVar(&c.fused, "fused", false, "emit container's fused type instead of meta data")
	return c, nil
}

func (c *Command) Run(args []string) error {
	ctx, cleanup, err := c.Init(&c.outputFlags)
	if err != nil {
		return err
	}
	defer cleanup()
	if len(args) != 1 {
		return errors.New("a single file is required")
	}
	uri, err := storage.ParseURI(args[0])
	if err != nil {
		return err
	}
	engine := storage.NewLocalEngine()
	r, err := engine.Get(ctx, uri)
	if err != nil {
		return err
	}
	defer r.Close()
	c.reader = r
	writer, err := c.outputFlags.Open(ctx, engine)
	if err != nil {
		return err
	}
	c.writer = writer
	c.sctx = super.NewContext()
	c.marshaler = super.NewMarshaler(c.sctx)
	c.marshaler.Decorate(super.StyleSimple)
	if c.fused {
		if err := c.emitFused(); err != nil {
			return err
		}
	} else {
		for {
			hdr, err := bsup.ReadHeader(r)
			if err != nil {
				return err
			}
			if hdr == nil {
				break
			}
			c.marshal(hdr)
			if err := c.frame(hdr); err != nil {
				return err
			}
		}
	}
	err = c.flush()
	if err2 := writer.Close(); err == nil {
		err = err2
	}
	return err
}

func (c *Command) emitFused() error {
	r, ok := c.reader.(io.ReaderAt)
	if !ok {
		return errors.New("need seekable input for -fused")
	}
	container := bsup.NewSeekable(c.sctx, r)
	typ, err := container.FusedType(c.sctx)
	if err != nil {
		return err
	}
	return c.emit(c.sctx.LookupTypeValue(typ))
}

func (c *Command) emit(val super.Value) error {
	c.vals = append(c.vals, val)
	if len(c.vals) > 100 {
		return c.flush()
	}
	return nil
}

func (c *Command) marshal(thing any) error {
	val, err := c.marshaler.Marshal(thing)
	if err != nil {
		return err
	}
	return c.emit(val)
}

func (c *Command) flush() error {
	if len(c.vals) != 0 {
		err := c.writer.Push(sbuf.Dematerialize(c.sctx, c.vals...))
		c.vals = c.vals[0:]
		return err
	}
	return nil
}

func (c *Command) discard(n int64) error {
	_, err := io.CopyN(io.Discard, c.reader, n)
	return err
}
func (c *Command) frame(hdr bsup.Header) error {
	switch hdr := hdr.(type) {
	case *bsup.ColumnHeader:
		return c.columnFrame(hdr)
	case *bsup.RowHeader:
		return c.rowFrame(hdr)
	case *bsup.SuperFooter:
		return c.superFooter(hdr)
	default:
		panic(hdr)
	}
}

func (c *Command) columnFrame(header *bsup.ColumnHeader) error {
	metaBytes, err := io.ReadAll(io.LimitReader(c.reader, int64(header.MetadataSize)))
	if err != nil {
		return err
	}
	fit := bsup.NewSeekable(c.sctx, bytes.NewReader(metaBytes))
	for {
		frame, err := fit.Next()
		if err != nil {
			return err
		}
		if frame == nil {
			break
		}
		rowframe, ok := frame.(*bsup.RowFrame)
		if !ok {
			return errors.New("non-row data in column metadata")
		}
		vals, err := rowframe.DeserializeValues()
		if err != nil {
			return err
		}
		for _, val := range vals {
			c.emit(val.Copy())
		}
	}
	typedefsBytes := make([]byte, header.TypedefsSize)
	if _, err := io.ReadFull(c.reader, typedefsBytes); err != nil {
		return err
	}
	if err := c.marshalTypeDefs(typedefsBytes); err != nil {
		return err
	}
	fusedTypeBytes := make([]byte, header.FusedTypeSize)
	if _, err := io.ReadFull(c.reader, fusedTypeBytes); err != nil {
		return err
	}
	fusedType, err := c.sctx.LookupByValue(fusedTypeBytes)
	if err != nil {
		return err
	}
	if err := c.marshal(struct {
		Kind string
		Type super.Type
	}{
		Kind: "ColumnFrameFusedType",
		Type: fusedType,
	}); err != nil {
		return err
	}
	_, segSize := header.SegmentsSection()
	return c.discard(segSize)
}

func (c *Command) rowFrame(header *bsup.RowHeader) error {
	fusedTypeBytes := make([]byte, header.FusedTypeSize)
	if _, err := io.ReadFull(c.reader, fusedTypeBytes); err != nil {
		return err
	}
	fusedType, err := c.sctx.LookupByValue(fusedTypeBytes)
	if err != nil {
		return err
	}
	typedefsBytes := make([]byte, header.TypedefsSize)
	if _, err := io.ReadFull(c.reader, typedefsBytes); err != nil {
		return err
	}
	if err := c.marshalTypeDefs(typedefsBytes); err != nil {
		return err
	}

	if err := c.marshal(struct {
		Kind string
		Type super.Type
	}{
		Kind: "RowFrameFusedType",
		Type: fusedType,
	}); err != nil {
		return err
	}
	_, dataSize := header.DataSection()
	if err := c.marshal(struct {
		Kind string
		Size int64
	}{
		Kind: "RowFrameData",
		Size: dataSize,
	}); err != nil {
		return err
	}
	return c.discard(dataSize)
}

func (c *Command) superFooter(header *bsup.SuperFooter) error {
	fusedTypeBytes := make([]byte, header.TypedefsSize())
	if _, err := io.ReadFull(c.reader, fusedTypeBytes); err != nil {
		return err
	}
	fusedType, err := c.sctx.LookupByValue(fusedTypeBytes)
	if err != nil {
		return err
	}
	if err := c.discard(bsup.SuperFooterPad); err != nil {
		return err
	}
	return c.marshal(struct {
		Kind string
		Type super.Type
	}{
		Kind: "SuperFrameFusedType",
		Type: fusedType,
	})
}

func (c *Command) marshalTypeDefs(bytes []byte) error {
	id := uint32(super.IDTypeComplex)
	for len(bytes) > 0 {
		var desc any
		bytes, desc = decodeTypeDef(id, bytes)
		if desc != nil {
			val, err := c.marshaler.Marshal(desc)
			if err != nil {
				return err
			}
			c.emit(val)
		}
		id++
	}
	return nil
}

func DecodeTypeDefs(bytes []byte, offset int) ([]any, error) {
	id := uint32(offset + super.IDTypeComplex)
	var out []any
	for len(bytes) > 0 {
		var desc any
		bytes, desc = decodeTypeDef(id, bytes)
		if desc != nil {
			out = append(out, desc)
		}
		id++
	}
	return out, nil
}

func decodeTypeDef(slot uint32, bytes []byte) ([]byte, any) {
	var out any
	typedef := bytes[0]
	bytes = bytes[1:]
	var n int
	var name string
	var id uint32
	switch typedef {
	case super.TypeDefNamed:
		name, bytes = super.DecodeName(bytes)
		if bytes == nil {
			return nil, errInfo(slot, "TypeDefNamed", "at name field")
		}
		id, bytes = super.DecodeFixedID(bytes)
		if bytes == nil {
			return nil, errInfo(slot, "TypeDefNamed", "at ID field")
		}
		out = &struct {
			Kind string
			Slot uint32
			Name string
			ID   uint32
		}{
			Kind: "TypeDefNamed",
			Slot: slot,
			Name: name,
			ID:   id,
		}
	case super.TypeDefRecord:
		type Field struct {
			Name string
			ID   uint32
		}
		n, bytes = super.DecodeLength(bytes)
		if bytes == nil {
			return nil, errInfo(slot, "TypeDefRecord", "at length field")
		}
		var fields []Field
		for range n {
			name, bytes = super.DecodeName(bytes)
			if bytes == nil {
				return nil, errInfo(slot, "TypeDefRecord", "at field name field")
			}
			id, bytes = super.DecodeID(bytes)
			if bytes == nil {
				return nil, errInfo(slot, "TypeDefRecord", "at field ID field")
			}
			fields = append(fields, Field{name, id})
		}
		out = &struct {
			Kind   string
			Slot   uint32
			Fields []Field
		}{
			Kind:   "TypeDefRecord",
			Slot:   slot,
			Fields: fields,
		}
	case super.TypeDefArray:
		bytes, out = wrapped(bytes, "TypeDefArray", slot)
	case super.TypeDefSet:
		bytes, out = wrapped(bytes, "TypeDefSet", slot)
	case super.TypeDefError:
		bytes, out = wrapped(bytes, "TypeDefError", slot)
	case super.TypeDefFusion:
		bytes, out = wrapped(bytes, "TypeDefFusion", slot)
	case super.TypeDefMap:
		var keyID, valID uint32
		keyID, bytes = super.DecodeID(bytes)
		if bytes == nil {
			return nil, errInfo(slot, "TypeDefMap", "at key ID")
		}
		valID, bytes = super.DecodeID(bytes)
		if bytes == nil {
			return nil, errInfo(slot, "TypeDefMap", "at value ID")
		}
		out = &struct {
			Kind  string
			Slot  uint32
			KeyID uint32
			ValID uint32
		}{
			Kind:  "TypeDefMap",
			Slot:  slot,
			KeyID: keyID,
			ValID: valID,
		}
	case super.TypeDefUnion:
		n, bytes = super.DecodeLength(bytes)
		if bytes == nil {
			return nil, errInfo(slot, "TypeDefUnion", "at length field")
		}
		if n > super.MaxUnionTypes {
			return nil, errInfo(slot, "TypeDefUnion", "at length field (size exceed)")
		}
		var ids []uint32
		for range n {
			id, bytes = super.DecodeID(bytes)
			if bytes == nil {
				return nil, errInfo(slot, "TypeDefUnion", "in type ID list")
			}
			ids = append(ids, id)
		}
		out = &struct {
			Kind string
			Slot uint32
			IDs  []uint32
		}{
			Kind: "TypeDefUnion",
			Slot: slot,
			IDs:  ids,
		}
	case super.TypeDefEnum:
		n, bytes = super.DecodeLength(bytes)
		if bytes == nil {
			return nil, errInfo(slot, "TypeDefEnum", "at length field")
		}
		if n > super.MaxEnumSymbols {
			return nil, errInfo(slot, "TypeDefEnum", "at length field (size exceed)")
		}
		var names []string
		for range n {
			name, bytes = super.DecodeName(bytes)
			if bytes == nil {
				return nil, errInfo(slot, "TypeDefEnum", "at enum symbol")
			}
			names = append(names, name)
		}
		out = &struct {
			Kind  string
			Slot  uint32
			Names []string
		}{
			Kind:  "TypeDefEnum",
			Slot:  slot,
			Names: names,
		}
	default:
		out = &struct {
			Kind string
			Slot uint32
			Code int
		}{
			Kind: "Bad TypeDef code",
			Slot: slot,
			Code: int(typedef),
		}
		bytes = nil
	}
	return bytes, out
}

func wrapped(bytes []byte, kind string, slot uint32) ([]byte, any) {
	var id uint32
	id, bytes = super.DecodeID(bytes)
	if bytes == nil {
		return nil, errInfo(slot, kind, "at ID field")
	}
	return bytes, &struct {
		Kind string
		Slot uint32
		ID   uint32
	}{
		Kind: kind,
		Slot: slot,
		ID:   id,
	}
}

func errInfo(slot uint32, typedef, message string) any {
	return &struct {
		Kind    string
		Slot    uint32
		TypeDef string
		Where   string
	}{
		Kind:    "Decode Error",
		Slot:    slot,
		TypeDef: typedef,
		Where:   message,
	}
}
