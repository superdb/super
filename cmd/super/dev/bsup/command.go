package bsup

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"io"
	"os"

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
	headers     bool
	fused       bool
	metas       bool
	typedefs    bool
	metabytes   bool
	stats       bool
	marshaler   *super.Marshaler
	vals        []super.Value
	writer      vio.Pusher
	w           io.Writer
	sctx        *super.Context
	limit       int
	framelimit  int
	count       int
	frames      int
}

func New(parent charm.Command, f *flag.FlagSet) (charm.Command, error) {
	c := &Command{Command: parent.(*dev.Command)}
	c.outputFlags.SetFlags(f)
	f.BoolVar(&c.headers, "headers", true, "emit file's headers")
	f.BoolVar(&c.metas, "metas", false, "emit BSUP columns metas")
	f.BoolVar(&c.metabytes, "metabytes", false, "dump raw BSUP columns metas as BSUP row frames to stdout")
	f.BoolVar(&c.typedefs, "typedefs", false, "emit file's typedefs table")
	f.BoolVar(&c.fused, "fused", false, "emit the fused types")
	f.BoolVar(&c.stats, "stats", false, "emit stats section")
	f.IntVar(&c.limit, "limit", 0, "cap number of values output")
	f.IntVar(&c.framelimit, "framelimit", 0, "cap number of frames output")
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
	reader := bsup.NewRawReader(r)
	if c.metabytes {
		return c.dumpMetaBytes(ctx, reader)
	}
	writer, err := c.outputFlags.Open(ctx, engine)
	if err != nil {
		return err
	}
	c.writer = writer
	err = c.dump(ctx, reader)
	if err2 := c.flush(); err == nil {
		err = err2
	}
	if err2 := writer.Close(); err == nil {
		err = err2
	}
	return err
}

func (c *Command) dump(ctx context.Context, reader *bsup.RawReader) error {
	c.sctx = super.NewContext()
	c.marshaler = super.NewMarshaler(c.sctx)
	c.marshaler.Decorate(super.StyleSimple)
	for !c.stop() && ctx.Err() == nil {
		frame, err := reader.Next()
		if err != nil {
			return err
		}
		if frame == nil {
			return nil
		}
		if err := c.frame(frame); err != nil {
			return err
		}
		c.frames++
	}
	return ctx.Err()
}

func (c *Command) dumpMetaBytes(ctx context.Context, reader *bsup.RawReader) error {
	for !c.stop() && ctx.Err() == nil {
		frame, err := reader.Next()
		if err != nil {
			return err
		}
		if frame == nil {
			return nil
		}
		switch frame := frame.(type) {
		case *bsup.RawColFrame:
			metaBytes, err := frame.Metadata()
			if err != nil {
				return err
			}
			if _, err = os.Stdout.Write(metaBytes); err != nil {
				return err
			}
		case *bsup.RawRowFrame:
		case *bsup.RawSuperFooter:
		default:
			panic(frame)
		}
		c.frames++
	}
	return ctx.Err()
}

func (c *Command) stop() bool {
	if c.limit != 0 && c.count >= c.limit {
		return true
	}
	return c.framelimit != 0 && c.frames >= c.framelimit
}

func (c *Command) emit(val super.Value) error {
	if c.limit != 0 && c.count >= c.limit {
		return nil
	}
	c.vals = append(c.vals, val)
	c.count++
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

func wrapType(kind string, typ any) any {
	return &struct {
		Kind string
		Type any
	}{
		Kind: kind,
		Type: typ,
	}
}

func (c *Command) flush() error {
	if len(c.vals) != 0 {
		err := c.writer.Push(sbuf.Dematerialize(c.sctx, c.vals...))
		c.vals = c.vals[:0]
		return err
	}
	return nil
}

func (c *Command) frame(frame bsup.RawFrame) error {
	switch frame := frame.(type) {
	case *bsup.RawColFrame:
		return c.columnFrame(frame)
	case *bsup.RawRowFrame:
		return c.rowFrame(frame)
	case *bsup.RawSuperFooter:
		return c.superFooter(frame)
	default:
		panic(frame)
	}
}

func (c *Command) columnFrame(frame *bsup.RawColFrame) error {
	if c.headers {
		if err := c.marshal(frame.Header()); err != nil {
			return err
		}
	}
	if c.metas {
		metaBytes, err := frame.Metadata()
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
				if err := c.emit(val.Copy()); err != nil {
					return err
				}
			}
		}
	}
	if c.typedefs {
		typedefsBytes, err := frame.Typedefs()
		if err != nil {
			return err
		}
		if err := c.marshalTypeDefs(typedefsBytes); err != nil {
			return err
		}
	}
	if c.fused {
		typedefsBytes, err := frame.Typedefs()
		if err != nil {
			return err
		}
		fusedType, err := c.translateTypeID(typedefsBytes, frame.Header().FusedType)
		if err != nil {
			return err
		}
		if err := c.marshal(wrapType("ColumnFrameFusedType", fusedType)); err != nil {
			return err
		}
	}
	return nil
}

