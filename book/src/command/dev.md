# super dev

```
super dev sub-command [ options ] [ arguments ]
```

The `super dev` command contains various sub-commands for developers.

Run `super dev` with no arguments to see what is available.

The sub-commands documented here are:
* [super dev bsup](#super-dev-bsup) dumps the metadata of a BSUP file.

As the dev tooling matures, the other sub-commands will be documented here
as well.

## super dev bsup

```
super dev bsup [ options ] file
```

### Options

* `-fused` emit the fused types of frames and super frames (default "false")
* `-framelimit` stop after this many frames (default "0", no limit)
* `-headers` emit frame headers and super footers (default "true")
* `-limit` stop after emitting this many values (default "0", no limit)
* `-locatorbytes` write the raw bytes of each column frame's locators
  section to standard output instead of emitting values (default "false")
* `-locators` emit a value for each vector in a column frame's locators
  section (default "false")
* `-stats` emit the zone maps in each super footer (default "false")
* `-typedefs` emit the typedefs table of each row frame and the type values
  table of each column frame (default "false")
* `-vectortypedefs` emit the vector typedefs table of each column frame
  (default "false")
* [Output Options](output.md)
* [Global Options](options.md#global)

### Description

The `super dev bsup` command decodes the structure of a
[BSUP](../formats/bsup.md) file and emits a description of it as a
sequence of super-structured values rather than the values the file holds.
It is useful for debugging BSUP encoders and decoders, understanding how a
file is laid out, and seeing why a query reads the data that it does.

The file is read frame by frame from beginning to end.  For each
[row frame](../formats/bsup.md#7-row-frames) and
[column frame](../formats/bsup.md#8-column-frames), the command emits the
frame's header and, as requested by the options, the frame's sections.
For each [super footer](../formats/bsup.md#9-super-footer), it emits the
footer and, as requested, the super frame's fused type and zone maps.
The values are emitted in file order so each section follows the header of
its frame.

Each kind of value has its own [named type](../super-sql/types/named.md)
(e.g., `RowHeader`, `ColumnHeader`, or `SuperFooter`) or a `Kind` field
(e.g., `TypeDefRecord` or `ColumnFrameFusedType`), so the output is easy to
filter by piping it to another `super` command.

The fields of the headers and footers follow the BSUP specification, e.g.,
a `RowHeader` has the
[row frame header](../formats/bsup.md#71-row-frame-header) fields, a
`ColumnHeader` has the
[column frame header](../formats/bsup.md#81-column-frame-header)
fields, and a `SuperFooter` has the
[super footer](../formats/bsup.md#9-super-footer) fields.
The `Control` field of a `RowHeader` is true for a
[control frame](../formats/bsup.md#74-control-frames).
In a `ColumnHeader`, `TypedefsSize` is the size of the type values section.

With `-typedefs` and `-vectortypedefs`, each entry of a
[typedefs table](../formats/bsup.md#4-typedefs-tables) is emitted as a record
whose `Kind` is the typedef's kind (e.g., `TypeDefRecord` or
`TypeDefUnion`), whose `Slot` is the type ID that the entry defines, and
whose other fields are the entry's fields, in which other types are
referred to by their type IDs.  Type IDs less than 48 are
[primitive types](../formats/bsup.md#5-primitive-types).

With `-locators`, the [locators](../formats/bsup.md#83-locators) of each
column frame are emitted in depth-first order, one value per vector.  Each
value's type is named for the vector's encoding (e.g., `Record`, `Int`,
`Bytes`, `Dict`, or `Const`), and its `Path` is the vector's position in
the frame, e.g., `0.a` is field `a` of the frame's first root vector.
Each [segment](../formats/bsup.md#84-segments) of a vector is described by
its offset and length in the frame's data section, its uncompressed length,
and its compression format.  If the frame's values form a dynamic,
the first value is a `Dynamic` that locates its tags.

The input must be a file; standard input and URLs are not supported.

### Examples

---

_The headers of a file with one column frame_

Given this input in `example.sup`,
```mdtest-input example.sup
{a:1,b:"foo"}
{a:2,b:"bar"}
{a:3}
```
the default BSUP output has one column frame, so
```mdtest-command
super -o example.bsup example.sup
super dev bsup -s example.bsup
```
emits its header and the super footer:
```mdtest-output
type Version={Major:uint8,Minor:uint16}
type ColumnHeader={Version:Version,FrameSize:uint64,FusedType:uint32,VectorTypedefsSize:uint64,LocatorsSize:uint64,TypedefsSize:uint64}
{Version:{Major:0,Minor:31},FrameSize:183,FusedType:50,VectorTypedefsSize:16,LocatorsSize:21,TypedefsSize:18}::ColumnHeader
type SuperFooter={Version:Version,FooterSize:uint64,FusedTypeSize:uint64,StatsSize:uint64,SuperFrameSize:uint64}
{Version:{Major:0,Minor:31},FooterSize:98,FusedTypeSize:12,StatsSize:40,SuperFrameSize:183}::SuperFooter
```

---

_The same values in a row frame_

```mdtest-command
super -rows -o rows.bsup example.sup
super dev bsup -s rows.bsup
```
```mdtest-output
type Version={Major:uint8,Minor:uint16}
type RowHeader={Version:Version,FrameSize:uint64,FusedType:uint32,TypedefsSize:uint64,Control:bool}
{Version:{Major:0,Minor:31},FrameSize:79,FusedType:52,TypedefsSize:33,Control:false}::RowHeader
type SuperFooter={Version:Version,FooterSize:uint64,FusedTypeSize:uint64,StatsSize:uint64,SuperFrameSize:uint64}
{Version:{Major:0,Minor:31},FooterSize:58,FusedTypeSize:12,StatsSize:0,SuperFrameSize:79}::SuperFooter
```

---

_A row frame's typedefs and fused type_

```mdtest-command
super -rows -o rows.bsup example.sup
super dev bsup -s -headers=false -typedefs -fused rows.bsup
```
```mdtest-output
type Field={Name:string,ID:uint32}
{Kind:"TypeDefRecord",Slot:48::uint32,Fields:[{Name:"a",ID:8}::Field,{Name:"b",ID:29}::Field]}
{Kind:"TypeDefRecord",Slot:49::uint32,Fields:[{Name:"a",ID:8}::Field]}
{Kind:"TypeDefUnion",Slot:50::uint32,IDs:[29::uint32,38::uint32]}
{Kind:"TypeDefFusion",Slot:51::uint32,ID:50::uint32}
{Kind:"TypeDefRecord",Slot:52::uint32,Fields:[{Name:"a",ID:8}::Field,{Name:"b",ID:51}::Field]}
{Kind:"RowFrameFusedType",Type:<{a:int64,b:fusion(string|missing)}>}
{Kind:"SuperFrameFusedType",Type:<{a:int64,b:fusion(string|missing)}>}
```
Here, type IDs 8, 29, and 38 are the primitive types `int64`, `string`,
and `missing`, and the fused type is type 52.

---

_A column frame's vectors and their segments_

```mdtest-command
super -o example.bsup example.sup
super dev bsup -s -headers=false -locators example.bsup
```
```mdtest-output
type Segment={Offset:uint64,Length:uint64,MemLength:uint64,CompressionFormat:uint8}
type Dynamic={Length:uint32,Tags:Segment}
{Length:3,Tags:{Offset:0,Length:16,MemLength:16,CompressionFormat:0}}::Dynamic
type Record={Path:string,Type:type,Length:uint32}
{Path:"0",Type:<{a:int64,b:string}>,Length:2}::Record
type Int={Path:string,Type:type,Length:uint32,Segments:[Segment]}
{Path:"0.a",Type:<int64>,Length:2,Segments:[{Offset:16,Length:24,MemLength:24,CompressionFormat:0}]}::Int
type Bytes={Path:string,Type:type,Length:uint32,Segments:[Segment]}
{Path:"0.b",Type:<string>,Length:2,Segments:[{Offset:40,Length:6,MemLength:6,CompressionFormat:0},{Offset:46,Length:16,MemLength:16,CompressionFormat:0}]}::Bytes
{Path:"1",Type:<{a:int64}>,Length:1}::Record
{Path:"1.a",Type:<int64>,Length:1,Segments:[{Offset:62,Length:24,MemLength:24,CompressionFormat:0}]}::Int
```
The two record types form a dynamic whose tags are in the first segment.
The `string` vector has two segments, one for the bytes of its strings and
one for their offsets.

---

_The zone maps of a super frame_

```mdtest-command
super -o example.bsup example.sup
super dev bsup -s -headers=false -stats example.bsup
```
```mdtest-output
type FrameStats={Offset:uint64}
{Offset:0}::FrameStats
type Path=[string]
type ColumnStats={Path:Path,Min:fusion(all),Max:fusion(all)}
{Path:["a"],Min:1::any,Max:3::any}::ColumnStats
{Path:["b"],Min:"bar"::any,Max:"foo"::any}::ColumnStats
```
Each `FrameStats` gives the offset of a frame in its super frame and is
followed by the minimum and maximum values of each column of the frame.

---

_Filter the output with another query_

```mdtest-command
super -o example.bsup example.sup
super dev bsup -headers=false -locators example.bsup |
  super -s -c 'where nameof(this)=="Int" | values {Path,Segments}' -
```
```mdtest-output
type Segment={Offset:uint64,Length:uint64,MemLength:uint64,CompressionFormat:uint8}
{Path:"0.a",Segments:[{Offset:16,Length:24,MemLength:24,CompressionFormat:0}::Segment]}
{Path:"1.a",Segments:[{Offset:62,Length:24,MemLength:24,CompressionFormat:0}::Segment]}
```
