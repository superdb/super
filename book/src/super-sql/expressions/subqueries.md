# Subqueries

A subquery is a [query](../queries.md) embedded in an [expression](intro.md).

When the expression containing the subquery is evaluated, the query is run with
an input consisting of a single value equal to the value being evaluated
by the expression.

The syntax for a subquery is simply a query in parentheses as in
```
( <query> )
```
where `<query>` is any query, e.g., the query
```
values {s:(values "hello, world" | upper(this))}
```
results in the value `{s:"HELLO, WORLD"}`.

Except for subqueries appearing as the right-hand side of
an [in](containment.md) operator, the result of a subquery must be a single value.
When multiple values are generated, an [error](../types/error.md) is produced.

For the [in](containment.md) operator, any subquery on the right-hand side is
always treated as an [array subquery](#array-subqueries), thus
providing compatibility with SQL syntax.

## Array Subqueries

When multiple values are expected, an array subquery can be used to group the
multi-valued result into a single-valued array.

The syntax for an array subquery is simply a query in square brackets as in
```
[ <query> ]
```
where `<query>` is any query, e.g., the query
```
values {a:[values 1,2,3 | values this+1]}
```
results in the value `{a:[2,3,4]}`.

An array subquery is shorthand for
```
( <query> | collect(this) )
```
e.g., the array subquery above could also be rewritten as
```
values {a:(values 1,2,3 | values this+1 | collect(this))}
```

## Independent Subqueries

A subquery that depends on its input as described above is called a _dependent subquery_.

When the subquery ignores its input value, e.g., when it begins with
a [from](../operators/from.md) operator, then the query is called an _independent subquery_.

For efficiency, the system materializes independent subqueries so that they are evaluated
just once.

For example, this query
```
let input = (values 1,2,3)
values 3,4
| values {that:this,count:(from input | count())}
```
evaluates the subquery `from input | count()` just once and materializes the result.
Then, for each input value `3` and `4`, the result is emitted, e.g.,
```
{that:3,count:3}
{that:4,count:3}
```

## Correlated Subqueries

When a subquery appears within a [SQL operator](../sql/intro.md),
[relational scope](../intro.md#relational-scoping) is active and references to table aliases and columns
may reach a scope that is outside of the subquery.
In this case, the subquery is a
[correlated subquery](https://en.wikipedia.org/wiki/Correlated_subquery).

> [!NOTE]
> Correlated subqueries are not yet supported.  They are detected and a
> compile-time error is reported when encountered.
> ([super#6549](https://github.com/superdb/super/issues/6549))

A correlated subquery can always be rewritten as a pipe subquery using
[unnest](../operators/unnest.md) using this pattern:
```
unnest {outer:this,inner:[<query>]}
```
where `<query>` generates the correlated subquery values, then they can
be accessed as if the `outer` field is the outer scope and the `inner` field
is the subquery scope.

## Named Subqueries

When a previously declared [named query](../declarations/queries.md)
is referenced in an expression, it is automatically evaluated as a subquery,
e.g.,
```
let q = (values 1,2,3 | max(this))
values q+1
```
outputs the value `4`.

When a named query is expected to return multiple values, it should be referenced
as an [array subquery](#array-subqueries), e.g.,
```
let q = (values 1,2,3)
values [q]
```
outputs the value `[1,2,3]`.

## Recursive Subqueries

When subqueries are combined with recursive invocation of the [function](../declarations/functions.md) they
appear in, some powerful patterns can be constructed.

For example, the [visitor-walk pattern](https://en.wikipedia.org/wiki/Visitor_pattern)
can be implemented using recursive subqueries and function values.

Here's a template for walk:
```
fn walk(node, visit):
  case kind(node)
  when "array" then
    [unnest node | walk(this, visit)]
  when "record" then
    unflatten([unnest flatten(node) | {path,value:walk(value, visit)}])
  when "union" then
    walk(under(node), visit)
  else visit(node)
  end
```

>[!NOTE]
> Since `flatten` traverses nested values all the way to their leaves,
> the record case alone could handle all of the nesting.  The recursion here
> illustrates re-entrant subqueries.

Here, `walk` is invoking an [array subquery](#array-subqueries) on the unnested
entities (records or arrays), calling the `walk` function recursively on each item,
then assembling the results back into an array (i.e., the raw result of the array subquery)
or a record (i.e., calling unflatten on the path/value pairs returned in the array).

If we call `walk` with this function on an arbitrary nested value
```
fn addOne(node): case typeof(node) when <int64> then node+1 else node end
```
then each leaf value of the nested value of type `int64` would be incremented
while the other leaves would be left alone.  See the example below.

## Examples

---

_Operate on arrays with values shortcuts and arrange answers into a record_

```mdtest-spq {data-layout="stacked"}
# spq
values {
    squares:[unnest this | this*this],
    roots:[unnest this | round(sqrt(this)*100)*0.01]
}
# input
[1,2,3]
[4,5]
# expected output
{squares:[1,4,9],roots:[1.,1.41,1.73]}
{squares:[16,25],roots:[2.,2.24]}
```

---

_Multi-valued subqueries emit an error_

```mdtest-spq {data-layout="stacked"}
# spq
values (values 1,2,3)
# input

# expected output
error("query expression produced multiple values (consider [subquery])")
```
---

_Multi-valued subqueries can be invoked as an array subquery_

```mdtest-spq
# spq
values [values 1,2,3]
# input

# expected output
[1,2,3]
```

---

_Right-hand side of "in" operator is always an array subquery_

```mdtest-spq
# spq
let data = (values {x:1},{x:2})
where this in (select x from data)
# input
1
2
3
# expected output
1
2
```

---

_Independent subqueries in SQL operators are supported while correlated subqueries are not_

```mdtest-spq
# spq
let input = (values {x:1},{x:2},{x:3})
select x
from input
where x >= (select avg(x) from input)
# input

# expected output
{x:2}
{x:3}
```

---

_Correlated subqueries in SQL operators not yet supported_

```mdtest-spq {data-layout="stacked"} fails
# spq
select x
from (values (1),(2)) a(x)
where exists (
  select 1
  from (values (3),(4)) b(y)
  where x=y
)
# input

# expected output
correlated subqueries not currently supported at line 6, column 9:
  where x=y
        ~
```

---

_Recursive subqueries inside function implementing the walk-visitor pattern_

```mdtest-spq
# spq
fn walk(node, visit):
  case kind(node)
  when "array" then
    [unnest node | walk(this, visit)]
  when "record" then
    unflatten([unnest flatten(node) | {path,value:walk(value, visit)}])
  when "union" then
    walk(under(node), visit)
  else visit(node)
  end
fn addOne(node): case typeof(node) when <int64> then node+1 else node end
values walk(this, &addOne)
# input
1
[1,2,3]
[{x:[1,"foo"]},{y:2}]
# expected output
2
[2,3,4]
[{x:[2,"foo"]},{y:3}]
```
