# Introduction

SuperDB<span class="tm">®</span> is an analytics database and command-line tool
for querying data of any shape &mdash; relational tables, JSON, CSV, Parquet,
logs, API responses &mdash; with SQL or with a simpler pipe syntax.
Instead of fitting data into tables with predefined schemas, SuperDB gives
every value a type: a table is simply a sequence of records of the same type
and a schema is simply a record type.  So you can point SuperDB at your data
and start querying without defining schemas or loading anything first.

```sh
$ super -c "SELECT 'hello, world' AS greeting"
{greeting:"hello, world"}
```

> [!NOTE]
> The SuperDB source is available as a
> [GitHub repository](https://github.com/superdb/super).  Pre-built binaries
> may be [downloaded and installed](getting-started/install.md)
> via customary mechanisms.

## A Quick Look

Suppose a file `events.json` holds some messy JSON, as real-world JSON
often is.  One amount is a string, one is `null`, and one is missing
altogether:
```json
{"id":1,"user":"alice","amount":2}
{"id":2,"user":"bob","amount":10}
{"id":3,"user":"carol","amount":"7"}
{"id":4,"user":"dave","amount":null}
{"id":5,"user":"erin"}
```
You can query the file with SQL:
```sh
$ super -c "SELECT user, amount FROM 'events.json' WHERE amount > 5"
{user:"bob",amount:10}
```
or with the equivalent [pipe syntax](super-sql/intro.md):
```sh
$ super -c "from events.json | where amount > 5 | values user"
"bob"
```
Since every value in SuperDB has a precise type, the string `"7"` is not
mistaken for a number and you can ask what types are actually in your data:
```sh
$ super -c "from events.json | count() by typeof(amount) | sort this"
{typeof:<int64>,count:2}
{typeof:<string>,count:1}
{typeof:<null>,count:1}
{typeof:<error(string)>,count:1}
```
The missing amount is an [error value](super-sql/types/error.md)
rather than a silent `null`, so `null` and "not there" remain distinct.

## Why SuperDB?

* **One Data Model** &mdash;
  Relational systems handle JSON as a special column type with its own
  operators and rules.  SuperDB treats tables and JSON as two cases of
  the [Algebraic Data Model](adm.md), so the same queries work on both.
* **Types not Schemas** &mdash;
  Data is [self-describing](formats/model.md) and carries its types with it.
  Types may vary from value to value, but values are never coerced or
  confused with one another.
* **SQL Compatibility** &mdash;
  [SuperSQL](super-sql/intro.md) aims to be backward compatible with
  [PostgreSQL](https://www.postgresql.org/).
* **Pipes** &mdash;
  [Pipe syntax](https://research.google/pubs/sql-has-problems-we-can-fix-them-pipe-syntax-in-sql/)
  composes a query as a sequence of simple steps that work on data of any type.
* **Many Formats** &mdash;
  SuperDB reads and writes JSON, CSV, Parquet, Arrow, and
  [other formats](command/formats.md) as well as its own
  [super-structured formats](formats/intro.md), a
  [text format](formats/sup.md) and a [binary format](formats/bsup.md)
  with row and columnar layouts, which preserve every type in the data.
* **One Binary** &mdash;
  `super` has no dependencies, so getting started is just a matter of
  [installing it](getting-started/install.md).

## Using `super`

SuperDB is implemented as a single command called [super](command/super.md),
which you use in three ways.

**Without a database**, `super` runs queries directly on files, URLs, and
streams like standard input:
```sh
super -c "SELECT count(*) FROM 'events.json'"
super -c "from https://api.github.com/repos/superdb/super | values name"
cat events.json | super -c "where amount > 5" -
```
In this mode, `super` is a little like [DuckDB](https://duckdb.org/) and a
little like [jq](https://stedolan.github.io/jq/), with strongly typed,
dynamic data tying the two styles together.

**As a database client**, the [super db](command/db.md) subcommands create
tables, load data, and run queries on a persistent SuperDB database, either
directly on its storage, which may be a file system or an S3 object store, or
through a SuperDB service.

**As a database service**, [super db serve](command/db.md#super-db-serve)
runs a service endpoint that clients reach over HTTP using `super db`,
the [database API](database/api.md), or the
[Python client](dev/libraries/python.md).

The design of the database rhymes with the
[lakehouse pattern](https://www.cidrdb.org/cidr2021/papers/cidr2021_paper17.pdf).

>[!NOTE]
> The persistent database layer is still under development and not yet
> ready for turnkey production use.

## SuperSQL<span class="tm">®</span>

[SuperSQL](super-sql/intro.md) is a superset of SQL.  When its inputs are
tables, it works like the SQL you already know:
```sh
$ super -c "
SELECT a+b AS x, a-b AS y FROM (
  VALUES (1,2), (3,0)
) AS T(a,b)
"
{x:3,y:-1}
{x:3,y:3}
```
But SuperSQL also has a rich [algebraic type system](super-sql/types/intro.md)
like those of TypeScript and Rust, with records, arrays, sets, maps, enums,
[unions](super-sql/types/union.md), [named types](super-sql/types/named.md),
and first-class errors and types, along with
[pipe operators](super-sql/operators/intro.md) and expressions that make
data of complex types easy to work with.  For example, this query finds each
user's best score in a nested array, which in SQL would require unnesting
the arrays and regrouping the results:
```sh
$ super -c "
values {user:'alice',scores:[3,1,2]}, {user:'bob',scores:[5,4]}
| values {user, best:(unnest scores | max(this))}
"
{user:"alice",best:3}
{user:"bob",best:5}
```
SuperSQL keeps backward compatibility in its SQL clauses while its pipe
operators modernize the language, e.g., with simpler
[scoping](super-sql/intro.md#pipe-scoping), 0-based array indexing, and
case-sensitive field names, so it can run existing SQL while offering a
gradual path to a more ergonomic language.

## Learn More

To dive deeper:
* follow the [Getting Started](getting-started/intro.md) guide,
* read about the ideas behind SuperDB in
  [The Algebraic Data Model](adm.md),
* explore the [SuperSQL](super-sql/intro.md) query language, or
* learn about the [data model](formats/model.md) and
  [formats](formats/intro.md) underlying SuperDB.

We'd love your feedback and we hope to build a thriving community around
SuperDB so please feel free to reach out to us via
* [Slack](https://www.brimdata.io/join-slack/),
* [GitHub issues](https://github.com/superdb/super/issues), or
* [GitHub pull requests](https://github.com/superdb/super/pulls).

See you online!
