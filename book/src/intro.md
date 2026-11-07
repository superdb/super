# Introduction

SuperDB<span class="tm">®</span> is an analytics database and command-line tool
for querying data of any shape &mdash; relational tables, JSON, CSV, Parquet,
logs, API responses &mdash; with SQL or with a simpler pipe syntax.
Point it at your data and start querying: there are no schemas to define and
no loading step.

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

* **One model for tables and JSON.**
  Relational systems handle JSON as a special column type with its own
  operators and rules.  SuperDB instead treats tables and JSON as two cases of
  a single, more general model called the
  [Algebraic Data Model](adm.md), so the same queries work on both.
* **Strongly typed without schemas.**
  Data is [self-describing](formats/model.md), so it carries its types with it
  and those types can vary from value to value, yet values are never coerced
  or confused with one another.
* **SQL compatible, plus pipes.**
  [SuperSQL](super-sql/intro.md) is a
  [Pipe SQL](https://research.google/pubs/sql-has-problems-we-can-fix-them-pipe-syntax-in-sql/)
  that aims to be backward compatible with
  [PostgreSQL](https://www.postgresql.org/) while adding pipe operators that
  work on data of any type.
* **Lossless formats.**
  SuperDB reads and writes JSON, CSV, Parquet, Arrow, and
  [other formats](command/formats.md), and its own
  [super-structured formats](formats/intro.md) &mdash; a
  [text format](formats/sup.md) and a
  [binary format](formats/bsup.md) with both row and columnar layouts &mdash;
  preserve every type in the data.
* **A single binary.**
  `super` has no dependencies, so getting started is just a matter of
  [installing it](getting-started/install.md).

## The `super` Command

SuperDB is implemented as the standalone [super](command/super.md) command.
`super` is a little like [DuckDB](https://duckdb.org/) and a little like
[jq](https://stedolan.github.io/jq/), with strongly typed, dynamic data
tying the two styles together.

Run `super` with a query given by `-c` to query files, streams, or URLs directly:
```sh
super -c "SELECT count(*) FROM 'events.json'"
```
or use its [db](command/db.md) subcommands to manage and query data in a
persistent [SuperDB database](command/db.md), whose design rhymes with the
[lakehouse pattern](https://www.cidrdb.org/cidr2021/papers/cidr2021_paper17.pdf).

>[!NOTE]
> The persistent database layer is still under development and not yet
> ready for turnkey production use.

## SuperSQL<span class="tm">®</span>

SuperSQL works like SQL when data looks like a table:
```sh
$ super -c "
SELECT a+b AS x, a-b AS y FROM (
  VALUES (1,2), (3,0)
) AS T(a,b)
"
{x:3,y:-1}
{x:3,y:3}
```
and its pipe operators extend SQL to data of any type.
Pipes compose a query as a sequence of simple steps, which makes
interactive exploration, data wrangling, and ETL easier, and
[shortcuts](super-sql/operators/intro.md#shortcuts) and
[keyword search](super-sql/operators/search.md) cut down on typing.

SQL has well-known flaws, e.g., see
[C.J. Date's critique](https://dl.acm.org/doi/pdf/10.1145/984549.984551),
[Against SQL](https://www.scattered-thoughts.net/writing/against-sql/), and
[Neumann and Leis](https://www.cidrdb.org/cidr2024/papers/p48-neumann.pdf),
but it is also everywhere.  Rather than replace SQL, SuperSQL keeps full
compatibility in its SQL clauses while its pipe operators modernize the
language, e.g., they use simple [pipe scoping](super-sql/intro.md#pipe-scoping)
instead of SQL's complex scoping rules, 0-based array indexing, and
case-sensitive field names, and they can produce results that need not fit in a
table.  This way, SuperSQL can run existing SQL while providing a gradual path
to a more ergonomic language.

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
