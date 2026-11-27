# FROM

The `FROM` clause of a [SELECT](select.md) has the form
```
FROM <table-expr>
```
where a `<table-expr>` represents data sources (like files,
API endpoints, or database tables), table subqueries, pipe queries,
or joins.

## Table Expressions

A table expression `<table-expr>` has one of the following forms:

```
<entity> [ ( <options> ) ] [ <as> ]
<named-query> [ <as> ]
( <query> ) [ <as> ]
<join-expr>
( <table-expr> )
```

`<entity>` is defined as in the pipe form of [from](../operators/from.md), namely one of
* a [text entity](../queries.md#text-entity) representing a file, URL, or table,
* an [f-string](../expressions/f-strings.md) representing a file, URL, or table,
* a [glob](../queries.md#glob) matching files in the local file system or tables in a database, or
* a [regular expression](../queries.md#regular-expression) matching tables

`<options>` are the [entity options](../operators/from.md#options)
   as in pipe `from`.

`<named-query>` is the name of a common-table expression (CTE)
defined in a [WITH](with.md) clause or a
[declared query](../declarations/queries.md).

`<query>` is any [query](../queries.md) inclusive of
[SQL operators](intro.md#sql-operator)
or [pipe operators](../operators/intro.md).

`<join-expr>` is any [JOIN](join.md) operation, which is defined to
recursively operate upon any `<table-expr>` defined here.

Any `<table-expr>` may be parenthesized to control precedence
and evaluation order.

## Table Aliases

The table expressions above that represent data-source entities
and table subqueries may be bound to a table alias
with the option `<as>` clause of the form
```
[ AS ] <alias>
```
where the `AS` keyword is optional and `<alias>` has the form
```
<table> [ ( <column> [ , <column> ... ] ) ]
```
`<table>` and `<column>` are [identifiers](../queries.md#identifiers)
naming a table or a table and the columns of the indicated table
and an optional parenthesized list of columns positionally specifies the
column names of that table.

Joined expression and parenthesized table expressions cannot be assigned
aliases as the [relational scope](intro.md#relational-scopes)
produced by such expression is comprised of their constituent table names
and columns.

## Input Table

A `FROM` clause is a component of [SELECT](select.md) that
identifies the query's input data to create the _input table_
for `SELECT`.

The input table is accessed via a namespace comprised of
table and column [references](intro.md#relational-bindings)
that may then appear in the various expressions appearing throughout
the query.

This namespace is called a [relational scope](intro.md#relational-scopes)
and the `FROM` clause creates the [input scope](intro.md#input-scope)
for `SELECT`.

The name space consists of the table names and aliases (and their
constituent columns) created by the initial `FROM` clause and
any [JOIN](join.md) clauses that appear.  Any tables that are defined
in table subqueries in the `FROM` clause are not part of the
input scope.

>[!NOTE]
> The SQL `FROM` clause is similar to the pipe form of the
> [from](../operators/from.md) operator but
> * uses [relational scoping](../intro.md#relational-scoping) instead of
>   [pipe scoping](../intro.md#pipe-scoping),
> * allows the binding of table aliases to relational data sources, and
> * can be combined with [JOIN](join.md) clauses to implement relational joins.

## File Examples

---

_Source structured data from a local file_

```mdtest-command
echo '{"greeting":"hello world!"}' > hello.json
super -s -c 'SELECT greeting FROM hello.json'
```
```mdtest-output
{greeting:"hello world!"}
```

---

_Translate some CSV into Parquet and query it_
```mdtest-command
echo 'Name,Email,Phone Number,Address
John Doe,john.doe@example.com,123-555-1234,"123 Example Address, City, State"
Jane Smith,jane.smith@example.com,123-555-5678,"456 Another Lane, Town, State"' > example.csv
super -o example.parquet example.csv
super -s -c 'SELECT collect("Phone Number") as numbers FROM example.parquet'
```
```mdtest-output
{numbers:["123-555-1234","123-555-5678"]}
```

---

## HTTP Example

---

_Source data from a URL_
```
super -s -c "SELECT name FROM https://api.github.com/repos/superdb/super"
```
```
{name:"super"}
```

---

### F-String Example

---

_Read from dynamically defined files and add a column_

```mdtest-command
echo '{a:1}{a:2}' > a.sup
echo '{b:3}{b:4}' > b.sup
echo '"a.sup" "b.sup"' | super -s -c "
SELECT this, coalesce(a,b)+1 AS c
FROM f'{this}'
" -
```
```mdtest-output
{that:{a:1},c:2}
{that:{a:2},c:3}
{that:{b:3},c:4}
{that:{b:4},c:5}
```

---

## Database Examples

---

_Query a database table with SQL_

```mdtest-command
super db init -q db
super db attach -q db
super db -c "
  create table t (a int64, b string);
  insert into t values (1, 'x'), (2, 'y')
" > /dev/null
super db -s -c "SELECT b, a+1 AS c FROM t WHERE a > 1"
```
```mdtest-output
{b:"y",c:3}
```

---
