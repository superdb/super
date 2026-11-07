# The Algebraic Data Model

The relational model is unquestionably the king of data models.
Its champions have declared victory, citing the many failed attempts to replace it
[[1]](#references), and Andy Pavlo teaches his undergraduates that
"in the same way... one plus one is two..., the relational model is the true way,
the correct way... to represent data"
[[2]](#references).

We think this is wrong.
The relational model is not wrong, but it is not the end of the story.
SuperDB is built on the premise that the relational model should become
a special case of a more general model, which we call the
_Algebraic Data Model_ (ADM).  This section explains why.

## The Trouble with JSON in a Table

Modern data rarely arrives as neat tables.  It arrives as JSON from APIs,
as events and logs, and as documents whose shape varies from one to the next.
The relational answer is to put such data in a column of a special
JSON or [variant](https://github.com/apache/parquet-format/blob/master/VariantEncoding.md)
type, or to infer a relational schema for it.  Neither works well.

Consider again the messy `events.json` from the [introduction](intro.md#a-quick-look):
```json
{"id":1,"user":"alice","amount":2}
{"id":2,"user":"bob","amount":10}
{"id":3,"user":"carol","amount":"7"}
{"id":4,"user":"dave","amount":null}
{"id":5,"user":"erin"}
```
Loaded into a PostgreSQL table with a `jsonb` column called `payload`,
the seemingly simple question "which amounts are greater than 5?"
has four different answers depending on how it's asked:
```sql
-- compares text, so "10" < "5" and only carol's "7" matches
SELECT id FROM events WHERE payload->>'amount' > '5';            -- 3
-- converts text to integers, so carol's string matches
SELECT id FROM events WHERE (payload->>'amount')::int > 5;       -- 2, 3
-- compares JSON values, which orders all numbers above all strings
SELECT id FROM events WHERE payload->'amount' > '5'::jsonb;      -- 2
-- converts JSON values to integers
SELECT id FROM events WHERE (payload->'amount')::int > 5;
-- ERROR:  cannot cast jsonb string to type integer
```
Arithmetic doesn't work on a JSON number at all:
```sql
SELECT (payload->'amount') + 1 FROM events;
-- ERROR:  operator does not exist: jsonb + integer
```
And there are two kinds of null: dave's JSON `null` is _not_
SQL `NULL` when accessed with `->`, but it is with `->>`, while
erin's missing amount is SQL `NULL` either way.

Even a literal array that mixes a number and a string, which is
perfectly ordinary JSON, is an error in SQL:
```sh
$ psql -c "SELECT array[1,'foo']"
ERROR:  invalid input syntax for type integer: "foo"
$ duckdb -c "SELECT [1,'foo']"
Conversion Error:
Could not convert string 'foo' to INT32
```

None of these behaviors is a bug.  They arise because the JSON type isn't
really a type at all.  It's an entire type system of its own &mdash; what we call
a _special-purpose recursive sum type_ (SRST) &mdash; with its own values,
operators, ordering, and nulls, bolted inside a single relational column.
The result is a _frankentype_ that combines two incompatible type disciplines
in one data structure.  Since SQL can't express user-defined sum types or
recursive types, all the variation in the data is pushed into this one opaque
type, developers reconstruct the meaning of their data by hand from generic
key-value trees, and the query optimizer can't see the types inside.

Other SRSTs, like the [Parquet variant type](https://github.com/apache/parquet-format/blob/master/VariantEncoding.md),
[BSON](https://bsonspec.org/), and [CBOR](https://cbor.io/), are more
general than JSON but suffer the same problems when embedded in a table.
Systems are beginning to add more general types &mdash; ClickHouse, for example,
infers the mixed array `[1,"foo"]` as `Array(Dynamic)` &mdash; but these
efforts patch the relational model piecemeal rather than defining a new foundation.

## Sum Types and Product Types

What's missing is a native way to say that a value may be one of several
types.  Modern programming languages do this with a
[sum type](https://en.wikipedia.org/wiki/Tagged_union) (also called a
tagged union, an enum in Rust, or a union type in TypeScript), alongside the
_product type_ (a struct or record) that combines several values into one.
Types built from sums, products, and recursion are called
[algebraic data types](https://en.wikipedia.org/wiki/Algebraic_data_type).

The relational model has the products but not the sums.
In fact, Codd's original paper describes a relation's schema in a
footnote as essentially a product type:

<!-- markdownlint-disable-next-line MD033 -->
<center><img src="codd-footnote.png" alt="Codd's Footnote" title="Codd's Footnote" width="500"></center>

but sum types are notably absent.

## Turning the Relational Model Inside Out

Rather than embed SRSTs in the relational model, the ADM turns the
relational model inside out: it starts from algebraic types, and relational
tables and JSON documents both fall out as special cases.
In the ADM, data is a sequence of values, each of which has a
[type](formats/model.md) built from primitive types (like `int64` and `string`),
products (records), sums ([unions](super-sql/types/union.md)), and the other
types of the [data model](formats/model.md).

**A relational table is a sequence of records of the same type.**
So the relational model is embedded unchanged in the ADM and SQL
works on such data as usual:
```sh
$ super -c "SELECT user, amount FROM 'events.json' WHERE amount > 5"
{user:"bob",amount:10}
```

**A JSON document is a value with a precise type.**
Rather than an opaque `json`, every JSON value has an algebraic type,
e.g., the mixed array is simply an array of a sum type:
```sh
$ super -c "SELECT [1,'foo'] AS a, typeof([1,'foo']) AS type"
{a:[1,"foo"],type:<[int64|string]>}
```
and a collection of documents, like `events.json`, is a sequence of values
that may differ in type:
```sh
$ super -c "from events.json | count() by typeof(this) | sort this"
{typeof:<{id:int64,user:string}>,count:1}
{typeof:<{id:int64,user:string,amount:int64}>,count:2}
{typeof:<{id:int64,user:string,amount:string}>,count:1}
{typeof:<{id:int64,user:string,amount:null}>,count:1}
```

**An SRST is just another algebraic type.**
Since an SRST is a recursive sum type, it drops right into the model.
For example, JSON's entire type system is a single
[named](super-sql/types/named.md) recursive union:
```
type json=null|bool|float64|string|[json]|map{string:json}
```

Because values carry precise types, the problems above go away.
`amount > 5` compares numbers as numbers, so carol's string `"7"` doesn't
match and bob's `10` does.  A type mismatch is visible in the data rather
than hidden by a coercion, e.g., `sum(amount)` over `events.json` is the
error `"mixture of string and numeric values"` rather than a quietly wrong total.
And `null` and a missing value are different things.

## Algebraic Types in Queries

The ADM also gives queries what SQL lacks: user-defined sum types.
A relational schema for shapes, for example, might use one sparse table with a
`kind` column and nullable `radius`, `width`, and `height` columns, which
can't express that a circle has a radius and no width.  In SuperSQL,
the variants of a sum type are simply [named types](super-sql/types/named.md)
and a [match](super-sql/expressions/match.md) expression handles each one:
```sh
$ super -c "
type circle={radius:float64}
type rect={width:float64,height:float64}
values {radius:1}, {width:2,height:1}, {radius:2}, {width:1,height:3.5}
| values cast(this, <circle|rect>)
| values {
    shape: nameof(this),
    area: match this
      when circle 3.14*radius*radius
      when rect width*height
  }
"
{shape:"circle",area:3.14}
{shape:"rect",area:2.}
{shape:"circle",area:12.56}
{shape:"rect",area:3.5}
```
Here, the cast to `circle|rect` classifies each value by its shape, and `match`
dispatches on the type of each value, much like a `match` in Rust or a `case`
in Haskell.

More generally, every operator in SuperSQL has defined behavior for values of
every type.  In this sense, SuperDB generalizes Codd's relational algebra to
an algebra of polymorphic operators over sequences of arbitrarily typed values,
and SuperSQL is a superset of SQL.

## Fusion Types

The flexibility of the ADM comes with a well-known problem: when abstract types
are serialized as JSON, a collection can hold a great many different types,
which defeats the efficient, column-oriented processing that tables enable.
SuperDB addresses this with
[type fusion](super-sql/type-fusion.md) [[3]](#references), which
combines many types into a single _fused type_, e.g.,
```sh
$ super -c "from events.json | fuse | values typeof(this) | head 1"
<{id:int64,user:string,amount:int64|string|null|missing}>
```
Unlike conventional schema inference, fusion doesn't lose information.
SuperDB's _fusion type_ records the original type of each fused value so the
data can be processed efficiently in columnar form and still be converted back
to its original form.

## Toward Super Tables

Because they keep their algebraic types, data in SuperDB's
[super-structured formats](formats/intro.md) and in schema-driven formats like
Protocol Buffers can be queried in place as algebraic "super tables" instead of
being flattened into relational tables through tedious ETL.

Just as Codd's relational model freed data from physical storage layouts
over fifty years ago, the ADM frees data from the divide between
the types of applications and the tables of databases.
_It's time for the relational model to become a special case of
an easier and broader approach for modern data._

## References

1. Andy Pavlo and Michael Stonebraker.
   [What Goes Around Comes Around... And Around...](https://db.cs.cmu.edu/papers/2024/whatgoesaround-sigmodrec2024.pdf)
   ACM SIGMOD Record 53, 1 (March 2024).
2. Andy Pavlo.
   [#01 - Relational Model & Algebra](https://youtu.be/APqWIjtzNGE?t=4466),
   CMU Intro to Database Systems, August 2024.
3. Mohamed-Amine Baazizi, Houssem Ben Lahmar, Dario Colazzo, Giorgio Ghelli,
   and Carlo Sartiani.
   Schema Inference for Massive JSON Datasets.
   In Proceedings of the 20th International Conference on Extending Database
   Technology (EDBT 2017).
