# Values, Not Columns

_November 2026_

If you've read the other posts here, you've seen the same operator turn up
in query after query:
```
from shapes.bsup | values dimensions.radius.ok()
```
New users ask about it right away.  What is `values`, and why isn't that
just a `SELECT`?

The short answer is that `values` is what `SELECT` turns into when a
result doesn't have to be a table.  It's the workhorse of SuperSQL pipe
queries, and this post explains why.

## What SELECT Does

`SELECT` projects columns.  Its input is a table and its output is a table,
so every result is a row and every expression in the list is a column of
that row.
```mdtest-spq
# spq
SELECT id, type
# input
{"id":1,"type":"Rectangle","dimensions":{"width":2,"height":1}}
{"id":2,"type":"Circle","dimensions":{"radius":3}}
# expected output
{id:1,type:"Rectangle"}
{id:2,type:"Circle"}
```
Ask for just one thing and you still get a row:
```mdtest-spq
# spq
SELECT id
# input
{"id":1,"type":"Rectangle","dimensions":{"width":2,"height":1}}
{"id":2,"type":"Circle","dimensions":{"radius":3}}
# expected output
{id:1}
{id:2}
```
And because every expression is a column, every expression needs a name.
When you don't supply one, SQL has to make one up:
```mdtest-spq
# spq
SELECT type, type
# input
{"id":1,"type":"Rectangle","dimensions":{"width":2,"height":1}}
{"id":2,"type":"Circle","dimensions":{"radius":3}}
# expected output
{type:"Rectangle",type_1:"Rectangle"}
{type:"Circle",type_1:"Circle"}
```
All of this is exactly right for the relational model, where a result
that isn't a row has nowhere to go.

## What values Does

In the [algebraic data model](../adm.md), a result can be any value, so
there's no need for the idea of projecting columns.  The
[values](../super-sql/operators/values.md) operator just evaluates an
expression on each input value and emits the result, whatever it is.

It can be a number:
```mdtest-spq
# spq
values id
# input
{"id":1,"type":"Rectangle","dimensions":{"width":2,"height":1}}
{"id":2,"type":"Circle","dimensions":{"radius":3}}
# expected output
1
2
```
It can be something different for every input:
```mdtest-spq
# spq
values dimensions
# input
{"id":1,"type":"Rectangle","dimensions":{"width":2,"height":1}}
{"id":2,"type":"Circle","dimensions":{"radius":3}}
{"id":5,"type":"Vector","dimensions":[1,2,3,2,1]}
# expected output
{width:2,height:1}
{radius:3}
[1,2,3,2,1]
```
That's three values of three different types.  There is no table here and
no need for one.

When you do want a record, you build one with a
[record expression](../super-sql/types/record.md).  The record is
something you ask for, not something the operator imposes:
```mdtest-spq
# spq
values {id, kind:type}
# input
{"id":1,"type":"Rectangle","dimensions":{"width":2,"height":1}}
{"id":2,"type":"Circle","dimensions":{"radius":3}}
# expected output
{id:1,kind:"Rectangle"}
{id:2,kind:"Circle"}
```
You can give it several expressions, and each one is its own output value
rather than a column of a shared row:
```mdtest-spq
# spq
values id, type
# input
{"id":1,"type":"Rectangle","dimensions":{"width":2,"height":1}}
{"id":2,"type":"Circle","dimensions":{"radius":3}}
# expected output
1
"Rectangle"
2
"Circle"
```
And when an expression has nothing to say for some input, `values` says
nothing.  Here [ok](../super-sql/functions/generics/ok.md) turns a missing
`radius` into `none`, which `values` drops instead of padding the output
with a `NULL`:
```mdtest-spq
# spq
values dimensions.radius.ok()
# input
{"id":1,"type":"Rectangle","dimensions":{"width":2,"height":1}}
{"id":2,"type":"Circle","dimensions":{"radius":3}}
{"id":5,"type":"Vector","dimensions":[1,2,3,2,1]}
# expected output
3
```