func (c *Command) translateTypeID(typedefBytes []byte, id uint32) (super.Type, error) {
	defs, ok := super.NewTypeDefsFromBytes(typedefBytes)
	if !ok {
		return nil, errors.New("malformed typedefs")
	}
	typ := super.NewTypeDefsMapper(c.sctx, defs).LookupType(id)
	if typ == nil {
		return nil, errors.New("fused type ID not in typedefs")
	}
	return typ, nil
}

func (c *Command) rowFrame(frame *bsup.RawRowFrame) error {
	if c.headers {
		if err := c.marshal(frame.Header()); err != nil {
			return err
		}
	}
	if c.typedefs {
		typedefsBytes, err := frame.Typedefs()
		if err != nil {
			return err
		}
		if err := c.marshalTypeDefs(typedefsBytes); err != nil {
			return err
		}
	}
	if c.fused {
		typedefsBytes, err := frame.Typedefs()
		if err != nil {
			return err
		}
		fusedType, err := c.translateTypeID(typedefsBytes, frame.Header().FusedType)
		if err != nil {
			return err
		}
		if err := c.marshal(wrapType("RowFrameFusedType", fusedType)); err != nil {
			return err
		}
	}
	return nil
}

func (c *Command) superFooter(frame *bsup.RawSuperFooter) error {
	if c.headers {
		if err := c.marshal(frame.Footer()); err != nil {
			return err
		}
	}
	if c.fused {
		fusedTypeBytes, err := frame.FusedTypeBytes()
		if err != nil {
			return err
		}
		fusedType, err := c.sctx.LookupByValue(fusedTypeBytes)
		if err != nil {
			return err
		}
		if err := c.marshal(wrapType("SuperFrameFusedType", fusedType)); err != nil {
			return err
		}
	}
	if c.stats {
		statsBytes, err := frame.StatsBytes()
		if err != nil {
			return err
		}
		type FrameStats struct {
			Offset uint64
		}
		offs, frameStats, err := bsup.DecodeStats(statsBytes)
		if err != nil {
			return err
		}
		for i, off := range offs {
			if err := c.marshal(FrameStats{off}); err != nil {
				return err
			}
			for _, s := range frameStats[i] {
				if err := c.marshal(s); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func (c *Command) marshalTypeDefs(bytes []byte) error {
	descs, err := DecodeTypeDefs(bytes, 0)
	if err != nil {
		return err
	}
	for _, desc := range descs {
		val, err := c.marshaler.Marshal(desc)
		if err != nil {
			return err
		}
		if err := c.emit(val); err != nil {
			return err
		}
	}
	return nil
}

// DecodeTypeDefs decodes the serialized typedefs table in bytes (see
// super.TypeDefs) into a description of each typedef, where the first
// typedef's ID is offset+super.IDTypeComplex.
func DecodeTypeDefs(bytes []byte, offset int) ([]any, error) {
	defs, ok := super.NewTypeDefsFromBytes(bytes)
	if !ok {
		return nil, errors.New("malformed typedefs")
	}
	var out []any
	for k := range defs.NTypes() {
		id := uint32(offset + k + super.IDTypeComplex)
		if _, desc := decodeTypeDef(id, defs.Serialization(uint32(k+super.IDTypeComplex))); desc != nil {
			out = append(out, desc)
		}
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
