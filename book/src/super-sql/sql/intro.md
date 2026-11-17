# SQL

SuperSQL is backward compatible with SQL in that any SQL query
is a SuperSQL [pipe operator](../operators/intro.md).
A SQL query used as a pipe operator in SuperSQL is called a _SQL operator_.
SQL operators can also be used recursively inside of a SQL operation
as [defined below](#sql-body).

## SQL Operator

A SQL operator is a query having the form of a `<sql-op>` defined as
```
[ <with> ]
<sql-body>
[ <order-by> ]
[ <limit> ]
```
where
* `<with>` is an optional [WITH](with.md) clause containing one or more
   comma-separated common table-expressions (CTEs),
* `<sql-body>` is a recursively defined query structure as [defined below](#sql-body),
* `<order-by>` is an optional list of one or more sort expressions
   in an [ORDER BY](order-by.md) clause, and
* `<limit>` is a [LIMIT](limit.md) clause constraining the number of rows in the output.

A SQL operator produces relational data in the form of sets of records
and may appear in several contexts including:
* the top-level query,
* as a parenthesized data-source element of a [FROM](from.md) clause,
* as a data-source element of [JOIN](join.md) clause embedded in a
  [FROM](from.md) clause,
* as a [subquery](../expressions/subqueries.md) in expressions, and
* as operands in a [set operation](set-ops.md).

The optional `<with>` component creates named SQL queries that are available
to any [FROM](from.md) clause contained (directly or recursively) within the
`<sql-body>`.

The output of the `<sql-body>` may be optionally sorted by the
`<order-by>` component and limited in length by the `<limit>` component.

Note that all of the elements of a `<sql-op>` are optional except the
`<sql-body>`.  Thus, any form of a simple `<sql-body>` may appear
anywhere a `<sql-op>` may appear.

> [!NOTE]
> The `WINDOW` clause is not yet available in SuperSQL.

## SQL Body

A `<sql-body>` component has one of the following forms:
* a [SELECT](select.md) clause,
* a [VALUES](values.md) clause,
* a [set operation](set-ops.md), or
* a parenthesized query of the form `( <sql-op> )`.

Thus, a `<sql-body>` must include either a `SELECT` or `VALUES` component
as its foundation, i.e., a `<sql-body>` at core is either a `SELECT` or
`VALUES` query.
Then, this core query may be combined in optionally parenthesized
set operations involving other `<sql-body>` or `<sql-op>`
components.

## Table Structure

Relational tables in SuperSQL are modeled as a sequence of records with
a uniform type.  With input in this form, standard SQL syntax may define
a table alias that references an input sequence so that the fields of the
record type then correspond to relational columns.

When the record type of the input data is known, the SuperSQL treats
it as a relational schema thereby enabling familiar SQL concepts like
static type checking and unqualified column resolution.

However, SuperSQL also allows for non-record data as well as data
whose type is unknown at compile time (e.g., large JSON files that are not
parsed for their type information prior to compilation).
A table reference to input data who type is unknown is called
a _dynamic table_.

Dynamic tables pose a challenge to traditional SQL semantics because
the compiler cannot know the columns that comprise the input and thus
cannot resolve a column reference to a dynamic table.  Also, static
type checking as in traditional SQL cannot be carried out.

To remedy this, SuperSQL supports dynamic tables in SQL operators
but restrict how they may be used as described below.

> [!NOTE]
> The restrictions on dynamic tables avoid a situation where the
> semantics of a query is dependent on whether the input type is known.
> If column bindings were to change when the input type goes from
> unknown to known, then the semantics of the query would change simply
> because type information happened to be known.
> The constraints on dynamic tables are imposed to avoid this pitfall.

When SQL operators encounter data that is not in table form,
errors typically arise, e.g., compile-time errors indicating a query
referencing a non-existent column or, in the case of a dynamic table,
runtime errors indicating `error("missing")`.

> [!NOTE]
> When querying highly heterogeneous data (e.g., JSON events),
> it is typically preferable to use [pipe operators](../operators/intro.md)
> on arbitrary data instead of SQL queries on tables.

## Relational Scopes

Identifiers that appear in SQL expressions are resolved in accordance
with the relational model, typically referring to tables and columns and by name.

Each SQL pipe operator defines one or more relational namespaces
that are independent of other SQL pipe operators and does not span across
pipe operator boundaries.  A set of columns (from one or more tables)
comprising such a namespace is called a _relational scope_.

A [FROM](from.md) clause creates a relational scope defined by a
namespace comprising one or more table names each containing
one or more column names from the top-level tables that
appear in the `FROM` body.

A [VALUES](from.md) clause creates a relational scope defined by
the default column names `column1`, `column2`, etc. and typically appears as
a table expression in a `FROM` clause with a table alias to rename
the columns.

Table names and column names do not need to be unique but when non-unique
names cause ambiguous references, then errors are reported and the query
fails to compile.

A particular column is referenced by name using the syntax
```
<column>
```
or
```
<table> . <column>
```
where `<table>` and `<column>` are identifiers.
The first form is called an _unqualified column reference_ while the
second form is called a _qualified column reference_.

>[!NOTE]
> The `.` operator here is overloaded as it may (1) indicate
> a column inside of a table or (2)
> [dereference a record value](../expressions/dot.md).

A table referenced without a column qualifier, as in
```
<table>
```
is simply called a _table reference_.  Table references within expressions
result in values that comprise the entire row of the table as a record.

## Input Scope

A relational scope defined by the optional [FROM](from.md) clause is called
an _input scope_.

An input scope is comprised of the table and constituent column
that `FROM` defines,
which may in turn contain [JOIN](join.md) clauses and additional tables
and columns.
Any of the tables defined in subqueries embedded in the `FROM` clause
are not part of the input scope and thus not visible.

For example, this query
```
SELECT *
FROM (VALUES (1),(2)) T(x)
CROSS JOIN (VALUES (3,4),(5,6)) U(y,z)
```
creates an input scope with columns `T.x`, `U.y`, and `U.z`.

## Output Scope

A relational scope defined by a SQL operator that is not a
[SELECT](select.md) operation &mdash; i.e., set operations or a
[VALUES](values.md) clause &mdash; is called an _output scope_.

An output scope does not have a table name and is an anonymous
scope for which only unqualified column references apply.

When an output scope appears as a table subquery within a
[FROM](from.md) clause, the output scope may be named with
a [table alias](from.md#table-aliases) and becomes
part of the input scope for the `SELECT` operation in which it appears.

## Relational Bindings

While identifiers in SQL expressions typically resolve to columns in table,
they may also refer to lexically-scoped
[declarations](../declarations/intro.md) for constants, named queries,
and so forth.  These bindings have a precedence higher than relational
bindings so an identifier is first resolved via
[lexical binding](../expressions/intro.md#identifier-resolution).

When an identifier does not resolve to a declaration in a lexical scope,
then it is resolved as a table or column reference from
the [input scope](#input-scope) or to a
[column alias](#column-aliases).

The relational identifiers are bound to a table, column, or input expression
as follows:
* when [alias-first resolution](#column-aliases) is in effect: if the
  identifier matches a column alias,
  then the identifier is substituted with the corresponding column's
  input expression;
* if the identifier resolves as an
  [unqualified reference](#unqualified-references)
  without error, then the identifier binds to that column;
* if the identifier resolves as an unqualified reference with an ambiguous column
  error, then the error is reported and the query fails to compile;
* when alias resolution is in effect (but column-first is not in effect):
  if the identifier matches a column alias,
  then the identifier is substituted with the corresponding column's
  input expression;
* when the identifier is a candidate for a
  [qualified reference](#qualified-references)
  (i.e., the identifier is followed by a `.` and a second identifier):
  * if the identifier pair resolves as an unqualified reference without error,
    then the pair binds to that column;
  * if the identifier pair resolves as an unqualified reference with an
    ambiguous column error, then the error is reported and the query fails
    to compile;
* when the identifier is not a candidate for a qualified reference:
  * if the identifier resolves as a table reference without error,
    then the identifier binds to that table;
  * if the identifier resolves as a table reference with an ambiguous table
    error, then the error is reported and the query fails to compile.

If no such matches are found, then an error is reported indicating
a non-existent table or column reference and the query fails to compile.

### Unqualified References

An unqualified reference of the form `<column>` is resolved by
searching the input scope over all columns where the identifier
and `<column>` match:
* if there is exactly one match, then the identifier binds to that column;
* if there is more than one match, then an error is reported indicating
  an ambiguous column reference and the query fails to compile;
* if there is no match, then the resolution fails without error.

When there are multiple tables in scope and at least one of the tables
is dynamic, then unqualified references are not allowed.  In this case,
an error is reported and the query fails to compile.

### Qualified References

A qualified reference of the form `<table> . <column>` is
resolved by searching the input scope over all tables that match
`<table>` and where the table contains a column matching `<column>`:
* if there is exactly one match, then the identifier binds to that column;
* if there is more than one match, then an error is reported indicating
  an ambiguous column reference and the query fails to compile;
* if there is no match, then the resolution fails without error.

For dynamic tables, all qualified references bind to any column name and
runtime errors are generated when referencing columns that do not exist
in the dynamic table.

### Table References

A table reference of the form `<table>` is resolved by searching
the input scope over all tables that match `<table>`:
* if there is exactly one match, then the identifier binds to that table;
* if there is more than one match, then an error is reported indicating
  an ambiguous table reference and the query fails to compile;
* if there is no match, then the resolution fails without error.

Table references for dynamic tables are not allowed.  In this case,
an error is reported and the query fails to compile.

### Column Aliases

Depending on the particular clause, column aliases
may be referenced in expressions.

For expressions in `GROUP BY` and `ORDER BY` clauses
(and when pragma `pg` is false):
* if the identifier matches a column alias, then the corresponding expression
  is substituted in place of the identifier,
* otherwise, the identifier is resolved to a table or column reference from
  the input scope as described [previously](#relational-bindings).

When pragma `pg` is true, the column alias check is performed _after_ the
input scope but only for `GROUP BY` expressions, which is
in line with PostgreSQL semantics.

For expressions in an `ORDER BY` clause, the column alias check
is always performed _before_ the input scope check independent of
the pragma `pg` setting.

For expressions in a `WHERE` clause, the identifier is resolved using
only the input scope, i.e., column aliases are not allowed.

## `this`

When querying dynamic tables, the `*` selector for all columns is not
available as the input columns are unknown.  Also, the input is not guaranteed
to be relational.

To remedy this, SuperSQL allows `this` to be referenced in expressions,
which resolves to the input row for `WHERE`, `GROUP BY` and the projected
expressions and resolves the output row for `HAVING` and `ORDER BY`.

For example, the `SELECT` statement here, places `this` into a
first [column called that](../types/record.md#derived-field-names)
thereby producing a relational output:
```mdtest-spq {data-layout='no-labels&stacked'}
# spq
values 1, 'foo', {x:1}
| SELECT this
# input

# expected output
{that:1}
{that:"foo"}
{that:{x:1}}
```

## SQL Compatibility

With so many [incompatible SQL dialects in existence](https://youtu.be/TBAf5l1RmcA),
SuperSQL's goal is to be (mostly) compatible with the
the [PostgreSQL](https://www.postgresql.org/) dialect.

In addition to SQL tests in the
[SuperDB GitHub repository](https://github.com/superdb/super),
PostgreSQL compatibility is tested using
[sqllogictest](https://sqlite.org/sqllogictest/doc/trunk/about.wiki) queries
from [SQLite](https://sqlite.org/).

As of the SuperDB commit `36cb48a`, 3,615,386 of 3,619,718 eligible queries
(99.88% success rate) from the SQLite set produce the same result as
PostgreSQL when run via `super`. The following open issues are known to be a
cause of one or more of the 0.12% remaining unsuccessful queries and will be
addressed in future SuperDB releases.

|**Issue**|**Description**|
|---------|----------------|
|[super#6549](https://github.com/superdb/super/issues/6549)|SQL: Correlated subqueries|
|[super#6033](https://github.com/superdb/super/issues/6033)|SQL: INTERSECT and EXCEPT|
|[super#6074](https://github.com/superdb/super/issues/6074)|SQL: Large cartesian product causes very long query runtime|
|[super#6536](https://github.com/superdb/super/issues/6536)|SQL: Signed zero|
|[super#6517](https://github.com/superdb/super/issues/6517)|SQL: Promoting return types to common type of arguments|
|[super#7347](https://github.com/superdb/super/issues/7347)|SQL: CAST of a non-integer to INTEGER truncates instead of rounding|

Additional details on how the tests are assembled and executed can be found in
the [sqllogic-ztests repo](https://github.com/superdb/sqllogic-ztests).
