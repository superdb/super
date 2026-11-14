package loader

import (
	"fmt"
	"slices"

	"github.com/superdb/super"
	"github.com/superdb/super/bsup"
	"github.com/superdb/super/pkg/field"
	"github.com/superdb/super/vector"
)

// A Shadow mirrors a vector.Any implementation with locks and pointers to
// shared vector slices.  This lets us page in just the portions of vector data
// that are needed at any given time (which we cache inside the shadow).  When
// we need a runtime vector, we build the immutable vector.Any components from
// mutable shadow pieces that are dynamically loaded and maintained here.
//
// The shadows of a frame are created by unmarshaling its locators (see
// Unmarshal), which traverses the type of each vector in DFS order decoding
// the vector's locators (see bsup.LocatorWriter) and its typedef into its
// shadow.  Every vector has a shadow, so the locators and typedefs are
// decoded just once and are cached in the shadows, which don't change once
// they're unmarshaled.  A projection then decides only which data is loaded:
// the project method of a shadow loads the data of the vectors in the
// projection (concurrently and just once for each vector) and returns a
// vector.Any.
//
// Note that a shadow doesn't know about the query type context, thereby
// allowing the shadow to be shared across different queries.  Instead, the
// loader that builds a vector.Any is responsible for computing the type in
// its context from the shadow hierarchy.
type Shadow interface {
	// Len returns the length of the vector.
	Len() uint32
	project(*FrameLoader, field.Projection) vector.Any
	strings(*collector)
}

// typesOnly is the projection for a value whose type is needed but not its data.
var typesOnly = field.Projection{{Types: true}}

// hasFusion returns true if typ has a fusion type that superdefuse would
// defuse (see the FusionChecker in runtime/sam/expr/function).
func hasFusion(typ super.Type) bool {
	switch typ := typ.(type) {
	case *super.TypeRecord:
		return slices.ContainsFunc(typ.Fields, func(f super.Field) bool { return hasFusion(f.Type) })
	case *super.TypeArray:
		return hasFusion(typ.Type)
	case *super.TypeSet:
		return hasFusion(typ.Type)
	case *super.TypeUnion:
		return slices.ContainsFunc(typ.Types, hasFusion)
	case *super.TypeMap:
		return hasFusion(typ.KeyType) || hasFusion(typ.ValType)
	case *super.TypeError:
		return hasFusion(typ.Type)
	case *super.TypeFusion:
		return true
	}
	return false
}

// Node holds what every shadow has: the type ID of the vector in the frame's
// vector typedefs table and the vector's length.
type Node struct {
	TypeID uint32
	Length uint32
}

func (n *Node) Len() uint32 {
	return n.Length
}

// Unmarshal returns the root shadow of a frame whose vector typedefs table is
// defs, whose locator section is locators, and whose data section is dataSize
// bytes, or an error if the typedefs or locators are malformed.  The root
// shadow is a *Dynamic if the frame has more than one root vector.
func Unmarshal(defs *super.TypeDefs, locators []byte, dataSize uint64) (Shadow, error) {
	return unmarshal(defs, bsup.NewLocatorReader(locators, dataSize))
}

// unmarshal decodes the locator section read by r into the root shadow or
// returns an error if the section or defs is malformed.
func unmarshal(defs *super.TypeDefs, r *bsup.LocatorReader) (shadow Shadow, err error) {
	defer func() {
		if r := recover(); r != nil {
			shadow, err = nil, fmt.Errorf("malformed BSUP locators: %v", r)
		}
	}()
	return mustUnmarshal(defs, r), nil
}

