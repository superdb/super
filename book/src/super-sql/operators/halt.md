# halt

[✅](../intro.md#data-order)&ensp; stop the query and output a final value

## Synopsis

```
halt [ <expr> ]
```

## Description

The `halt` operator stops the query when it receives its first input value.
The query's pending output is dropped and the value of the
[expression](../expressions/intro.md) `<expr>` for that input value becomes
the query's last value.  If `<expr>` is omitted, it's `this`.
As for the [values](values.md) operator, an input value for which `<expr>` is a
plain `none` is skipped.

A query that halts fails: after its last value, the query ends with the error
`query halted`, so the [super](../../command/super.md) command exits with a
nonzero status, a query run by a database service ends with an error, and the
MCP `query` tool returns its results with `isError` set.

`halt` can appear anywhere a query can, including in a
[subquery](../expressions/subqueries.md) and in the handler of a
[catch](catch.md).  Since the query stops at the first input value, a halt
usually follows a filter or is used to fail fast on an error with
`catch halt`.

## Examples

---

_Stop at the first value that meets a condition_
```mdtest-spq fails
# spq
where this > 2 | halt {stopped_at:this}
# input
1
2
3
4
# expected output
{stopped_at:3}
query halted
```

---

_Stop at the first error_
```mdtest-spq fails
# spq
values 10/this | catch halt
# input
"two"
# expected output
error("incompatible types")
query halted
```
