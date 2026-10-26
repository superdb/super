# SuperSQL<span class="tm">®</span>

SuperSQL<span class="tm">®</span> is a
[Pipe SQL](https://research.google/pubs/sql-has-problems-we-can-fix-them-pipe-syntax-in-sql/)
adapted for
[super-structured data](../formats/model.md).
The language is a superset of SQL query syntax and includes
a modern [type system](types/intro.md) with [sum types](types/union.md) to represent
heterogeneous data.

Similar to a Unix pipeline, a SuperSQL [query](queries.md)
is expressed as a data source followed by a number of
[operators](operators/intro.md) that manipulate the data:
```
from source | operator | operator | ...
```
As with SQL, SuperSQL is
[declarative](https://en.wikipedia.org/wiki/Declarative_programming)
and the SuperSQL compiler often optimizes a query into an implementation
different from the [dataflow](https://en.wikipedia.org/wiki/Dataflow) implied by the pipeline to achieve the
same semantics with better performance.

## Friendly Syntax

In addition to its user-friendly pipe syntax,
SuperSQL embraces two key design patterns that simplify
query editing for interactive usage:
* [shortcuts](operators/intro.md#shortcuts) that reduce
typing overhead and provide a concise syntax for common query patterns, and
* [search](operators/search.md)
reminiscent of Web or email keyword search, which is otherwise hard
to carry out with traditional SQL syntax.

With shortcuts, verbose queries can be typed in a shorthand facilitating
rapid data exploration.  For example, the query
```
SELECT count(), key
FROM source
GROUP BY key
```
can be simplified to
```
from source | count() by key
```

With search, all of the string fields in a value can easily be searched for
patterns, e.g., this query
```
from source
| ? example.com urgent message_length > 100
```
searches for the strings "example.com" and "urgent" in all of the string values in
the input and also includes a numeric comparison regarding the field `message_length`.

## SQL Compatibility

While SuperSQL at its core is a pipe-oriented language, it is also
[backward compatible](sql/intro.md) with relational SQL in that any
arbitrarily complex SQL query may appear as a single pipe operator
anywhere in a SuperSQL pipe query.

In other words, a single pipe operator that happens to be a standalone SQL query
is also a SuperSQL pipe query.
For example, these are all valid SuperSQL queries:
```
SELECT 'hello, world'
SELECT * FROM table
SELECT * FROM f1.json JOIN f2.json ON f1.id=f2.id
SELECT watchers FROM https://api.github.com/repos/superdb/super
```

## Pipe Queries

The entities that transform data within a SuperSQL pipeline are called
[pipe operators](operators/intro.md)
and take super-structured input from the upstream operator or data source,
operate upon the input, and produce zero or more super-structured
values as output.

Unlike relational SQL, SuperSQL pipe queries define their computation in terms of [dataflow](https://en.wikipedia.org/wiki/Dataflow)
through the directed graph of operators.  But instead of relational tables
propagating from one pipe operator to another
(e.g., as in
[ZetaSQL pipe syntax](https://github.com/google/zetasql/blob/master/docs/pipe-syntax.md#pipe-operator-semantics)),
any sequence of potentially heterogeneously typed data
may flow between SuperSQL pipe operators.

When a super-structured sequence conforms to a single, homogeneous
[record type](types/record.md),
then the data is equivalent to a SQL relation.
And because [any SQL query is also a valid pipe operator](sql/intro.md),
SuperSQL is thus a superset of SQL.
In particular, a single operator defined as pure SQL is an
acceptable SuperSQL query so all SQL query texts are also SuperSQL queries.

Unlike a Unix pipeline, a SuperSQL query can be forked and joined, e.g.,
```
from source
| operator
| fork
  ( operator | ... )
  ( operator | ... )
| join on condition
| ...
| switch expr
  case value ( operator | ... )
  case value ( operator | ... )
  default ( operator | ... )
| ...
```
A query can also include multiple data sources, e.g.,
```
fork
  ( from source1 | ... )
  ( from source2 | ... )
| ...
```

## Pipe Sources

Like SQL, input data for a pipe query is typically sourced with the
[from](operators/from.md) operator.

When `from` is not present, the file arguments to the
[super](../command/super.md) command are used as input to the query
as if there is an implied
`from` operator, e.g.,
```sh
super -c "op1 | op2 | ..." input.json
```
is equivalent to
```sh
super -c "from input.json | op1 | op2 | ..."
```
When neither
`from` nor file arguments are specified, a single `null` value
is provided as input to the query.
```sh
super -c "pass"
```
results in
```
null
```
This pattern provides a simple means to produce a constant input within a
query using the [values](operators/values.md) operator, wherein
`values` takes as input a single null and produces each constant
expression in turn, e.g.,
```sh
super -c "values 1,2,3"
```
results in
```
1
2
3
```

When running on the local file system,
`from` may refer to a file or an HTTP URL
indicating an API endpoint.
When connected to a [SuperDB database](../command/db.md),
`from` typically
refers to a collection of super-structured data in a table and
is referenced using the table's name as in SQL.

For more detail, see the reference page of the [from](operators/from.md) operator,
but as an example, you might use `from` to fetch data from an
HTTP endpoint and process it with `super`, in this case, to extract the description
and license of a GitHub repository:
```sh
super -f line -c "
from https://api.github.com/repos/superdb/super
| values description,license.name
"
```

## Relational Scoping

In SQL queries, data from tables is generally referenced with expressions that
specify a table name and a column name within that table,
e.g., referencing a column `x` in a table `T` as
```
SELECT T.x FROM (VALUES (1),(2),(3)) AS T(x)
```
More commonly, when the column name is unambiguous, the table name
can be omitted as in
```
SELECT x FROM (VALUES (1),(2),(3)) AS T(x)
```
When SQL queries are nested, joined, or invoked as subqueries, scoping
rules define how identifiers and dotted expressions resolve to the
different available table names and columns reachable via containing scopes.
To support such semantics, SuperSQL implements SQL scoping rules
_inside of any SQL pipe operator_ but not between pipe operators.

In other words, table aliases and column references all work within
a SQL query written as a single pipe operator but scoping of tables
and columns does not reach across pipe operators.  Likewise, a pipe query
embedded inside of a nested SQL query cannot access tables and columns in
the containing SQL scope.

## Pipe Scoping

For pipe queries, SuperSQL takes a different approach to scoping
called _pipe scoping_.

Here, a pipe operator takes any sequence of input values
and produces any computed sequence of output values and _all
data references are limited to these inputs and outputs_.
Since there is just one sequence of values, it may be
referenced as a special value with a special name, which for
SuperSQL is the value `this`.

The pipe scoping model can be summarized as follows:
* all input is referenced as a single value called `this`, and
* all output is emitted into a single value called `this`.

As mentioned above,
when processing a set of homogeneously-typed [records](types/record.md),
the data resembles a relational table where the record type resembles a
relational schema and each field in the record models the table's column.
In other words, the record fields of `this` can be accessed with the dot operator
reminiscent of a `table.column` reference in SQL.

For example, the SQL query from above can thus be written in pipe form
using the [values](operators/values.md) operator as:
```
values {x:1}, {x:2}, {x:3} | select this.x
```
which results in:
```
{x:1}
{x:2}
{x:3}
```
As with SQL table names, where `this` is implied, it is optional can be omitted, i.e.,
```
values {x:1}, {x:2}, {x:3} | select x
```
produces the same result.

Referencing `this` is often convenient, however, as in this query
```
values {x:1}, {x:2}, {x:3} | aggregate collect(this)
```
which collects each input value into an array and emits the array resulting in
```
[{x:1},{x:2},{x:3}]
```

### Combining Piped Data

If all data for all operators were always presented as a single input sequence
called `this`, then there would be no way to combine data from different entities,
which is otherwise a hallmark of SQL and the relational model.

To remedy this, SuperSQL extends pipe scoping to
[_joins_](#join-scoping) and
[_subqueries_](#subquery-scoping)
where multiple entities can be combined into the common value `this`.

#### Join Scoping

To combine joined entities into `this` via pipe scoping, the
[join](operators/join.md) operator
includes an _as clause_ that names the two sides of the join, e.g.,
```
... | join ( from ... ) as {left,right} | ...
```
Here, the joined values are formed into a new two-field record
whose first field is `left` and whose second field is `right` where the
`left` values come from the parent operator and the `right` values come
from the parenthesized join query argument.

For example, suppose the contents of a file `f1.json` is
```
{"x":1}
{"x":2}
{"x":3}
```
and `f2.json` is
```
{"y":4}
{"y":5}
```
then a `join` can bring these two entities together into a common record
which can then be subsequently operated upon, e.g.,
```
from f1.json
| cross join (from f2.json) as {f1,f2}
```
computes a cross-product over all the two sides of the join
and produces the following output
```
{f1:{x:1},f2:{y:4}}
{f1:{x:2},f2:{y:4}}
{f1:{x:3},f2:{y:4}}
{f1:{x:1},f2:{y:5}}
{f1:{x:2},f2:{y:5}}
{f1:{x:3},f2:{y:5}}
```
A downstream operator can then operate on these records,
for example, merging the two sides of the join using
spread operators (`...`), i.e.,
```
from f1.json
| cross join (from f2.json) as {f1,f2}
| values {...f1,...f2}
```
produces
```
{x:1,y:4}
{x:2,y:4}
{x:3,y:4}
{x:1,y:5}
{x:2,y:5}
{x:3,y:5}
```
In contrast, [relational scoping](#relational-scoping) in a `SELECT` clause
with the table source identified in `FROM` and `JOIN` clauses, e.g., this query
produces the same result:
```
SELECT f1.x, f2.y FROM f1.json as f1 CROSS JOIN f2.json as f2
```

#### Subquery Scoping

A subquery embedded in an expression can also combine data entities
via pipe scoping as in
```
from f1.json | values {outer:this,inner:[from f2.json | ...]}
```
Here data from the outer query can be mixed in with data from the
inner array subquery embedded in the expression inside of the
[values](operators/values.md) operator.

The array subquery produces an array value so it is often desirable to
[unnest](operators/unnest.md) this array with respect to the outer
values as in
```
from f1.json | unnest {outer:this,inner:[from f2.json | ...]} into ( <query> )
```
where `<query>` is an arbitrary pipe query that processes each
collection of unnested values separately as a unit for each outer value.
The `into ( <query> )` body is an optional component of `unnest`, and if absent,
the unnested collection boundaries are ignored and all of the unnested data is output as a combined sequence.

With the `unnest` operator, we can now consider how a [correlated subquery](https://en.wikipedia.org/wiki/Correlated_subquery) from
SQL can be implemented purely as a pipe query with pipe scoping.
For example,
```
SELECT (SELECT sum(f1.x+f2.y) FROM f1.json) AS s FROM f2.json
```
results in
```
{s:18}
{s:21}
```
To implement this with pipe scoping,
the correlated subquery is carried out by
unnesting the data from the subquery with the values coming from the outer
scope as in
```
from f2.json
| unnest {f2:this,f1:[from f1.json]} into ( s:=sum(f1.x+f2.y) )
```
giving the same result
```
{s:18}
{s:21}
```

## Type Checking

Data in SuperSQL is always strongly typed.

Like relational SQL, SuperSQL data sequences
can conform to a static schema that is type-checked at compile time.
And like
[document databases](https://en.wikipedia.org/wiki/Document-oriented_database)
and [SQL++](https://asterixdb.apache.org/files/SQL_Book.pdf),
data sequences may also be dynamically typed, but unlike these systems,
SuperSQL data is always strongly typed.

To perform type checking of dynamic data, SuperSQL utilizes a novel
approach based on [fused types](type-fusion.md).  Here, the compiler interrogates
the data sources for their fused type and uses these types (instead of
relational schemas) to perform type checking.  This is called _fused type checking_.

Because a relational schema is a special case of a fused type, fused type checking
works for both traditional SQL as well as for super-structured pipe queries.
These fused types are maintained in the super-structured database and the binary
forms of super-structured file formats provide efficient ways to retrieve their
fused type.

For traditional formats like JSON or CSV, computing the fused type requires
reading all of the data at compile time, so by default these inputs are
dynamically typed and type errors in queries over them are instead
runtime errors.  The `-static` flag causes the compiler to read such inputs
and compute their fused types on the fly.  See
[type checking of inputs](../command/input.md#type-checking) for the details.

In other words, dynamic data is statically type checked when possible.
This works by computing fused types of each operator's output and propagating
these types in a [dataflow analysis](https://en.wikipedia.org/wiki/Data-flow_analysis).  When types are unknown, the
analysis flexibly models them as having any possible type.

For example, this query produces the expected output
```sh
$ super -c "select b from (values (1,2),(3,4)) as T(b,c)"
{b:1}
{b:2}
```
But this query produced a compile-time error:
```sh
$ super -c "select a from (values (1,2),(3,4)) as T(b,c)"
column "a": does not exist at line 1, column 8:
select a from (values (1,2),(3,4)) as T(b,c)
       ~
```
Now supposing this data is in the file `input.json`:
```
{"b":1,"c":2}
{"b":3,"c":4}
```
If we run the query from above without type checking data from the source
(the default for JSON, or for any input with the `-dynamic` flag), then the
query runs even though there are type errors.  In this case, dynamic type
errors turn into SQL NULL for SQL queries:
```
$ super -dynamic -c "select a from input.json"
{a:null}
{a:null}
```
Even though the reference to column "a" is dynamically evaluated, all
the data is still strongly typed, i.e.,
 ```sh
$ super -c "from input.json | values typeof(this)"
<{b:int64,c:int64}>
<{b:int64,c:int64}>
```

## Data Order

Data sequences from sources may have a natural order.  For example,
the values in a file being read are presumed to have the order they
appear in the file.  Likewise, data stored in a database organized by
a sort constraint is presumed to have the sorted order.

For _order-preserving_ pipe operators, this order is preserved.
For _order-creating_ operators like [sort](operators/sort.md)
an output order is created independent of the input order.
For other operators, the output order is undefined.

The top of the documentation page for each [operator](operators/intro.md)
is marked with an icon to describe its data order behavior:

|Marker|Data Order behavior    |
|:----:|:----------------------|
| ✅   | Order-preserving      |
| 🔀   | Order-creating        |
| 🎲   | Undefined output order|

For example, [where](operators/where.md) drops values that do
not meet the operator's condition but otherwise preserves data order,
whereas [sort](operators/sort.md) creates an output order defined
by the sort expressions.  The [aggregate](operators/aggregate.md) operator
creates an undefined order at output.

When a pipe query branches as in
[join](operators/join.md),
[fork](operators/fork.md), or
[switch](operators/switch.md),
the order at the merged branches is undefined.
