# Pragmas

Pragmas control various language features and appear in a declaration block
so their effect is lexically scoped.  They have the form
```
pragma <id> [ = <expr> ]
```
where `<id>` is an [identifier](../queries.md#identifiers)
and `<expr>` is a constant [expression](../expressions/intro.md)
that must evaluate at compile time without referencing any
runtime state such as [this](../intro.md#pipe-scoping) or a field of `this`.

If `<expr>` is omitted, it defaults to `true`.

Pragmas must appear in the declaration section of a [scope](../queries.md#scope).

## List of Pragmas

Currently, there are three supported pragmas.

* `catch` - puts a [catch](../operators/catch.md) at the output of every
    operator in the pragma's scope and makes its handler the default handler of
    [side errors](../operators/catch.md#catch-clauses).  The value is the
    handler, which is a query in parentheses or one of `halt`, `ignore`, or
    `pass`, and if it's omitted, the handler is `halt`, so `pragma catch` stops
    the query at the first error anywhere in it.  The handlers of catches don't
    get the pragma's catch.
* `index_base` - controls whether [index expressions](../expressions/index.md) and
    [slice expressions](../expressions/slices.md) are 0-based or 1-based.
    * `0` for zero-based indexing
    * `1` for one-based indexing
* `pg` - controls the precedence of scoping for GROUP BY clauses in SQL operators
    * `false` to follow Google SQL semantics of resolving identifiers first from column aliases then from the input table
    * `true` to follow PostgreSQL semantics of resolving identifiers first from the input table then from the column aliases

## Example

---

_Controlling indexing and slicing_

```mdtest-spq
# spq
pragma index_base = 1
values {
  a: this[2:3],
  b: (
    pragma index_base = 0
    values this[0]
  )
}
# input
"bar"
[1,2,3]
# expected output
{a:"a",b:error({message:"value cannot be indexed",on:"bar"})}
{a:[2],b:1}
```

---

_Use default `GROUP BY` precedence (no `pragma`) to group by the alias "x" that refers to constant 0_

```mdtest-spq
# spq
SELECT 0 AS x, COUNT(*) AS n GROUP BY x
# input
{x:1}
{x:2}
{x:1}
{x:2}
# expected output
{x:0,n:4}
```

---

_Use PostgreSQL `GROUP BY` precedence to group by the source column "x" (not the alias)_

```mdtest-spq
# spq
pragma pg = true
SELECT 0 AS x, COUNT(*) AS n GROUP BY x
# input
{x:1}
{x:2}
{x:1}
{x:2}
# expected output
{x:0,n:2}
{x:0,n:2}
```

---

_Stop a query at its first error_

```mdtest-spq
# spq
pragma catch
values this + 1
| where this > 2
# input
1
"two"
# expected output
error("incompatible types")
```
