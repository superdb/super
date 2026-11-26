# Algebraic Types, End to End

_November 2026_

Here is a type that a program might use for some shapes, written in Rust:
```rust
struct Shape {
    id: i64,
    dimensions: Dimensions,
}

enum Dimensions {
    Rectangle { width: i64, height: i64 },
    Circle { radius: i64 },
    Square { side: i64 },
    Vector(Vec<i64>),
}
```
The compiler knows everything about a `Shape`.  It knows that a circle has
a radius and a rectangle doesn't, and it won't compile a `match` that
forgets about squares.

Then the program has to send a shape somewhere, and all of that knowledge
is thrown away.

## What JSON Leaves Out

The usual way to send a value somewhere is to serialize it as JSON, and as
we saw in [JSON Can't Tell a Map from a Record](json-map-or-record.md),
JSON has nowhere to put most of what the compiler knew.  A struct becomes
an object that could just as well be a map.  Every integer becomes a
number.  And a sum type like `Dimensions` has no counterpart at all, so the
serializer has to make something up.

In Rust, [serde](https://serde.rs/) does this by adding a discriminator
to the data.  By default it wraps each value in an object keyed by the name
of its variant:
```json
{"id":1,"dimensions":{"Rectangle":{"width":2,"height":1}}}
```
and with an attribute like `#[serde(tag = "type", content = "dimensions")]`
on the enum, it writes a `Dimensions` value with the name next to the
content instead:
```json
{"type":"Rectangle","dimensions":{"width":2,"height":1}}
```
There are [four such conventions](https://serde.rs/enum-representations.html)
to choose from.  None of them is JSON's idea.  Each is a private agreement
between the writer and whoever reads the data next, and the name of the
variant, which was part of the type, is now a string in the data.

On the other end, a TypeScript program calls `JSON.parse` and gets back a
value of type `any`.  To get types again, it describes the data a second
time in a validation library like [Zod](https://zod.dev/), which checks at
run time that the JSON has the shape the program hopes it has:
```ts
const Shape = z.discriminatedUnion("type", [
  z.object({ id: z.int(), type: z.literal("Rectangle"), dimensions: z.object({ width: z.int(), height: z.int() }) }),
  z.object({ id: z.int(), type: z.literal("Circle"), dimensions: z.object({ radius: z.int() }) }),
  z.object({ id: z.int(), type: z.literal("Square"), dimensions: z.object({ side: z.int() }) }),
  z.object({ id: z.int(), type: z.literal("Vector"), dimensions: z.array(z.int()) }),
])
```
This is the same sum type, declared again in another language and
reassembled from the string that serde left behind.  serde and Zod are
both excellent at what they do.  But what they do is encode higher-level
types onto a format that doesn't have them and then decode them on the
other side, and the two ends stay in agreement only as long as somebody
keeps them that way.

## And Then There's the Database

It gets worse when the data has to be stored and queried.  A relational
database has no sum types either, so there are two choices.

You can write ETL that takes the JSON apart and loads it into tables,
using a wide table full of `NULL`s or a table for each kind of shape, as
described in the [ADM section](../adm.md#the-frankentype).
Now the type exists in a third form that has to be kept in step with the
other two.

Or you can put the JSON in a JSON or Variant column and query it there,
where a misspelled key is a `NULL` instead of an error and every query
checks at run time what it's looking at.

Either way, the types exist at both ends and nowhere in between.  Data that
a program throws at a database and later catches again makes a _boomerang_,
and today what comes back is never quite what was thrown.

## Types That Survive the Trip

The [algebraic data model](../adm.md) (ADM) has the same types the
programs do: records, unions, arrays, maps, and names for all of them.
So there's nothing to encode.  A sum type in the program is a sum type on
the wire, in the file, in the table, and in the query, and it comes back
out as the same sum type.

This is the holy grail: an
[algebraic boomerang](../adm.md#the-algebraic-boomerang), where data stays
fully typed, end to end, and comes back exactly as it left.

Here is what it looks like today, in two languages.

### Rust

The [superdb crate](https://github.com/superdb/super-rs) is a serde data
format, so the types above need only the usual derives and no tagging
attributes:
```rust
#[derive(Serialize, Deserialize)]
struct Shape { /* as above */ }

#[derive(Serialize, Deserialize)]
enum Dimensions { /* as above */ }

let shapes = vec![
    Shape { id: 1, dimensions: Dimensions::Rectangle { width: 2, height: 1 } },
    Shape { id: 2, dimensions: Dimensions::Circle { radius: 3 } },
    Shape { id: 3, dimensions: Dimensions::Square { side: 5 } },
    Shape { id: 4, dimensions: Dimensions::Circle { radius: 7 } },
    Shape { id: 5, dimensions: Dimensions::Vector(vec![1, 2, 3, 2, 1]) },
];
std::fs::write("shapes.bsup", superdb::to_vec(&shapes)?)?;
```
The file holds the shapes in [BSUP](../formats/bsup.md) along with
their types, which are the Rust types:
```
type Dimensions_Circle={radius:int64}
type Dimensions_Rectangle={width:int64,height:int64}
type Dimensions_Square={side:int64}
type Dimensions_Vector=[int64]
type Dimensions=Dimensions_Circle|Dimensions_Rectangle|Dimensions_Square|Dimensions_Vector
type Shape={id:int64,dimensions:Dimensions}
{id:1,dimensions:{width:2,height:1}::Dimensions_Rectangle::Dimensions}::Shape
{id:2,dimensions:{radius:3}::Dimensions_Circle::Dimensions}::Shape
...
```
The struct is a named record.  The enum is a union with a named type for
each variant.  There is no `"type"` string, because the variant is in
the type where it belongs.

SuperDB can query the file as is, and because the file carries its types,
a query can refer to the Rust variants by name:
```
super -s -c 'values {id, radius: dimensions.(Dimensions_Circle)?.radius ?? 0}' shapes.bsup
```
```
{id:1,radius:0}
{id:2,radius:3}
{id:3,radius:0}
{id:4,radius:7}
{id:5,radius:0}
```
And a query's results read straight back into the Rust types:
```rust
// bytes from: super -f bsup -c 'where is(dimensions, <Dimensions_Circle>)' shapes.bsup
let circles: Vec<Shape> = superdb::from_slice(&bytes)?;
```

### TypeScript

TypeScript's types are erased when the program is compiled, so a library
needs a description of them that exists at run time, and for many programs
that description is already a Zod schema.
[super-ts](https://github.com/superdb/super-ts) puts it to a different
use.  Instead of checking JSON against the schema, it maps the schema to a
super type and sends the values as that type:
```ts
import { superCodec } from "@superdb/super-ts/zod"

const codec = superCodec(Shape.meta({ id: "Shape" }))
writeFileSync("shapes.bsup", codec.toBSUP(shapes))
```
The discriminated union arrives in SuperDB as a union of named records:
```
type Circle={id:int64,type:string,dimensions:{radius:int64}}
type Rectangle={id:int64,type:string,dimensions:{width:int64,height:int64}}
type Square={id:int64,type:string,dimensions:{side:int64}}
type Vector={id:int64,type:string,dimensions:[int64]}
type Shape=Circle|Rectangle|Square|Vector
{id:1,type:"Rectangle",dimensions:{width:2,height:1}}::Rectangle::Shape
{id:2,type:"Circle",dimensions:{radius:3}}::Circle::Shape
{id:3,type:"Square",dimensions:{side:5}}::Square::Shape
{id:4,type:"Circle",dimensions:{radius:7}}::Circle::Shape
{id:5,type:"Vector",dimensions:[1,2,3,2,1]}::Vector::Shape
```
TypeScript models the variant as a literal `type` property, so that
property is still here, but the variant is now in the type as well.
The union can be queried as what it is, with operators that will look familiar to a
TypeScript programmer:
```
super -s -c 'values {id, radius: this.(Circle)?.dimensions?.radius ?? 0}' shapes.bsup
```
```
{id:1,radius:0}
{id:2,radius:3}
{id:3,radius:0}
{id:4,radius:7}
{id:5,radius:0}
```
And the results come back as values of the TypeScript type, so the
compiler's checks apply to them like any other `Shape`:
```ts
// bytes from: super -f bsup -c 'where type=="Circle" or type=="Square"' shapes.bsup
const round: Shape[] = codec.fromBSUP(bytes)
for (const shape of round) {
  console.log(`${shape.type} ${shape.id} has area ${area(shape)}`)
}
```

In both languages, the boomerang came back as the value that was thrown,
and the type was declared once, in the program.  Nobody
wrote a schema for the database, and nobody wrote ETL.

>[!NOTE]
> Both libraries are new.  The Rust crate reads and writes BSUP and the
> Zod integration is the latest addition to super-ts.  They show where
> SuperDB is headed and they are not yet production ready.

## In the Meantime

Most of the world's data isn't in the ADM, and it won't be for a while.
It's in JSON files, CSV files, Parquet files, and SQL tables, and SuperDB
is built to fit into that world as it is.  SuperSQL is (aspirationally)
backward compatible with SQL.  A relational table is a special case of the
model.  The `super` command reads and writes the common formats.  And
[type fusion](../super-sql/type-fusion.md) lets SuperDB give messy JSON a
precise type and query it at high performance without anyone writing
a schema.

But everything else loses fidelity compared to data that started out in
the ADM.  If we load the JSON form of the shapes, SuperDB does the best
that can be done with what's there, and fuses the five objects into this
type:
```
{id:int64,type:string,dimensions:{width:int64|missing,height:int64|missing,radius:int64|missing,side:int64|missing}|[int64]}
```
That's a real type and queries can be checked against it.  But compare it to
```
type Shape=Circle|Rectangle|Square|Vector
```
which is what the program meant all along.  Fusion has to work out from the
data what the types might have been.  With end-to-end types there is
nothing to work out because the types were never thrown away.

So you can start with the data you have, in the formats you have, and get
real types and type-checked queries on day one.  And as more of your data
is born in programs that speak the ADM, more of it keeps the types it was
born with.

Stay tuned.
