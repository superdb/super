# Introduction


* compat  
  * RM-> product types -> parquet
  * Arrow

SuperDB<span class="tm">®</span> is a an analytics database that provides
a univeral model for managing data that is easier, more precise, and more
flexible compared to existing data models.

Instead of attempting to replace the relational model (RM) or the document model
with an altogether different approach, SuperDB unifies them into a new data model
called the [Algebraic Data Model (ADM)](adm.md).  In this approach,
the RM and the document model are merely special cases of a generalized ADM.

With the ADM, SuperDB provides
* deep visibility across large, heterogeneous data sets,
* type-safe queries whether data is schema-constrained or not, and
* an agile means to organize data around types in addition to schemas.

Type safety means bugs are caught in development, test, and QA instead of
landing in production.  It also means that upstream schema changes in data pipelines
or APIs can be managed and handled gracefully and reliably.

## The Best of Both Worlds

Putting JSON into a relational table &mdash; whether adding a JSON or variant
column to a relational table or performing schema inference that does not
always work &mdash; is like putting a square peg in a round hole.
SuperDB turns this status quo _upside down_ where both JSON _and_
relational tables are simply special cases of
the more general and holistic super-structured data model.

This leads to ergonomics for SuperDB that are far better for the query language
and for managing data end to end because there is not one way for handling
relational data and a different way for managing dynamic data &mdash;
relational tables and eclectic JSON data are treated in a uniform way
from the ground up.  For example, there's no need for a set of Parquet input files
to all be schema-compatible and it's easy to mix and match Parquet with
JSON across queries.

In a nutshell, the ADM combines the type safety and relability of the relational model
with the flexibility and ease-of-use of the document model.

Unlike SQL, the output of an operator is not constrained to be a table.
Instead the ADM provides a query model based on heteregenously-typed sequences
of values, sometimes ordered sometimes unordered.
And since a special case of a heteregenously-typed sequence of values is a table,
the ADM is fully compatible with the RM.

While [schema-constrained tables](link to create table) are supported by SuperDB,
there is no a priori requirement to define a schema up front for many use cases.
Instead, SuperDB accepts heterogeneity as a normal condition of the problem,
rather than as an anomaly that must be eliminated before work can begin.”

