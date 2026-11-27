# Introduction

SuperDB<span class="tm">®</span> is an analytics database that provides
a universal model for managing data that is easier, more precise, and more
flexible than existing data models.

Rather than replace the relational model (RM) or the document model
with an altogether different approach, SuperDB unifies them into a new data model
called the [algebraic data model (ADM)](adm.md).  In this approach,
the RM and the document model are merely special cases of a generalized ADM:
a relational schema is a product type, JSON is one particular sum type,
and the ADM has both.

SuperDB's query language [SuperSQL<span class="tm">®</span>](super-sql/intro.md)
is (aspirationally) backward compatible with SQL while providing a better approach for type-safe
querying of both clean data like relational tables as well as messy data like collections
of ad hoc JSON.

SuperDB is also a natural data layer for AI agents.  Through its
[MCP server](command/db.md#super-db-mcp), an agent can explore a database's
tables, see the precise types of their data &mdash; including exactly where
messy data varies &mdash; and run type-checked queries, so a mistake like
a misspelled field is reported as an error instead of silently returning
the wrong answer.

SuperSQL is particularly well suited for data-wrangling use cases like
ETL and data exploration and discovery.
[Syntactic shortcuts](super-sql/operators/intro.md#shortcuts),
[keyword search](super-sql/operators/search.md), and the
[pipe syntax](super-sql/intro.md#pipe-queries)
make interactively querying data a breeze.  And language features like
[recursive functions](super-sql/expressions/subqueries.md#recursive-subqueries)
with re-entrant subqueries allow for traversing nested data
in a general and powerful fashion.

With SuperSQL and the ADM, SuperDB provides
* deep visibility across large, heterogeneous data sets,
* type-safe queries whether data is schema-constrained or not, and
* an agile means to organize data around types in addition to schemas.

Type safety means bugs are caught in development, test, and QA instead of
landing in production.  It also means that upstream schema changes in data pipelines
or APIs can be managed and handled gracefully and reliably.
When deploying to production, static type checking can be disabled and replaced by
robust dynamic error handling where first-class error values wrap any offending data values;
this means unanticipated changes in the environment can be gracefully accommodated and hotfixed.

SuperDB is implemented as the standalone,
dependency-free [super](command/super.md) command.
`super` is a little like [DuckDB](https://duckdb.org/) and a little like
[jq](https://jqlang.github.io/jq/) but the ADM ties these
two command styles together with static typing of dynamic data.

In addition to the standard uses of the relational model, use cases
for SuperDB include:
* **AI agents**, where type errors guide an agent to a correct query,
* **exploratory data analysis** of unfamiliar JSON, CSV, or Parquet with
  no schema required up front (try out [SuperLens](https://lens.superdb.org)),
* **type-safe ETL**, where pipelines are type checked so upstream schema
  changes are caught before they reach production,
* **JSON analytics at scale**, where columnar BSUP and fusion types make
  messy JSON fast to query,
* **observability and security**, where logs and events of many shapes
  are searched and analyzed side by side, and
* **typed data interchange**, where a program's algebraic data types,
  like the structs, enums, and unions of Rust and TypeScript, are
  [serialized, stored, queried, and read back](adm.md#the-algebraic-boomerang)
  as the same types.

## The Best of Both Worlds

Putting JSON into a relational table &mdash; whether adding a JSON or
[Variant column](https://parquet.apache.org/docs/file-format/types/variantencoding/)
to a relational table or performing schema inference that does not
always work &mdash; is like putting a square peg in a round hole.
SuperDB turns this status quo _upside down_ where both JSON _and_
relational tables are simply special cases of
the more general and holistic algebraic data model.

This leads to ergonomics for SuperDB that are far better for the query language
and for managing data end to end because there is not one way for handling
relational data and a different way for managing dynamic data &mdash;
relational tables and eclectic JSON data are treated in a uniform way
from the ground up.  For example, there's no need for a set of Parquet input files
to all be schema-compatible and it's easy to mix and match Parquet with
JSON across queries.

Unlike SQL, the output of an operator is not constrained to be a table
(see [Values, Not Columns](blog/values.md)).
Instead the ADM provides a query model based on heterogeneously-typed sequences
of values, sometimes ordered and sometimes unordered.
And since a special case of a heterogeneously-typed sequence of values is a table,
the ADM is fully compatible with the RM.

While [schema-constrained tables](super-sql/statements/create-table.md) are supported by SuperDB,
there is no a priori requirement to define a schema up front for many use cases.
Instead, SuperDB accepts heterogeneity as a normal condition of the problem,
rather than as an anomaly that must be eliminated before work can begin.
In fact, SuperDB tables can be created without a schema at all or
created as [super tables](super-sql/statements/create-super-table.md).

To ease adoption, SuperSQL's SQL compatibility targets
[PostgreSQL](https://www.postgresql.org/), which it extends with type concepts
and operators that take advantage of the ADM.

Under the covers, SuperSQL makes extensive use of a novel data type called a
[fusion type](super-sql/types/fusion.md).  Using [type fusion](super-sql/type-fusion.md),
the varying types of messy data like JSON are combined into a single algebraic
type, and fusion values provide a means to recover the original type of any
fused value.

Fusion types are invaluable to the ADM because they provide:
* a means to statically type check arbitrary data no matter how messy or incoherent it is, and
* a high-performance foundation for a query engine operating on heterogeneously typed data.

In this way, SuperDB provides the best of both worlds: the flexibility of the
document data model and the performance and type safety of the relational model.

## The Super Formats

Foundational to the ADM is the ability to reflect its algebraic type system
into the storage layer itself with self-contained and easy-to-use packaging.
To this end, the SuperDB data formats are self describing.

Self-describing data makes data easier: when transmitting data from one entity
to another, there is no need for the two sides to agree up front what the schemas
must be in order to communicate and land the data.

The two super formats are:
* [SUP](formats/sup.md) a human-readable form of super-structured data, and
* [BSUP](formats/bsup.md) a binary-optimized form for performance.

SUP is a superset of JSON in that all valid JSON values are also SUP values.
BSUP supports both a row form (a bit like Parquet's Variant encoding)
as well as columnar form (a bit like Arrow or Parquet).
By default, SuperDB utilizes the columnar form of BSUP and one usually does not
need to worry about these details.

And unlike translating between JSON and Parquet, there is never any loss of
data fidelity when translating between SUP and BSUP.

## The Super Table Format

The ADM carries all the way down to storage.  SuperDB stores each database
table in a cloud-optimized table format called [SuperTable](database/format.md#supertable), made of
immutable BSUP objects, and keeps track of a database's tables in a catalog
format called [SuperCatalog](database/format.md#supercatalog).  A SuperTable can be a schema-constrained
relational table, a super table whose values all conform to an algebraic type
like a union of record types, or a table with no type at all, and like Git,
every table has branches, atomic commits, and time travel.

A [database](database/intro.md) lives entirely at a storage path in cloud
storage or on a local file system, with no separate metastore or service to
set up.  The same `super` command queries it directly, serves it over an HTTP
[API](database/api.md) or to AI agents over MCP, and acts as
a client of that service.

So from the query language to the wire format to the table format, data keeps
its types end to end.  That's what lets SuperDB type-check queries
over messy data that no schema anticipated.

## Try It

Because SuperDB has no dependencies, it's easy to get going &mdash;
just [install the super command](getting-started/install.md) and try it out
on the [command line](command/super.md).

To connect an AI agent like Claude Code to a database, run SuperDB's
[MCP server](command/db.md#super-db-mcp), e.g.,
```
super db init mydb
claude mcp add superdb -- super db mcp "$PWD/mydb"
```
and ask the agent to load and explore your data.

Or, drop some data into [SuperLens](https://lens.superdb.org/) and/or use its built-in
sample data.

## Get Involved

SuperDB is open source and we'd love your feedback, questions, and
contributions.  Join us on
* [Slack](https://www.brimdata.io/join-slack/) to ask questions and chat
  with the team,
* [GitHub issues](https://github.com/superdb/super/issues) to report bugs
  and request features, or
* [GitHub pull requests](https://github.com/superdb/super/pulls) to
  contribute.

See you online!
