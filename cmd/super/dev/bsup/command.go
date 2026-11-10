package bsup

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/superdb/super"
	"github.com/superdb/super/bsup"
	"github.com/superdb/super/bsup/loader"
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
	locators    bool
	vtypedefs   bool
	typedefs    bool
	locbytes    bool
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
	f.BoolVar(&c.locators, "locators", false, "emit a record for each vector in a column frame's locators section")
	f.BoolVar(&c.locbytes, "locatorbytes", false, "dump the raw bytes of column frame locators sections to stdout")
	f.BoolVar(&c.vtypedefs, "vectortypedefs", false, "emit column frame vector typedefs tables")
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
	if c.locbytes {
		return c.dumpLocatorBytes(ctx, reader)
	}
	c.sctx = super.NewContext()
	writer, err := c.outputFlags.Open(ctx, c.sctx, engine)
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

func (c *Command) dumpLocatorBytes(ctx context.Context, reader *bsup.RawReader) error {
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
			locBytes, err := frame.Locators()
			if err != nil {
				return err
			}
			if _, err = os.Stdout.Write(locBytes); err != nil {
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
	if c.vtypedefs {
		typedefsBytes, err := frame.VectorTypedefs()
		if err != nil {
			return err
		}
		if err := c.marshalTypeDefs(typedefsBytes); err != nil {
			return err
		}
	}
	if c.locators {
		if err := c.marshalLocators(frame); err != nil {
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
		if err := c.marshal(wrapType("ColumnFrameFusedType", fusedType)); err != nil {
			return err
		}
	}
	return nil
}

// Locator descriptions emitted for -locators, which are named by the encoding
// of the vector (e.g., Int, Record, Dict, Const).
type (
	locatorNode struct {
		Path   string
		Type   super.Type
		Length uint32
	}
	locatorSegments struct {
		Path     string
		Type     super.Type
		Length   uint32
		Segments []bsup.Segment
	}
	locatorConst struct {
		Path   string
		Type   super.Type
		Length uint32
		Bytes  []byte
	}
	locatorDynamic struct {
		Length uint32
		Tags   bsup.Segment
	}
)

// marshalLocators emits a record for each vector in the frame's locator
// section in DFS order, which it unmarshals into the frame's shadows.
func (c *Command) marshalLocators(frame *bsup.RawColFrame) error {
	typedefsBytes, err := frame.VectorTypedefs()
	if err != nil {
		return err
	}
	locBytes, err := frame.Locators()
	if err != nil {
		return err
	}
	defs, ok := super.NewTypeDefsFromBytes(typedefsBytes)
	if !ok {
		return errors.New("malformed vector typedefs")
	}
	_, dataSize := frame.Header().SegmentsSection()
	root, err := loader.Unmarshal(defs, locBytes, uint64(dataSize))
	if err != nil {
		return err
	}
	m := &shadowMarshaler{c: c, mapper: super.NewTypeDefsMapper(c.sctx, defs)}
	if dynamic, ok := root.(*loader.Dynamic); ok {
		if err := c.marshalNamed("Dynamic", &locatorDynamic{dynamic.Length, dynamic.Tags}); err != nil {
			return err
		}
		for k, value := range dynamic.Values {
			if err := m.marshal([]string{strconv.Itoa(k)}, value); err != nil {
				return err
			}
		}
		return nil
	}
	return m.marshal(nil, root)
}

type shadowMarshaler struct {
	c      *Command
	mapper *super.TypeDefsMapper
}

// marshal emits a record for shadow, whose path is path, and then for each
// of the vectors in shadow.
func (m *shadowMarshaler) marshal(path []string, shadow loader.Shadow) error {
	var node *loader.Node
	var name string
	var segs []bsup.Segment
	type child struct {
		label  string
		shadow loader.Shadow
	}
	var children []child
	switch s := shadow.(type) {
	case *loader.Const:
		typ, err := m.lookup(s.TypeID)
		if err != nil {
			return err
		}
		return m.c.marshalNamed("Const", &locatorConst{strings.Join(path, "."), typ, s.Length, s.Value})
	case *loader.Dict:
		node, name, segs = &s.Node, "Dict", []bsup.Segment{s.Counts, s.Index}
		children = []child{{"values", s.Values}}
	case *loader.Record:
		node, name = &s.Node, "Record"
		for k, field := range s.Fields {
			children = append(children, child{s.Names[k], field})
		}
	case *loader.Set:
		node, name, segs = &s.Node, "Set", []bsup.Segment{s.Offsets}
		children = []child{{"values", s.Values}}
	case *loader.Array:
		node, name, segs = &s.Node, "Array", []bsup.Segment{s.Offsets}
		children = []child{{"values", s.Values}}
	case *loader.Map:
		node, name, segs = &s.Node, "Map", []bsup.Segment{s.Offsets}
		children = []child{{"keys", s.Keys}, {"values", s.Values}}
	case *loader.Union:
		node, name, segs = &s.Node, "Union", []bsup.Segment{s.Tags}
		for k, value := range s.Values {
			children = append(children, child{strconv.Itoa(k), value})
		}
	case *loader.Enum:
		node, name, segs = &s.Node, "Enum", []bsup.Segment{s.Values.Values}
	case *loader.Error:
		node, name = &s.Node, "Error"
		children = []child{{"", s.Values}}
	case *loader.Named:
		node, name = &s.Node, "Named"
		children = []child{{"", s.Values}}
	case *loader.FusionAll:
		node, name, segs = &s.Node, "Fusion", []bsup.Segment{s.Subtypes.Values}
		children = []child{{"values", s.Values}}
	case *loader.Fusion:
		node, name, segs = &s.Node, "Fusion", []bsup.Segment{s.Subtypes.Values}
		children = []child{{"values", s.Values}}
	case *loader.Option:
		node, name, segs = &s.Node, "Option", []bsup.Segment{s.Tags}
		children = []child{{"values", s.Values}}
	case *loader.Int:
		node, name, segs = &s.Node, "Int", []bsup.Segment{s.Values}
	case *loader.Uint:
		node, name, segs = &s.Node, "Uint", []bsup.Segment{s.Values}
	case *loader.Float:
		node, name, segs = &s.Node, "Float", []bsup.Segment{s.Values}
	case *loader.Bool:
		node, name, segs = &s.Node, "Bool", []bsup.Segment{s.Values}
	case *loader.TypeValue:
		node, name, segs = &s.Node, "TypeValue", []bsup.Segment{s.Values}
	case *loader.Bytes:
		node, name, segs = &s.Node, "Bytes", []bsup.Segment{s.Bytes, s.Offsets}
	case *loader.IP:
		node, name, segs = &s.Node, "IP", []bsup.Segment{s.Bytes, s.Offsets}
	case *loader.Net:
		node, name, segs = &s.Node, "Net", []bsup.Segment{s.Bytes, s.Offsets}
	case *loader.Null:
		node, name = &s.Node, "Null"
	case *loader.None:
		node, name = &s.Node, "None"
	default:
		return fmt.Errorf("unknown shadow %T", shadow)
	}
	typ, err := m.lookup(node.TypeID)
	if err != nil {
		return err
	}
	var desc any
	if len(segs) != 0 {
		desc = &locatorSegments{strings.Join(path, "."), typ, node.Length, segs}
	} else {
		desc = &locatorNode{strings.Join(path, "."), typ, node.Length}
	}
	if err := m.c.marshalNamed(name, desc); err != nil {
		return err
	}
	for _, child := range children {
		childPath := path
		if child.label != "" {
			childPath = append(slices.Clone(path), child.label)
		}
		if err := m.marshal(childPath, child.shadow); err != nil {
			return err
		}
	}
	return nil
}

func (m *shadowMarshaler) lookup(id uint32) (super.Type, error) {
	typ := m.mapper.LookupType(id)
	if typ == nil {
		return nil, fmt.Errorf("bad type ID %d in vector typedefs", id)
	}
	return typ, nil
}

// marshalNamed emits the marshaled desc with its type named name.
func (c *Command) marshalNamed(name string, desc any) error {
	val, err := c.marshaler.Marshal(desc)
	if err != nil {
		return err
	}
	typ, err := c.sctx.LookupTypeNamed(name, super.TypeUnder(val.Type()))
	if err != nil {
		return err
	}
	return c.emit(super.NewValue(typ, val.Bytes()))
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
	case super.TypeDefOption:
		bytes, out = wrapped(bytes, "TypeDefOption", slot)
	case super.TypeDefDecimal:
		var precision, scale int
		precision, bytes = super.DecodeLength(bytes)
		if bytes == nil {
			return nil, errInfo(slot, "TypeDefDecimal", "at precision field")
		}
		scale, bytes = super.DecodeLength(bytes)
		if bytes == nil {
			return nil, errInfo(slot, "TypeDefDecimal", "at scale field")
		}
		out = &struct {
			Kind      string
			Slot      uint32
			Precision int
			Scale     int
		}{
			Kind:      "TypeDefDecimal",
			Slot:      slot,
			Precision: precision,
			Scale:     scale,
		}
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
