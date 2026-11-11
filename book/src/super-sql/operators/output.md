# output

[✅](../intro.md#data-order)&ensp; send values to a named output

## Synopsis

```
output <name>
```

## Description

The `output` operator sends its input values to the query's output named
`<name>`, where `<name>` is an [identifier](../queries.md#identifiers).

A query's results are delivered on one or more named outputs.
Each leaf of a query's flowgraph that doesn't end in an `output` operator
implicitly ends in `output main`, so the results of most queries are delivered
on the output named `main`.  An `output` operator may only appear at a leaf
of a flowgraph, e.g., at the end of a query or at the end of a path of a
[fork](fork.md) or [switch](switch.md).

An `output` operator may also appear at a leaf of a
[subquery](../expressions/subqueries.md), including the body of
[unnest](unnest.md) `into`.  In this case, its values are sent to the
query's output, not to the subquery's result, so the leaf contributes no
values to the subquery.  A subquery whose leaves all end in `output` produces
no values.  Since a constant expression is evaluated before the query runs,
an `output` operator can't appear in one (e.g., in a
[const declaration](../declarations/constants.md)).

As for the [values](values.md) operator, a plain `none` (e.g., from
[ok](../functions/generics/ok.md) or `?.`) is dropped.

The values of a single `output` operator are output in order, but the order of
values from different `output` operators, whether in the query's flowgraph
or in a subquery, is undefined.

When running a query with the [super](../../command/super.md) command,
the output named `main` is written to the command's output
and the output named `debug` is printed to standard error (as for the
[debug](debug.md) operator).  Other outputs are discarded.

When running a query in a database service, each output is transmitted to the
client on the output channel of the same name.

## Examples

---

_Send values to the main output from a subquery_
```mdtest-spq
# spq
values (values this*10 | output main)
# input
1
2
# expected output
10
20
```

---

_Output the sum of each array_
```mdtest-spq
# spq
unnest this into (sum(this) | output main)
# input
[1,2]
[3,4]
# expected output
3
7
```

---

_The super command shows only the main output_
```mdtest-spq
# spq
switch
  case this > 2 ( output main )
  case true ( output small )
# input
1
3
# expected output
3
```