// mustUnmarshal is like unmarshal but panics if the section or defs is
// malformed.
func mustUnmarshal(defs *super.TypeDefs, r *bsup.LocatorReader) Shadow {
	ids, tags := bsup.ReadLocatorRoots(r)
	d := &decoder{defs: defs, r: r}
	var root Shadow
	if len(ids) == 1 {
		root = d.vector(ids[0], inVector)
	} else {
		dynamic := &Dynamic{TypeIDs: ids, Tags: tags, Values: make([]Shadow, len(ids))}
		for k, id := range ids {
			dynamic.Values[k] = d.vector(id, inVector)
			n := dynamic.Values[k].Len()
			if dynamic.Length+n < n {
				panic("BSUP loader: dynamic vector too long")
			}
			dynamic.Length += n
		}
		root = dynamic
	}
	if r.Len() != 0 {
		panic("BSUP loader: extra bytes in locator section")
	}
	if !r.AtLimit() {
		panic("BSUP loader: segments don't fill the data section")
	}
	return root
}

// decoder decodes the locators and typedefs of a frame's vectors into shadows.
type decoder struct {
	defs  *super.TypeDefs
	r     *bsup.LocatorReader
	depth int
}

// The places a vector may be, which restrict its kind: a none vector holds
// only the values of an option and the values of a dict are plain.
const (
	inVector = iota
	inOption
	inDict
)

// vector decodes the locators of a vector of type id in the place in and
// returns its shadow.
func (d *decoder) vector(id uint32, in int) Shadow {
	d.depth++
	if d.depth > bsup.MaxLocatorDepth {
		panic("BSUP loader: vectors nested too deeply")
	}
	defer func() { d.depth-- }()
	kind := d.r.ReadKind()
	if (kind == bsup.KindNone && in != inOption) || (kind != bsup.KindPlain && in == inDict) {
		panic(fmt.Sprintf("BSUP loader: vector kind %d not allowed here", kind))
	}
	switch kind {
	case bsup.KindConst:
		n := d.r.ReadCount()
		return &Const{Node: Node{id, n}, Value: d.r.Value()}
	case bsup.KindNone:
		return &None{Node{id, d.r.ReadCount()}}
	case bsup.KindDict:
		if id >= super.IDTypeComplex {
			panic(fmt.Sprintf("BSUP loader: dict of complex type ID %d", id))
		}
		values := d.vector(id, inDict)
		counts := d.r.Segment()
		index := d.r.Segment()
		return &Dict{Node: Node{id, uint32(index.MemLength)}, Values: values, Counts: counts, Index: index}
	case bsup.KindPlain:
		if id < super.IDTypeComplex {
			return d.primitive(id)
		}
		return d.complex(id)
	default:
		panic(fmt.Sprintf("BSUP loader: unknown vector kind %d", kind))
	}
}

// primitive decodes the locators of a plain vector of the primitive type id.
func (d *decoder) primitive(id uint32) Shadow {
	typ, err := super.LookupPrimitiveByID(int(id))
	if err != nil {
		panic(err)
	}
	node := Node{id, d.r.ReadCount()}
	switch id {
	case super.IDUint8, super.IDUint16, super.IDUint32, super.IDUint64:
		return &Uint{Node: node, Values: d.r.Segment(), typ: typ}
	case super.IDInt8, super.IDInt16, super.IDInt32, super.IDInt64, super.IDDuration, super.IDTimestamp:
		return &Int{Node: node, Values: d.r.Segment(), typ: typ}
	case super.IDFloat16, super.IDFloat32, super.IDFloat64:
		return &Float{Node: node, Values: d.r.Segment(), typ: typ}
	case super.IDBool:
		return &Bool{Node: node, Values: d.r.Segment()}
	case super.IDType:
		return &TypeValue{Node: node, Values: d.r.Segment()}
	case super.IDBytes, super.IDString, super.IDUUID, super.IDAll:
		return &Bytes{Node: node, Bytes: d.r.Segment(), Offsets: d.r.Segment()}
	case super.IDIP:
		return &IP{Node: node, Bytes: d.r.Segment(), Offsets: d.r.Segment()}
	case super.IDNet:
		return &Net{Node: node, Bytes: d.r.Segment(), Offsets: d.r.Segment()}
	case super.IDNull:
		return &Null{node}
	case super.IDNone:
		return &None{node}
	case super.IDMissing:
		return &Missing{node}
	default:
		panic(fmt.Sprintf("BSUP loader: type %s has no vector encoding", typ))
	}
}

