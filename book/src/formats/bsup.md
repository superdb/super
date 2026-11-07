# Super Binary (BSUP) Format

## 1. Introduction

Super Binary (BSUP) is an efficient binary serialization format for
[super-structured data](model.md).  It holds data in either of two
orientations, and a single BSUP file may mix them:

* _row frames_ encode a sequence of values one after another, a bit like
  [Apache Avro](https://avro.apache.org), and
* _column frames_ encode a sequence of values as vectors, a bit like
  [Apache Parquet](https://parquet.apache.org/) or
  [Apache Arrow](https://arrow.apache.org/docs/format/Columnar.html).

Unlike Avro and Parquet, BSUP has no external schema.  Each frame carries
the definitions of the types of its values in a compact _typedefs table_,
so values of many different types may be freely intermixed in a single
sequence and each frame can be decoded on its own.

A BSUP file is organized into _super frames_, each a sequence of frames
followed by a _super footer_ that summarizes the super frame.  The super
footer holds the _fused type_ of the super frame's values (a single type to
which all of its values conform) and, for column frames,
[zone maps](#86-zone-maps) of the minimum and maximum value of each
column of each frame.  This allows a reader to determine the type of an
entire file and to skip frames that can't match a query without reading
the frames themselves.

Since each frame is self-contained and each super frame ends with a footer
that records its size, BSUP files may simply be concatenated to form a
larger, valid BSUP file, which can be traversed either forward from its
start or backward from its end.

This specification describes BSUP version 0.31.

## 2. Conventions

All fixed-size integers in headers, footers, and data are encoded in
little-endian byte order unless stated otherwise.

A `uvarint` is an unsigned, variable-length integer as defined by
[Protocol Buffers](https://protobuf.dev/programming-guides/encoding/#varints):
a sequence of bytes with bit 7 set in each byte except the last, whose value
is the base-128 number composed of the lower 7 bits of each byte from
least-significant digit (byte 0) to most-significant digit.

A _counted string_ is a `uvarint` length followed by that many bytes of
UTF-8 text.

The byte layouts below show fixed-size fields with their size in bytes
(e.g., `<frame-size:8>`) and variable-size fields without one.

## 3. File Structure

A BSUP file is a sequence of super frames.  A super frame is a sequence of
one or more frames followed by a super footer:
```
| frame | frame | ... | super footer | frame | frame | ... | super footer | ...
```
Each frame and each super footer begins with a 4-byte magic string that
identifies it:

| Magic  | Section                                        |
|--------|------------------------------------------------|
| `SUPR` | [row frame](#7-row-frames)                     |
| `SUPX` | [control frame](#74-control-frames)            |
| `SUPC` | [column frame](#8-column-frames)               |
| `SUPS` | [super footer](#9-super-footer)                |

The frames of a super frame are either all row frames or all column frames,
except that control frames may appear in either.  Row super frames and
column super frames may be intermixed in a file.

The magic is followed by a 2-byte version number, which encodes a
major version in its upper 6 bits and a minor version in its lower 10 bits.
Implementations may read data with the same major version as their own,
though an implementation might not understand types added by a later minor
version.  Major version 0 is reserved for development and there is no
compatibility between its minor versions.  The current version is 0.31.

Each frame header and super footer holds its own size, so a reader can
traverse a file forward from its start, skipping over any frame
it doesn't need.  Each super footer also ends with a copy of its size and
holds the size of its super frame, so a reader can also traverse a
file backward from its end, e.g., to read the fused types and zone maps
of a file's super frames without reading any frames.

An implementation may bound the sizes of these sections.  The reference
implementation limits a frame to 2 GiB, a super frame to 512 GiB, and the
typedefs, locator, and zone map sections and the super footer to 100 MiB.

## 4. Typedefs Tables

A typedefs table defines a set of complex types and assigns each a
_type ID_.  Each frame has its own typedefs tables (a row frame has one and a
column frame two) and a type ID is meaningful only with respect to its table.

The primitive types have predefined type IDs less than 48 as listed in the
[Primitive Types](#5-primitive-types) table.  The typedefs in a table are
numbered in order starting at 48, so the type ID of a typedef is implied by
its position and isn't encoded.

A typedefs table is encoded as the number of typedefs, the offset of each
typedef, and the typedefs:
```
|<ntypes>|<offset-1><offset-2>...|<typedef-1><typedef-2>...|
```
where `<ntypes>` and each offset are `uvarint`s and each offset is relative
to the start of the first typedef, so the first offset is always 0.  The
offsets allow a typedef to be found by its type ID without decoding the
typedefs that precede it.  An empty table is encoded as zero bytes.

Each typedef is a 1-byte typedef code followed by its type-specific encoding:

| Code | Complex Type |
|------|--------------|
|   0  |  record      |
|   1  |  array       |
|   2  |  set         |
|   3  |  map         |
|   4  |  union       |
|   5  |  enum        |
|   6  |  error       |
|   7  |  named type  |
|   8  |  fusion      |
|   9  |  option      |
|  10  |  decimal     |

Within a typedef, a reference to another type is that type's ID encoded as a
`uvarint`, except for the inner type of a named type (see below).

A typedef other than a named type may refer only to types with smaller
type IDs, i.e., to primitive types and to typedefs that precede it.
A named type may refer to any type, which allows recursive
types, but a cycle of references must include a type that is not named,
i.e., a named type may not refer to itself directly or through other
named types alone.  A table that breaks these rules is malformed.

An encoder writes at most one typedef for any type in a table.

### 4.1 Record Typedef

```
|0x00|<nfields>|<name-1><type-id-1><name-2><type-id-2>...|
```
A record type is an ordered list of fields, each a name and a type.
`<nfields>` is a `uvarint`, each name is a counted string, and the
names of a record's fields must be unique.  Unlike JSON, the order of the
fields is significant.

A field that may be absent from a value has an [option](#410-option-typedef)
type.

### 4.2 Array Typedef

```
|0x01|<type-id>|
```
where `<type-id>` is the type of the array's elements.

### 4.3 Set Typedef

```
|0x02|<type-id>|
```
where `<type-id>` is the type of the set's elements.

### 4.4 Map Typedef

```
|0x03|<key-type-id>|<value-type-id>|
```

### 4.5 Union Typedef

```
|0x04|<ntypes>|<type-id-1><type-id-2>...|
```
where `<ntypes>` is a `uvarint` and the type IDs are those of the union's
member types, which must be distinct, may not themselves be unions, and are
sorted in the canonical order of types.  The position of a member type
in this list is its _tag_.

### 4.6 Enum Typedef

```
|0x05|<nsymbols>|<symbol-1><symbol-2>...|
```
where `<nsymbols>` is a `uvarint` and each symbol is a counted string.

### 4.7 Error Typedef

```
|0x06|<type-id>|
```
where `<type-id>` is the type of the error's value.

### 4.8 Named Type Typedef

```
|0x07|<name>|<type-id:4>|
```
where `<name>` is a counted string and `<type-id:4>` is the ID of the named
type's underlying type encoded as a 4-byte, _big-endian_ integer.
(The fixed size allows an encoder to define a recursive type by writing
the named typedef before its underlying type is known and filling in its
type ID later.)

A name may be bound to only one type in a typedefs table.  It is an error
to name a type with the name of a primitive type.

### 4.9 Fusion Typedef

```
|0x08|<type-id>|
```
where `<type-id>` is the fusion's supertype.

### 4.10 Option Typedef

```
|0x09|<type-id>|
```
where `<type-id>` is the type of an option's value when it has one.  The
underlying type may not be an option, `missing`, or `none`.

### 4.11 Decimal Typedef

```
|0x0a|<precision>|<scale>|
```
where `<precision>` and `<scale>` are `uvarint`s.  Decimal values are
reserved and not yet defined.

## 5. Primitive Types

The primitive types have the following type IDs.  The table also describes the
_body_ of a value of each type (see [Values](#73-values)).

| Type               | ID | Body                                                       |
|--------------------|---:|------------------------------------------------------------|
| `uint8`            |  0 | unsigned integer as a variable-length integer (see below)  |
| `uint16`           |  1 | "                                                          |
| `uint32`           |  2 | "                                                          |
| `uint64`           |  3 | "                                                          |
| `uint128`          |  4 | reserved                                                   |
| `int8`             |  5 | signed integer as a variable-length integer (see below)    |
| `int16`            |  6 | "                                                          |
| `int32`            |  7 | "                                                          |
| `int64`            |  8 | "                                                          |
| `int128`           |  9 | reserved                                                   |
| `duration`         | 10 | signed integer count of nanoseconds                        |
| `timestamp(0)`     | 11 | reserved                                                   |
| `timestamp(3)`     | 12 | reserved                                                   |
| `timestamp(6)`     | 13 | reserved                                                   |
| `timestamp`        | 14 | signed integer count of nanoseconds since the Unix epoch   |
| `timestamp_ntz(0)` | 15 | reserved                                                   |
| `timestamp_ntz(3)` | 16 | reserved                                                   |
| `timestamp_ntz(6)` | 17 | reserved                                                   |
| `timestamp_ntz`    | 18 | reserved                                                   |
| `date`             | 19 | reserved                                                   |
| `time(0)`          | 20 | reserved                                                   |
| `time(3)`          | 21 | reserved                                                   |
| `time(6)`          | 22 | reserved                                                   |
| `time`             | 23 | reserved                                                   |
| `float16`          | 24 | 2 bytes of IEEE 754 binary16                               |
| `float32`          | 25 | 4 bytes of IEEE 754 binary32                               |
| `float64`          | 26 | 8 bytes of IEEE 754 binary64                               |
| `bool`             | 27 | 1 byte, 0 (false) or 1 (true)                              |
| `bytes`            | 28 | the bytes                                                  |
| `string`           | 29 | UTF-8 text                                                 |
| `ip`               | 30 | 4 or 16 bytes of IPv4 or IPv6 address in network byte order |
| `net`              | 31 | address followed by mask, each 4 or 16 bytes as for `ip`   |
| `interval`         | 32 | reserved                                                   |
| `uuid`             | 33 | 16 bytes                                                   |
| `geometry`         | 34 | reserved                                                   |
| `geography`        | 35 | reserved                                                   |
| `type`             | 36 | [type value](#6-type-values)                              |
| `null`             | 37 | empty                                                      |
| `missing`          | 38 | empty                                                      |
| `none`             | 39 | empty                                                      |
| `all`              | 40 | a value of any type (see [Fusion Values](#733-complex-values)) |
| `any`              | 41 | none: no value has type `any`, the top type of the type system |

IDs 42 through 47 are reserved for future primitive types.  A type whose
body is "reserved" has a type ID, so it may appear in a type, but its values
are not yet defined.

An unsigned integer is encoded as the minimum number of bytes needed to hold
it in little-endian order, so zero has an empty body.
A signed integer `i` is first mapped to an unsigned integer
as `i<<1` when `i` is nonnegative and as `(-i)<<1 | 1` when `i` is negative,
which is then encoded as an unsigned integer.  (The unsigned integer 1
represents the minimum 64-bit signed integer.)
Since the length of a body is always known, these variable-length integers
need no further delimiters.

A `net` value's address is masked by its mask, whose leading one bits give
the prefix length.

## 6. Type Values

A type value (a value of type `type`, the fused type of a super frame, or the
subtype of a fusion value) does not depend on any typedefs table, so it is the
same in every frame.  The body of a type value is the type's encoding as
follows.  A primitive type is its type ID encoded as a single byte.  A complex type is
encoded recursively as a 1-byte code followed by:

| Code | Type         | Encoding                                                             |
|------|--------------|----------------------------------------------------------------------|
|  48  | record       | `uvarint` number of fields, then each field's name and type value    |
|  49  | array        | element type value                                                   |
|  50  | set          | element type value                                                   |
|  51  | map          | key type value, value type value                                     |
|  52  | union        | `uvarint` number of types, then each type value in canonical order   |
|  53  | enum         | `uvarint` number of symbols, then each symbol                        |
|  54  | error        | value type value                                                     |
|  55  | named (definition) | name, underlying type value                                    |
|  56  | named (reference)  | name                                                           |
|  57  | fusion       | supertype type value                                                 |
|  58  | option       | value type value                                                     |
|  59  | decimal      | `uvarint` precision, `uvarint` scale                                 |

where names and symbols are counted strings.

The first occurrence of a named type in a type value, in a left-to-right
depth-first traversal, is a definition and later occurrences are references,
which allows recursive types.  The name binding is local to the type
value.

## 7. Row Frames

A row frame encodes a sequence of values, each with its type ID in the
frame's typedefs table.

### 7.1 Row Frame Header

A row frame begins with a 26-byte header:

| Offset | Size | Field                                                       |
|-------:|-----:|-------------------------------------------------------------|
|      0 |    4 | magic, `SUPR` (or `SUPX` for a control frame)                |
|      4 |    2 | version                                                     |
|      6 |    8 | frame size, which includes the header                       |
|     14 |    4 | type ID of the frame's fused type in its typedefs table     |
|     18 |    8 | size of the typedefs section                                |

### 7.2 Row Frame Sections

The header is followed by a typedefs section, which is the frame's
[typedefs table](#4-typedefs-tables), and the values section, which fills
the rest of the frame:
```
|<header:26>|<typedefs>|<values>|
```
The values section is a sequence of values, each encoded as its type ID
followed by its length-prefixed body:
```
|<type-id><length><body>|<type-id><length><body>|...
```
where `<type-id>` and `<length>` are `uvarint`s.

### 7.3 Values

A value is encoded as a _body_ whose length is known from its context.
Where values are concatenated, each value is _length-prefixed_, i.e.,
encoded as the `uvarint` length of its body followed by its body:
```
|<length><body>|
```
The body of a value of a complex type is a _container_, which is the
concatenation of the length-prefixed values described below.  This structure is
explicit in every value so the boundaries of each value and its nested
values can be found without knowledge of their types, which allows
values to be traversed efficiently and only the parts of interest to be
interpreted.

#### 7.3.1 Primitive Values

The body of a primitive value is described in the
[Primitive Types](#5-primitive-types) table.

#### 7.3.2 Type Values

The body of a value of type `type` encodes the entire type canonically as
described in [Type Values](#6-type-values).

#### 7.3.3 Complex Values

The body of a value of a complex type is:

| Type     | Body                                                              |
|----------|-------------------------------------------------------------------|
| record   | container of the values of its fields in field order              |
| array    | container of its elements                                         |
| set      | container of its elements, normalized (see below)                 |
| map      | container of its keys and values alternating, normalized          |
| union    | container of its tag as an unsigned integer and its value         |
| enum     | the position of its symbol as an unsigned integer                 |
| error    | the body of its value                                             |
| named    | the body of its value as its underlying type                      |
| option   | container of 0 and its value if it has one, else container of 1  |
| fusion   | container of its value and the [type value](#6-type-values) of its subtype |

The integers in these bodies (e.g., a union's tag) are encoded as
in [Primitive Types](#5-primitive-types), so a union value of tag 0 begins
with a length-prefixed value of length 0.

A set's elements are normalized by sorting them by their length-prefixed
bytes and removing duplicates.  Likewise, a map's entries are normalized
by sorting them by the length-prefixed bytes of their keys and removing entries
with duplicate keys.

A fusion value is its value encoded as a value of the fusion's supertype
followed by the type value of the value's actual type.  A value of type
`fusion(all)` holds the body of a value of any type, which is
interpreted according to the subtype.

### 7.4 Control Frames

A control frame has the same format as a row frame but the magic `SUPX`.
Its values are application-defined control messages, which are delivered
in order with the values of the file (e.g., to signal the end of a
logical stream to a receiver).  A reader that doesn't understand a
control message may ignore it.

## 8. Column Frames

A column frame encodes a sequence of values as one or more vectors.
A vector holds a sequence of values of a single type, which is described by
the type's _locators_: its length and the location in the
frame's data section of each _segment_ of the data that encodes its
values.  For example, a vector of `int64` values has one segment of
integers and a vector of records has a vector for each of its fields.
If the frame's values have more than one type, its values are a _dynamic_
of several vectors, each of a different type, with a tag for each value that
indicates its vector.

A reader that needs only some of the values of a frame (e.g., the
values of some record fields) reads the locators and then only the
segments it needs.

### 8.1 Column Frame Header

A column frame begins with a 42-byte header:

| Offset | Size | Field                                                       |
|-------:|-----:|-------------------------------------------------------------|
|      0 |    4 | magic, `SUPC`                                               |
|      4 |    2 | version                                                     |
|      6 |    8 | frame size, which includes the header                       |
|     14 |    4 | type ID of the frame's fused type in its type values table  |
|     18 |    8 | size of the vector typedefs section                         |
|     26 |    8 | size of the locators section                                |
|     34 |    8 | size of the type values section                             |

### 8.2 Column Frame Sections

The header is followed by four sections:
```
|<header:42>|<vector typedefs>|<locators>|<type values>|<data>|
```

* The _vector typedefs_ section is a [typedefs table](#4-typedefs-tables) of
  the types of the frame's vectors.
* The _locators_ section holds the [locators](#83-locators) of the frame's
  vectors, which refer to types in the vector typedefs table.
* The _type values_ section is a [typedefs table](#4-typedefs-tables) of
  the types of the frame's type values and the subtypes of its fusion
  values, and the frame's fused type.  These types are held apart from
  the vector types since a reader typically needs them only for the
  vectors it reads.
* The _data_ section fills the rest of the frame and holds the
  segments of the frame's vectors.

### 8.3 Locators

The locators section begins with the number of root vectors and the type ID
of each:
```
|<nroots>|<type-id-1><type-id-2>...|<tags>|<vector-1><vector-2>...|
```
where `<nroots>` and each type ID are `uvarint`s.  If there is more than one
root vector, the values of the frame form a dynamic and `<tags>` is a
[segment](#84-segments) of [unsigned 32-bit integers](#85-segment-data),
one for each value, whose value is the index of the root vector that holds
the value, so the frame's length is the sum of the lengths of the root
vectors.  Otherwise, `<tags>` is absent and the single root vector holds the
frame's values.  Each `<vector>` is the locators of a root vector.

The locators of a vector are encoded by traversing its type in depth-first
order.  The locators of a vector begin with a 1-byte _kind_:

| Kind | Name  | Description                                                 |
|------|-------|-------------------------------------------------------------|
|  0   | plain | a vector whose values are encoded by type as described below |
|  1   | const | a vector whose values are all the same value                |
|  2   | dict  | a vector of a primitive type encoded as a dictionary        |
|  3   | none  | the values of an option vector whose values are all none    |

In the descriptions below, `<count>` is a `uvarint` length of a vector and
each `<segment>` is a [segment](#84-segments).

A _const_ vector is
```
|0x01|<count>|<value>|
```
where `<value>` is a `uvarint` that is 0 for no value or the body length
plus 1 followed by the body of the vector's value
(see [Values](#73-values)).  A const vector of
count 0 is an empty vector.  A const vector may have any type, including
the reserved primitive types and decimal types, which have no plain encoding.

A _dict_ vector is
```
|0x02|<values>|<counts>|<index>|
```
where `<values>` is the locators of a plain vector of the dict's distinct
values (at most 256), `<counts>` is a segment of unsigned 32-bit integers
whose `k`th integer is the number of occurrences of the `k`th distinct value,
and `<index>` is an uncompressed segment of one byte for each value of the
vector whose byte is the position of its value among the distinct values.
The vector's length is the length of `<index>`.  Only a vector of a primitive
type may be a dict vector.

A _none_ vector is
```
|0x03|<count>|
```
which may appear only as the values of an option vector (see below).

A _plain_ vector is encoded according to its type:

| Type | Locators after the kind byte |
|------|------------------------------|
| `uint8`...`uint64`, `int8`...`int64`, `duration`, `timestamp` | `<count><values>`: a segment of 64-bit integers |
| `float16`, `float32`, `float64` | `<count><values>`: a segment of 8-byte IEEE 754 binary64 values |
| `bool` | `<count><values>`: a segment of bits |
| `bytes`, `string`, `uuid`, `all` | `<count><bytes><offsets>`: a [bytes table](#85-segment-data) of the values' bodies |
| `ip`, `net` | `<count><bytes><offsets>`: a bytes table of the values' bodies |
| `type` | `<count><ids>`: a segment of unsigned 32-bit type IDs in the type values table |
| `null`, `missing`, `none` | `<count>` |
| record | the locators of each field's vector in field order, or `<count>` if the record has no fields |
| array, set | `<count><offsets><values>` |
| map | `<count><offsets><keys><values>` |
| union | `<count><tags><member-1><member-2>...` |
| enum | `<count><values>`: a segment of 64-bit unsigned integers, each the position of a symbol |
| error | `<values>` |
| named | `<values>` |
| fusion | `<values><subtypes>` |
| option | `<count><tags><values>` |

where:
* the vector of a record's field has the record's length;
* `<offsets>` of an array, set, or map is a segment of `<count>+1` unsigned
  32-bit integers, starting with 0, where the elements of the `k`th value
  are the elements of `<values>` (or the entries of `<keys>` and `<values>`)
  from offset `k` up to offset `k+1`;
* `<tags>` of a union is a segment of unsigned 32-bit integers that gives
  each value's union tag and `<member-k>` is the locators of the vector of the
  values whose tag is `k-1`, in order.  If the union has two member types,
  `<tags>` instead holds the lengths of alternating runs of values with
  tag 1 and tag 0, starting with a run of tag 1 (which may be 0), and is
  empty if all values have tag 0;
* `<values>` of an error or named vector is the locators of the vector of
  its values, which has its length;
* `<values>` of a fusion vector is the locators of the vector of its values
  as values of the fusion's supertype (or of type `all`, for `fusion(all)`),
  which has its length, and `<subtypes>` is a segment of unsigned 32-bit
  integers, one for each value, that is the ID of its subtype in the type
  values table; and
* `<tags>` of an option vector is a segment of unsigned 32-bit integers,
  one for each value, that is 0 if the value has a value and 1 if
  not, and `<values>` is the locators of the vector of the values that
  are present, in order.  If all the values are present or none are,
  `<tags>` is empty, and if none are, `<values>` is a none vector of the
  option's length.

A vector's length is encoded only where it can't be derived from its
children's lengths, e.g., a record's length is the length of its first
field.

Vectors may be nested no more than 10,000 deep, counting each
vector (e.g., an array of records of `int64` values is three vectors deep).

### 8.4 Segments

A segment is a contiguous range of the frame's data section.  It is encoded
in the locators as its compression format, the length of its data
when uncompressed, and, if it's compressed, its length in the data section:
```
|<format:1>|<length>|[<compressed-length>]|
```
where the lengths are `uvarint`s and `<compressed-length>` is present only if
`<format>` is not 0.  The compression formats are:

| Format | Compression                                                                       |
|--------|-----------------------------------------------------------------------------------|
|   0    | none                                                                              |
|   1    | [LZ4 block](https://github.com/lz4/lz4/blob/master/doc/lz4_Block_format.md)       |

The offset of a segment isn't encoded.  Instead, the segments of a frame are
written to its data section in the order in which they appear in the locators
section with no gaps between them, so each segment begins where the
previous one ends and the first begins at the start of the data section.
The segments must fill the data section exactly.

A dynamic occurs only at the root of a column frame.  Values of a union
type, by contrast, are encoded as union vectors, which retain the union type.

### 8.5 Segment Data

The data of the segments (after any decompression) are encoded as follows:

* A segment of 64-bit integers (signed or unsigned) or of unsigned 32-bit
  integers holds them compressed with the bit-packing integer codec of
  [`github.com/ronanh/intcomp`](https://github.com/ronanh/intcomp)
  (`CompressInt64`, `CompressUint64`, or `CompressUint32`) as that codec's
  little-endian 64-bit or 32-bit words.  Such a segment's compression format
  is always 0.
* A segment of 8-byte binary64 values holds the values in order.
  Values of `float16` and `float32` vectors are widened to binary64.
* A segment of bits holds bit `k` of the vector in bit `k%8` of byte `k/8`,
  padded to a multiple of 8 bytes.
* A _bytes table_ is two segments: the concatenation of the values' bodies
  and a segment of `<count>+1` unsigned 32-bit offsets, starting with 0, where
  the body of the `k`th value is the bytes from offset `k` up to offset `k+1`.
  The offsets segment may be empty when `<count>` is 0.

The encoder decides whether to compress a segment of binary64
values, bits, or bytes with LZ4, e.g., only when compression saves space.

### 8.6 Zone Maps

The zone maps section of the [super footer](#9-super-footer) of a column
super frame holds, for each column frame of the super frame, the minimum
and maximum values of the frame's _columns_, i.e., of its vectors of
primitive values, which a reader can use to skip frames that can't contain
values that match a query.

The section is the concatenation of a
[length-prefixed](#73-values) container
for each column frame in order.  A frame's container holds the
length-prefixed offset of the frame from the start of its super frame, as an
8-byte integer, followed by a zone map for each of its columns, sorted by
column path:
```
|<offset:8>|<zone-map-1><zone-map-2>...|
```
where each value is length-prefixed and each zone map is
```
|<path>|<min-type:2>|<min>|<max-type:2>|<max>|
```
where `<path>` is a container of the column's path, the
names of the record fields that lead to the column from the root of the
frame's values, as length-prefixed strings, and `<min-type:2>` and
`<max-type:2>` are the 2-byte primitive type IDs of the bodies of
the minimum value `<min>` and maximum value `<max>`.

A column's zone map summarizes the values of all the vectors at its path,
including the values of const and dict vectors.  Only values of integer,
float, `duration`, `timestamp`, `bytes`, `string`, `uuid`, `ip`, and `net`
types are summarized, along with enum values as the `uint64` positions of
their symbols.  The values of named, error, option, union, and fusion vectors
(other than `fusion(all)`) are summarized at the path of the vector, and the
values of other complex types (e.g., the elements of arrays) are not
summarized.  Since the values at a path may have different types (e.g., the
members of a union), a zone map's minimum and maximum may have different
types, which are compared by the order of super-structured values.

A frame whose root values are not records summarizes them at the empty path.

## 9. Super Footer

A super footer ends each super frame:

| Offset | Size | Field                                                            |
|-------:|-----:|------------------------------------------------------------------|
|      0 |    4 | magic, `SUPS`                                                    |
|      4 |    2 | version                                                          |
|      6 |    8 | footer size, which includes the whole footer and its trailer     |
|     14 |    8 | size of the fused type section                                   |
|     22 |    8 | size of the zone maps section                                    |
|     30 |    8 | super frame size, the total size of the frames of the super frame |
|     38 |      | fused type section                                               |
|        |      | zone maps section                                                |
|        |    8 | footer size again (the trailer)                                  |

The fused type section is the [type value](#6-type-values) of the fused type
of all the values of the super frame.  A reader can determine the type of a
file without reading its frames by traversing the super footers backward
from the end of the file, fusing their fused types.  Starting at the end of
a super footer, its trailer gives the footer's start, and the
super frame size gives the start of its super frame, which is the end of the
previous super footer, if there is one.

The zone maps section holds the [zone maps](#86-zone-maps) of a super frame
of column frames and is empty for a super frame of row frames.

An empty super frame (one with no frames) has no super footer.
