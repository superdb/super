package loader

import (
	"fmt"
	"net/netip"
	"sync"
	"uuid"

	"github.com/ronanh/intcomp"
	"github.com/superdb/super"
	"github.com/superdb/super/bsup"
	"github.com/superdb/super/pkg/byteconv"
	"github.com/superdb/super/pkg/field"
	"github.com/superdb/super/vector"
	"github.com/superdb/super/vector/bitvec"
)

// readSegment reads the bytes of s from the frame.
// checkLen panics if the n values of a vector loaded from its segments don't
// match its length, which is decoded from its locators.
func checkLen(what string, n int, length uint32) {
	if n != int(length) {
		panic(fmt.Sprintf("BSUP loader: %s vector of length %d has %d values", what, length, n))
	}
}

func readSegment(loader *FrameLoader, s bsup.Segment) []byte {
	bytes := make([]byte, s.MemLength)
	if err := s.Read(loader.frame, bytes); err != nil {
		panic(err)
	}
	return bytes
}

// Int is a vector of signed integers (including durations and times), whose
// locators are its kind, count, and the segment of its values.
type Int struct {
	Node
	Values bsup.Segment
	mu     sync.Mutex
	typ    super.Type
	vals   []int64
}

func (i *Int) project(loader *FrameLoader, _ field.Projection) vector.Any {
	return vector.NewInt(i.typ, i.load(loader))
}

func (i *Int) load(loader *FrameLoader) []int64 {
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.vals == nil {
		bytes := readSegment(loader, i.Values)
		i.vals = intcomp.UncompressInt64(byteconv.ReinterpretSlice[uint64](bytes), nil)
		checkLen("int", len(i.vals), i.Length)
	}
	return i.vals
}

// Uint is a vector of unsigned integers, whose locators are its kind, count,
// and the segment of its values.
type Uint struct {
	Node
	Values bsup.Segment
	mu     sync.Mutex
	typ    super.Type
	vals   []uint64
}

func (u *Uint) project(loader *FrameLoader, _ field.Projection) vector.Any {
	return vector.NewUint(u.typ, u.load(loader))
}

func (u *Uint) load(loader *FrameLoader) []uint64 {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.vals == nil {
		bytes := readSegment(loader, u.Values)
		u.vals = intcomp.UncompressUint64(byteconv.ReinterpretSlice[uint64](bytes), nil)
		checkLen("uint", len(u.vals), u.Length)
	}
	return u.vals
}

// Float is a vector of floats, whose locators are its kind, count, and the
// segment of its values.
type Float struct {
	Node
	Values bsup.Segment
	mu     sync.Mutex
	typ    super.Type
	vals   []float64
}

func (f *Float) project(loader *FrameLoader, _ field.Projection) vector.Any {
	return vector.NewFloat(f.typ, f.load(loader))
}

func (f *Float) load(loader *FrameLoader) []float64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.vals == nil {
		f.vals = byteconv.ReinterpretSlice[float64](readSegment(loader, f.Values))
		checkLen("float", len(f.vals), f.Length)
	}
	return f.vals
}

// Bool is a vector of bools, whose locators are its kind, count, and the
// segment of its bits.
type Bool struct {
	Node
	Values bsup.Segment
	mu     sync.Mutex
	bits   *bitvec.Bits
}

func (b *Bool) project(loader *FrameLoader, _ field.Projection) vector.Any {
	return vector.NewBool(b.load(loader))
}

func (b *Bool) load(loader *FrameLoader) bitvec.Bits {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.bits == nil {
		bytes := readSegment(loader, b.Values)
		if uint64(len(bytes))*8 < uint64(b.Length) {
			panic(fmt.Sprintf("BSUP loader: bool vector of length %d has %d bits", b.Length, len(bytes)*8))
		}
		bits := bitvec.New(byteconv.ReinterpretSlice[uint64](bytes), b.Length)
		b.bits = &bits
	}
	return *b.bits
}

// Bytes is a vector of string, bytes, uuid, or all values, whose locators are
// its kind, count, and the segments of its bytes table (its bytes and their
// offsets).
type Bytes struct {
	Node
	Bytes   bsup.Segment
	Offsets bsup.Segment
	mu      sync.Mutex
	table   *vector.BytesTable
}

func (b *Bytes) project(loader *FrameLoader, _ field.Projection) vector.Any {
	table := b.load(loader)
	switch b.TypeID {
	case super.IDString:
		return vector.NewString(table)
	case super.IDBytes, super.IDAll:
		// The values of a fusion(all) vector are bytes.
		return vector.NewBytes(table)
	case super.IDUUID:
		vals := make([]uuid.UUID, table.Len())
		for k := range vals {
			vals[k] = super.DecodeUUID(table.Bytes(uint32(k)))
		}
		return vector.NewUUID(vals)
	default:
		panic(b.TypeID)
	}
}