// complex decodes the typedef and the locators of a plain vector of the
// complex type id.
func (d *decoder) complex(id uint32) Shadow {
	code, td := bsup.ReadTypeDef(d.defs, id)
	switch code {
	case super.TypeDefRecord:
		n := td.Length(super.MaxRecordFields)
		r := &Record{Node: Node{TypeID: id}, Names: make([]string, n), Fields: make([]Shadow, n)}
		if n == 0 {
			r.Length = d.r.ReadCount()
			return r
		}
		for k := range n {
			r.Names[k] = string(td.Name())
			r.Fields[k] = d.vector(td.Child(), inVector)
			if k == 0 {
				r.Length = r.Fields[0].Len()
			} else if r.Fields[k].Len() != r.Length {
				panic(fmt.Sprintf("BSUP loader: record field %q has length %d but the record has length %d", r.Names[k], r.Fields[k].Len(), r.Length))
			}
		}
		return r
	case super.TypeDefArray, super.TypeDefSet:
		inner := td.Child()
		node := Node{id, d.r.ReadCount()}
		offsets := d.r.Segment()
		values := d.vector(inner, inVector)
		if code == super.TypeDefSet {
			return &Set{Array{Node: node, Offsets: offsets, Values: values}}
		}
		return &Array{Node: node, Offsets: offsets, Values: values}
	case super.TypeDefMap:
		keyID, valID := td.Child(), td.Child()
		m := &Map{Node: Node{id, d.r.ReadCount()}, Offsets: d.r.Segment()}
		m.Keys = d.vector(keyID, inVector)
		m.Values = d.vector(valID, inVector)
		if m.Keys.Len() != m.Values.Len() {
			panic(fmt.Sprintf("BSUP loader: map has %d keys but %d values", m.Keys.Len(), m.Values.Len()))
		}
		return m
	case super.TypeDefUnion:
		n := td.Length(super.MaxUnionTypes)
		u := &Union{Node: Node{id, d.r.ReadCount()}, Tags: d.r.Segment(), Values: make([]Shadow, n)}
		for k := range n {
			u.Values[k] = d.vector(td.Child(), inVector)
		}
		return u
	case super.TypeDefEnum:
		n := td.Length(super.MaxEnumSymbols)
		symbols := make([]string, n)
		for k := range n {
			symbols[k] = string(td.Name())
		}
		node := Node{id, d.r.ReadCount()}
		values := &Uint{Node: Node{super.IDUint64, node.Length}, Values: d.r.Segment(), typ: super.TypeUint64}
		return &Enum{Node: node, Symbols: symbols, Values: values}
	case super.TypeDefError:
		values := d.vector(td.Child(), inVector)
		return &Error{Node: Node{id, values.Len()}, Values: values}
	case super.TypeDefNamed:
		name := string(td.Name())
		values := d.vector(td.NamedInner(), inVector)
		return &Named{Node: Node{id, values.Len()}, Name: name, Values: values}
	case super.TypeDefFusion:
		inner := td.Child()
		values := d.vector(inner, inVector)
		node := Node{id, values.Len()}
		subtypes := &TypeValue{Node: Node{super.IDType, node.Length}, Values: d.r.Segment()}
		if inner == super.IDAll {
			return &FusionAll{Fusion{Node: node, Values: values, Subtypes: subtypes}}
		}
		return &Fusion{Node: node, Values: values, Subtypes: subtypes}
	case super.TypeDefOption:
		inner := td.Child()
		o := &Option{Node: Node{id, d.r.ReadCount()}, Tags: d.r.Segment()}
		o.Values = d.vector(inner, inOption)
		return o
	default:
		panic(fmt.Sprintf("BSUP loader: type ID %d has no vector encoding", id))
	}
}
