# SuperDB [![Tests][tests-img]][tests] [![GoPkg][gopkg-img]][gopkg]

[SuperDB](https://superdb.org/) is an analytics database built on the
[algebraic data model](https://superdb.org/adm.html), which unifies
relational tables and JSON in a single type system.

A relational schema is a product type.  JSON is one particular sum type.
SuperDB has both, in the form of the records, unions, arrays, and maps
found in languages like Rust and TypeScript, so tables and JSON are each
just a special case.  This means:

* there are no "JSON columns" and no separate "JSON way to do things",
* messy data is ingested as is, with no schema inference to fall over,
* every value has a precise type whether or not anyone declared a schema, and
* queries are type checked against those types.

The query language, [SuperSQL](https://superdb.org/super-sql/intro.html),
is (aspirationally) backward compatible with SQL and adds
[pipe syntax](https://superdb.org/super-sql/intro.html#pipe-queries) and
[lots of fun shortcuts](https://superdb.org/super-sql/operators/intro.html#shortcuts).

## Example

Here is some messy JSON in a file `shapes.json`:
```
{"id":1,"type":"Rectangle","dimensions":{"width":2,"height":1}}
{"id":2,"type":"Circle","dimensions":{"radius":3}}
{"id":3,"type":"Square","dimensions":{"side":5}}
{"id":4,"type":"Circle","dimensions":{"radius":7}}
{"id":5,"type":"Vector","dimensions":[1,2,3,2,1]}
```
Nobody wrote a schema, but the data has a type:
```
$ super -s -c 'from shapes.json | aggregate fuse(this)'
<{id:int64,type:string,dimensions:{width:int64|missing,height:int64|missing,radius:int64|missing,side:int64|missing}|[int64]}>
```
The types are stored with the data, so a query is checked before it runs:
```
$ super -o shapes.bsup shapes.json
$ super -s -c 'from shapes.bsup | values dimensions.radius'
no such field "radius": . applied to [int64] in {width:int64|missing,height:int64|missing,radius:int64|missing,side:int64|missing}|[int64] at line 1, column 38:
from shapes.bsup | values dimensions.radius
                                     ~~~~~~
field may be missing (consider ok() or ?.) at line 1, column 27:
from shapes.bsup | values dimensions.radius
                          ~~~~~~~~~~~~~~~~~
```
The checker knows that `dimensions` might be an array and that `radius`
might be missing.  Say that's expected and you get the answer:
```
$ super -s -c 'from shapes.bsup | values dimensions.radius.ok()'
3
7
```
With a JSON column in SQL, the first query would have quietly returned
`NULL` for most of the shapes, and so would a misspelled `radious`.

## What's Here

* The [`super`](https://superdb.org/command/super.html) command, a
  dependency-free binary that queries files, HTTP endpoints, and S3 paths
  in JSON, CSV, Parquet, Arrow, and other common formats.
* The self-describing super formats: [SUP](https://superdb.org/formats/sup.html),
  a human-readable superset of JSON, and
  [BSUP](https://superdb.org/formats/bsup.html), its efficient binary form
  with both row and columnar layouts.
* A [database](https://superdb.org/database/intro.html) that lives at a
  path in cloud storage or a local file system, where a table can have a
  relational schema, any algebraic type, or no type at all, and
  every table has branches, atomic commits, and time travel.
* An [MCP server](https://superdb.org/command/db.html#super-db-mcp) so
  AI agents can explore a database's types and run type-checked queries.

## Project Status

Our long-term goal is for data to keep its types end to end, from the
application through the wire format, storage, and queries and back again,
with the relational model as a special case rather than the whole story.

In the meantime, SuperDB is stabilizing but still a bit of a moving
target.  We would love [your engagement](#join-the-community) to evolve
and fine tune its syntax and semantics.

## Try It

[Install](https://superdb.org/getting-started/install.html) the `super`
command, drop some data into [SuperLens](https://lens.superdb.org/)
in your browser, or read the [docs](https://superdb.org).

## Contributing

See the [contributing guide](CONTRIBUTING.md) on how you can help improve SuperDB!

## Join the Community

Join our [public Slack](https://www.brimdata.io/join-slack/) workspace for announcements, Q&A, and to trade tips!

[tests-img]: https://github.com/superdb/super/workflows/Tests/badge.svg
[tests]: https://github.com/superdb/super/actions?query=workflow%3ATests
[gopkg-img]: https://pkg.go.dev/badge/github.com/superdb/super
[gopkg]: https://pkg.go.dev/github.com/superdb/super