## Subqueries

An expression can contain a
[subquery](../super-sql/expressions/subqueries.md), so `values` can run a
whole query for each input value and put the answer wherever it's wanted.
A subquery in parentheses produces a single value, and an _array subquery_
in square brackets collects many values into an array:
```mdtest-spq {data-layout="stacked"}
# spq
values {
  id,
  total: (unnest dimensions | sum(this)),
  doubled: [unnest dimensions | values this*2]
}
# input
{"id":5,"type":"Vector","dimensions":[1,2,3,2,1]}
# expected output
{id:5,total:9,doubled:[2,4,6,4,2]}
```
In SQL, running a query for each row takes a lateral join, and the result
has to be flattened back into the columns of the outer row.  Here it's
just an expression inside a record.

## Where the Name Comes From

SQL has had a `VALUES` clause all along.  It makes a table out of literal
rows, typically to insert into a table or to stand in for one:
```mdtest-spq
# spq
VALUES ('hello, world')
# input

# expected output
{column1:"hello, world"}
```
Each parenthesized tuple is a row, the rows must all have the same type,
and since a row needs column names, they are called `column1`, `column2`,
and so on.

SQL's `VALUES` can be used with expressions that aren't constants, though
it rarely is.  Here is a query that makes a table of points with `VALUES`
and then uses a second `VALUES` in a lateral join to compute the sum and
difference of each point's coordinates:
```sql
WITH points(x,y) AS (
  VALUES (2,1),(4,2),(6,3)
)
SELECT vals
FROM points CROSS JOIN LATERAL (VALUES (x+y), (x-y)) t(vals)
```
The pipe operator is named for this clause because it's the same idea
with the constraints removed.  There are no tuples, no shared type, and
no made-up column names:
```mdtest-spq
# spq
values {x:2,y:1},{x:4,y:2},{x:6,y:3}
| values x+y, x-y
# input

# expected output
3
1
6
2
9
3
```

## SELECT VALUE

We weren't the first to want this.  The designers of
[SQL++](https://arxiv.org/pdf/1405.3631) ran into the same wall when they
extended SQL to JSON, and their answer was `SELECT VALUE` (originally
`SELECT ELEMENT`), which outputs the value of an expression as is:
```sql
SELECT VALUE c.name FROM customers AS c
```
This returns names, not rows that each have a `name` column.  In SQL++,
`SELECT VALUE` is the core construct, and SQL's plain `SELECT` is defined
as syntactic sugar for the special case where each value is a tuple.
[PartiQL](https://partiql.org/dql/select.html) does the same.

That was the right insight.  SuperSQL arrived at the same idea on its own
path: the operator was originally called `yield`, and we renamed it `values`
to line up with SQL and SQL++.  And it takes the idea one step further.
Rather than a variation on `SELECT` that lives inside a query block, `values`
is an operator of its own that can go anywhere in a pipeline, as many times
as you like:
```
from customers | values name
```

## The Workhorse

Once results don't have to be rows, a lot of what a query does comes down
to "for each input value, emit this", and that's `values`.  In the other
posts you'll see it used to
* pick fields out of a value, as in `values dimensions.radius.ok()`,
* build new records, as in `values {...left, color:right.color}` after a join,
* keep one variant of a sum type with a
  [type assertion](../super-sql/expressions/type-assertions.md),
  as in `values this.(PushEvent)`, and
* [cast](../super-sql/expressions/cast.md) values to a type, as in
  `values this::Event`.

It is used so often that the keyword is optional.  An expression by itself
is a [shortcut](../super-sql/operators/intro.md#shortcuts) for `values`,
so `{id, kind:type}` is a complete query.

None of this takes `SELECT` away.  SuperSQL is (aspirationally) backward
compatible with SQL, and when a table is what you want, `SELECT` is still
the natural way to ask for one.  But a table is now a special case, and
`values` is how you ask for everything else.

If you would like more details, check out the documentation for the
[values operator](../super-sql/operators/values.md).