func (b *Bytes) load(loader *FrameLoader) vector.BytesTable {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.table == nil {
		table := loadBytesTable(loader, b.Offsets, b.Bytes, b.Length)
		b.table = &table
	}
	return *b.table
}

// loadBytesTable loads the bytes table of a vector of length n.
func loadBytesTable(loader *FrameLoader, offsets, bytes bsup.Segment, n uint32) vector.BytesTable {
	offs, err := bsup.ReadUint32s(offsets, loader.frame)
	if err != nil {
		panic(err)
	}
	if len(offs) == 0 {
		offs = []uint32{0}
	}
	checkLen("bytes", len(offs)-1, n)
	b := readSegment(loader, bytes)
	if int(offs[len(offs)-1]) > len(b) {
		panic(fmt.Sprintf("BSUP loader: bytes table offset %d is past the end of its %d bytes", offs[len(offs)-1], len(b)))
	}
	return vector.NewBytesTable(offs, b)
}

func (b *Bytes) strings(c *collector) {
	if b.TypeID != super.IDString {
		return
	}
	table := b.load(c.loader)
	c.append(table.RawBytes())
}

// IP is a vector of IP addresses, whose locators are those of a Bytes.
type IP struct {
	Node
	Bytes   bsup.Segment
	Offsets bsup.Segment
	mu      sync.Mutex
	vals    []netip.Addr
}

func (i *IP) project(loader *FrameLoader, _ field.Projection) vector.Any {
	return vector.NewIP(i.load(loader))
}

func (i *IP) load(loader *FrameLoader) []netip.Addr {
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.vals != nil {
		return i.vals
	}
	table := loadBytesTable(loader, i.Offsets, i.Bytes, i.Length)
	i.vals = make([]netip.Addr, i.Length)
	for k := range table.Len() {
		var ok bool
		if i.vals[k], ok = netip.AddrFromSlice(table.Bytes(k)); !ok {
			panic("malformed ip block")
		}
	}
	return i.vals
}

// Net is a vector of networks, whose locators are those of a Bytes.
type Net struct {
	Node
	Bytes   bsup.Segment
	Offsets bsup.Segment
	mu      sync.Mutex
	vals    []netip.Prefix
}

func (n *Net) project(loader *FrameLoader, _ field.Projection) vector.Any {
	return vector.NewNet(n.load(loader))
}

func (n *Net) load(loader *FrameLoader) []netip.Prefix {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.vals != nil {
		return n.vals
	}
	table := loadBytesTable(loader, n.Offsets, n.Bytes, n.Length)
	n.vals = make([]netip.Prefix, n.Length)
	for k := range table.Len() {
		if err := n.vals[k].UnmarshalBinary(table.Bytes(k)); err != nil {
			panic(err)
		}
	}
	return n.vals
}

// Null is a vector of nulls, whose locators are its kind and count.
type Null struct {
	Node
}

func (n *Null) project(*FrameLoader, field.Projection) vector.Any {
	return vector.NewNull(n.Length)
}

// None is a vector of none values, whose locators are its kind and count.  It
// may also be the values of an option whose values are all none, whose kind
// is bsup.KindNone.
type None struct {
	Node
}

func (n *None) project(*FrameLoader, field.Projection) vector.Any {
	return vector.NewNone(n.Length)
}

// Missing is a vector of missing values, whose locators are its kind and
// count.
type Missing struct {
	Node
}

func (m *Missing) project(*FrameLoader, field.Projection) vector.Any {
	return vector.NewMissing(m.Length)
}

// TypeValue is a vector of type values, whose IDs are in the frame's typedefs
// table (as opposed to its vector typedefs table) and whose locators are its
// kind, count, and the segment of its IDs.  It is also the subtypes of a
// fusion vector, whose locator is just the segment of the IDs.
type TypeValue struct {
	Node
	Values bsup.Segment
	mu     sync.Mutex
	// We load the IDs separately from the types so that a shadow
	// of ids may be shared by multipe contexts.
	ids []uint32
}

func (t *TypeValue) project(loader *FrameLoader, _ field.Projection) vector.Any {
	return vector.NewTypeValueWithLoader(loader.sctx, t.newLoader(loader))
}

func (t *TypeValue) newLoader(loader *FrameLoader) *typesLoader {
	return &typesLoader{loader, t}
}

func (t *TypeValue) loadIDs(loader *FrameLoader) []uint32 {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.ids == nil {
		ids, err := bsup.ReadUint32s(t.Values, loader.frame)
		if err != nil {
			panic(err)
		}
		checkLen("type value", len(ids), t.Length)
		t.ids = ids
	}
	return t.ids
}

type typesLoader struct {
	loader *FrameLoader
	type_  *TypeValue
}

var _ vector.TypesLoader = (*typesLoader)(nil)

func (s *typesLoader) Load() (*super.TypeIDMap, []uint32) {
	return s.loader.typeIDMap(), s.type_.loadIDs(s.loader)
}