To ease adoption, SuperDB's query language --- called [SuperSQL](super-sql/intro.md) ---
is backward compatible with [PostgreSQL](https://www.postgresql.org/) and is extended
with type concepts and operators that take advantage of the ADM.

Underneath the covers, SuperSQL makes extension use of a novel data type called a
[fusion type](super-sql/types/fusion.md).  Using [type fusion](super-sql/type-fusion.md),
non-algebraic data like JSON is converted to its equivalent algebraic form and
fusion values provide a means to recover the original type of any fused value.

Fusion types are invaluable to the ADM because they provide:
* a means to statically type check arbitrary data no matter how messy or incoherent it is, and
* a high-performance foundation for a query engine operating on heterogenously typed data.

In this way, SuperDB provides the best of both worlds: the flexibility of the
document data model and the performance and type safety of the relational model.

## Type-preserving Serialization

named types and recursive types

## Use Cases

in addition to bread and butter relational use cases

* exploratory data analysis ... [SuperLens](https://lens.superdb.org).
* agentic workflows (llms.txt, MCP)
* observability
* type-safe ETL
* type-preserving serialization for algebraic data types
* scalable JSON analytics

## The Storage Model

* types are serialized into the super format
  * human readable
  * optimized binary (column and row forms)
* BSUP files organized into database tables like iceberg but for ADM
  * cloud optimized
  * run locally on a file system
* SuperDB can run standalone or attached to a database
  * it can also service database requests

XXX
Static type checking may be disabled when deployed to production so that
errors become dynamic.  Errors are first-class values in the ADM and
may be caught and routed in band, landed ...
Use cases

the relational model of SQL databases with the document model of 
document databases

that promises an easier approach
to modern data because it unifies relational tables and eclectic JSON in a
powerful, new data model called [_super-structured data_](formats/model.md).
SuperDB's query language is a
[Pipe SQL](https://research.google/pubs/sql-has-problems-we-can-fix-them-pipe-syntax-in-sql/)
adapted for super-structured data called [_SuperSQL_](super-sql/intro.md),
which is aspirationally backward-compatible with
[PostgreSQL](https://www.postgresql.org/).

SuperDB has taken many of the best ideas of current data systems and adapted them
for super-structured data with the introduction of:

* the super-structured [data model](formats/model.md),
* several super-structured serialization [formats](formats/intro.md),
* a SQL-compatible [query language](super-sql/intro.md) adapted for super-structured data,
* a super-structured [query engine](https://github.com/superdb/super/tree/main/runtime), and
* a super-structured [database format](command/db.md#concepts) compatible with
  cloud object stores.

To achieve high performance for the dynamically typed data that lies at the heart of
super-structured data, SuperDB has devised a novel vectorized runtime
built as a _clean slate_ around [algebraic types](#enter-algebraic-types).
This contrasts with the Frankenstein approach taken by other analytics systems that
[shred variants](https://github.com/apache/parquet-format/blob/master/VariantShredding.md#value-shredding)
into relational columns.



_It's time for the relational model to become a special case of
an easier and broader approach for modern data._

## Super-structured Data

Super-structured data is strongly typed and self describing.
While compatible with relational schemas, SuperDB does not require such schemas
as they can be modeled as [super-structured records](formats/model.md#21-record).

More specifically, a _relational table_ in SuperDB is simply a collection of
uniformly typed records,
whereas a collection of dynamic but strongly-typed
data can model any sequence of JSON values, e.g., observability data,
application events, system logs, and so forth.

Thus, data in SuperDB is
* strongly typed like databases, but
* dynamically typed like JSON.

Self-describing data makes data easier: when transmitting data from one entity
to another, there is no need for the two sides to agree up front what the schemas
must be in order to communicate and land the data.

Likewise, when extracting and
serializing data from a query, there is never any loss of information as the
[super-structured formats](formats/intro.md)
capture all aspects of the strongly-typed data whether in
[human-readable form](formats/sup.md)
or
[binary row or columnar form](formats/bsup.md).

## The `super` Command

SuperDB is implemented as the standalone,
dependency-free [super](command/super.md) command.
`super` is a little like [DuckDB](https://duckdb.org/) and a little like
[jq](https://stedolan.github.io/jq/) but super-structured data ties these
two command styles together with strong typing of dynamic data.

Because `super` has no dependencies, it's easy to get going &mdash;
just [install the binary](getting-started/install.md) and you're off and running.

SuperDB separates compute and storage and is decomposed into
a runtime system that
* runs directly on any data inputs like files, streams, or APIs, or
* manipulates and queries data in a persistent
storage layer &mdash; the
[SuperDB database](command/db.md) &mdash; that rhymes in design with the emergent
[lakehouse pattern](https://www.cidrdb.org/cidr2021/papers/cidr2021_paper17.pdf)
but is based on super-structured data.

To invoke the SuperDB runtime without a database, just run `super` without
the [db](command/db.md) subcommand and specify an optional query with `-c`:
```sh
super -c "SELECT 'hello, world'"
```
To interact with a SuperDB database, invoke the [db](command/db.md) subcommands
of `super` and/or program against the database API.

>[!NOTE]
> The persistent database layer is still under development and not yet
> ready for turnkey production use.


## SuperSQL<span class="tm">®</span>

Since super-structured data is a superset of the relational model, it turns out that
a query language for super-structured data can be devised that is a superset of SQL.
The SuperDB query language is a
[Pipe SQL](https://research.google/pubs/sql-has-problems-we-can-fix-them-pipe-syntax-in-sql/)
adapted for super-structured data called
[_SuperSQL_](super-sql/intro.md).

SuperSQL is particularly well suited for data-wrangling use cases like
ETL and data exploration and discovery.
[Syntactic shortcuts](super-sql/operators/intro.md#shortcuts),
[keyword search](super-sql/operators/search.md), and the
[pipe syntax](super-sql/intro.md#pipe-queries)
make interactively querying data a breeze.  And language features like
[recursive functions](super-sql/expressions/subqueries.md#recursive-subqueries)
with re-entrant subqueries allow for traversing nested data
in a general and powerful fashion.

Instead of operating upon statically typed relational tables as SQL does,
SuperSQL operates upon super-structured data.
When such data happens to look like a table, then SuperSQL can work just
like SQL:
```sh
$ super -c "
SELECT a+b AS x, a-b AS y FROM (
  VALUES (1,2),	(3,0)
) AS T(a,b)
"
{x:3,y:-1}
{x:3,y:3}
```
But when data does not conform to the relational model, SuperSQL can
still handle it with its super-structured runtime:
```sh
$ super -c "
SELECT avg(radius) as R, avg(width) as W FROM (
  VALUES
    {kind:'circle',radius:1.5},
    {kind:'rect',width:2.0,height:1.0},
    {kind:'circle',radius:2},
    {kind:'rect',width:1.0,height:3.5}
)
"
{R:1.75,W:1.5}
```

Things get more interesting when you want to do different kinds of processing
for differently typed entities, e.g., let's compute an average radius of circles,
and double the width of each rectangle.  This time we'll use the pipe syntax
with shortcuts and employ first-class errors to flag unknown types:
```sh
$ super -c "
values
  {kind:'circle',radius:1.5},
  {kind:'rect',width:2.0,height:1.0},
  {kind:'circle',radius:2},
  {kind:'rect',width:1.0,height:3.5},
  {kind:'triangle',base:2.0,height:3.0}
| switch kind
    case 'circle' (
        R:=avg(radius)
    )
    case 'rect' (
        width:=width*2
    )
    default (
        error({message:'unknown shape',value:this})
    )
"
{R:1.75}
{kind:"rect",width:4.,height:1.}
{kind:"rect",width:2.,height:3.5}
error({message:"unknown shape",value:{kind:"triangle",base:2.,height:3.}})
```

So what's going on here?  The data model here is acting both
as a strongly typed representation of JSON-like sequences as well as a
means to represent relational tables.  And SuperSQL is behaving like SQL
when applied to table-like data, but at the same time is a
pipe-syntax language for arbitrarily typed data.
The super-structured data model ties it all together.

To make this all work, the runtime must handle arbitrarily typed data.  Hence, every
operator in SuperSQL has defined behavior for every possible input type.
This is the key point of departure for super-structured data: instead of the unit of processing being a relational table, which requires a statically defined schema, the unit of processing is a collection of arbitrarily typed values.
In a sense, SuperDB generalizes Codd's relational algebra to polymorphic operators.
All of Codd's relational operators can be recast in this fashion forming
the polymorphic algebra of super-structured data implemented by SuperDB.

## Evolving SQL

Despite SQL's enduring success,
it is widely accepted that there are serious flaws with the language.
Here are some noteworthy arguments that build the case:

* [A Critique of the SQL Database Language](https://dl.acm.org/doi/pdf/10.1145/984549.984551)
a 1983 paper by
[C.J. Date](https://en.wikipedia.org/wiki/Christopher_J._Date),
* [Against SQL](https://www.scattered-thoughts.net/writing/against-sql/)
by [Jamie Brandon](https://www.scattered-thoughts.net/),
* [A Critique of Modern SQL And A Proposal Towards A Simple and Expressive
Query Language](https://www.cidrdb.org/cidr2024/papers/p48-neumann.pdf)
by [Neumann](https://db.in.tum.de/~neumann/)
and [Leis](https://www.cs.cit.tum.de/dis/team/prof-dr-viktor-leis/).

These publications all argue that SQL is so problematic it should
be replaced outright. In contrast, the authors of
[SQL Has Problems. We Can Fix Them: Pipe Syntax In SQL](https://research.google/pubs/sql-has-problems-we-can-fix-them-pipe-syntax-in-sql/) propose that SQL
should be merely improved upon by creating a superset of SQL.
Except for compositional syntax, the paper says SQL is
perfectly reasonable and we should live with its anachronisms (see Section 2.4).
Thus, their
[Pipe SQL specification](https://github.com/google/zetasql/blob/master/docs/pipe-syntax.md)
carries forward SQL eccentricities into their modern adaptation
of pipes for SQL.

SuperSQL takes this approach of backward compatibility further
and seizes on the opportunity to introduce a language that is both
SQL compatible and also adapted to super-structured data.
In this way,
SuperSQL diverges significantly from SQL anachronisms in the pipe portion of
the language by introducing the concept of
[pipe scoping](super-sql/intro.md#pipe-scoping) while exploiting the
static typing of super-structured data
to simultaneously support [relational scoping](super-sql/intro.md#relational-scoping)
for SQL tables and columns.

The vision here is that comprehensive backward
compatibility can reside in the SQL operators while a modernized syntax and
and improved ergonomics can reside in the pipe operators, e.g.,

* array indexing [can be configured](super-sql/declarations/pragmas.md)
  as 1-based in SQL clauses but 0-based in pipe operators,
* column names in SQL clauses are case insensitive while record field references
are case sensitive in pipe operators,
* complex scoping rules for table aliases and column references are required in
relational SQL while binding from names to data in pipe operators is managed
in a uniform and simple way as dereferenced paths on [this](super-sql/intro.md#pipe-scoping),
* the syntactic structure of SQL clauses means all data must conform to a table
whereas pipe operators can emit any data type desired in a varying fashion, and
* [sum types](super-sql/types/union.md) are integral to piped data allowing mix-typed data processing
and results that need not fit in a uniform table.

> [!NOTE]
> Case insensitive column names in SQL clauses are not yet supported.
> ([super#6066](https://github.com/superdb/super/issues/6066))

With this approach, SuperSQL can be adopted and used for existing use cases
based on legacy SQL while incrementally expanding and embracing
the pipe model tied to super-structured data.
Perhaps this could enable a long-term and gradual transition away
from relational SQL toward a modern and more ergonomic replacement.

The jury is out as to whether a Pipe SQL for super-structured data is the
right approach for curing SQL's ills, but it certainly provides a framework
for exploring entirely new language abstractions while maintaining complete
backward compatibility with SQL all in the same query language.

_It's time for the relational model to become a special case of
an easier and broader approach for modern data._

> [!NOTE]
> If SuperDB or super-structured data has piqued your interest,
> you can dive deeper by:
> * exploring the [SuperSQL](super-sql/intro.md) query language, or
> * learning about the
> [super-structured data model](formats/model.md) and
  [formats](formats/intro.md) underlying SuperDB.
>
> We'd love your feedback and we hope to build a thriving community around
> SuperDB so please feel free to reach out to us via
> * [Slack](https://www.brimdata.io/join-slack/)
> * [GitHub issues](https://github.com/superdb/super/issues), or
> * [GitHub pull requests](https://github.com/superdb/super/pulls).
>
> See you online!
