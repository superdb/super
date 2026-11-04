# values

[✅](../intro.md#data-order)&ensp; emit values from expressions

## Synopsis

```
[values] <expr> [, <expr>...]
```

## Description

The `values` operator produces output values by evaluating one or more
comma-separated
expressions on each input value and sending each result to the output
in left-to-right order.  Each `<expr>` may be any valid
[expression](../expressions/intro.md).

The input order convolved with left-to-right evaluation order is preserved
at the output.

A result that is a plain `none`, e.g., from [ok](../functions/generics/ok.md),
`?.`, a [type assertion](../expressions/type-assertions.md) that fails, or a
[match](../expressions/match.md) with no matching case and no `else`,
is dropped, so a `none` is never passed to a downstream operator.
To output another value in its place, use the `??` operator.
An option's `none` (e.g., `none::int64`) is a value and is output as is.

The `values` operator name is optional since it can be used as a
[shortcut](intro.md#shortcuts).  When used as a shortcut, only one expression
may be present.

The `values` abstraction is also available as the SQL [`VALUES`](../sql/values.md) clause,
where the tuples that comprise
this form must all adhere to a common type signature.

The pipe form of `values` here is differentiated from the SQL form
by the absence of parenthesized expressions in the comma-separated list
of expressions, i.e., the expressions in a comma-separated list never
require top-level parentheses and the resulting values need not conform
to a common type.

The `values` operator is a _go to_ tool in SuperSQL queries as it allows
the flexible creation of arbitrary values from its inputs while the
SQL `VALUES` clause is a _go to_ building block for creating constant tables
to insert into or operate upon a database.  That said, the SQL `VALUES` clause
can also be comprised of dynamic expressions though it is less often used
in this fashion.  Nonetheless, this motivated the naming of the more general
SuperSQL `values` operator.

For example, this query uses SQL `VALUES` to
create a static table called _points_ then operate upon
each row of _points_ using expressions embodied in
dynamic `VALUES` subqueries placed in a lateral join as follows:
```
WITH points(x,y) AS (
  VALUES (2,1),(4,2),(6,3)
)
SELECT vals
FROM points CROSS JOIN LATERAL (VALUES (x+y), (x-y)) t(vals)
```
which produces
```
3
1
6
2
9
3
```
Using the `values` pipe operator, this can be written simply as
```
values {x:2,y:1},{x:4,y:2},{x:6,y:3}
| values x+y, x-y
```

## Examples

---

_Hello, world_
```mdtest-spq
# spq
values "hello, world"
# input

# expected output
"hello, world"
```

---

_Values evaluates each expression for every input value_
```mdtest-spq
# spq
values 1,2
# input
null
null
null
# expected output
1
2
1
2
1
2
```

---

_Values typically operates on its input_
```mdtest-spq
# spq
values this*2+1
# input
1
2
3
# expected output
3
5
7
```

---

_Values is often used to transform records_
```mdtest-spq
# spq
values [a,b],[b,a] | collect(this)
# input
{a:1,b:2}
# expected output
[[1,2],[2,1]]
```

---

_A plain none is dropped unless it's replaced with ??_
```mdtest-spq
# spq
values ok(a), ok(a) ?? 0
# input
{a:1}
{b:2}
# expected output
1
1
0
```
