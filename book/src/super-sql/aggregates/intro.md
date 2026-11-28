# Aggregate Functions

Aggregate functions compute aggregated results from zero or more
input values and have the form
```
<name> ( [ all | distinct ] <expr> ) [ filter ( [where] <pred> ) ]
```
where
* `<name>` is an identifier naming the function,
* `all` and `distinct` are optional keywords,
* `<expr>` is any [expression](../expressions/intro.md) that is type compatible
with the particular function, and
* `<pred>` is an optional Boolean expression that filters inputs to the function.

Aggregate functions may appear in
* the [aggregate](../operators/aggregate.md) operator,
* an aggregate [shortcut](../operators/intro.md#shortcuts), or
* in [SQL expressions](../sql/intro.md) when performing aggregations.

When aggregate functions appear in context of grouping
(e.g., the `by` clause of an [aggregate](../operators/aggregate.md) operator or a
[SELECT](../sql/select.md) query with a [GROUP BY](../sql/group-by.md) clause),
then the aggregate function produces one output value for each
unique combination of grouping expressions.

When an aggregate function has no values to aggregate, e.g., when its
input is empty, a `filter` clause excludes every value, or every value is
one the function skips (like a null for [sum](sum.md)), the result is
[none](../types/none.md), which disappears from the output record of the
[aggregate](../operators/aggregate.md) operator, or, in a SQL query, `null`.
[count](count.md) and [dcount](dcount.md) return 0 and
[collect](collect.md) returns an empty array instead.

If the case-insensitive `distinct` option is present, then the inputs
to the aggregate function are filtered to unique values for each
unique grouping.

If the case-insensitive `all` option is present, then all values
for each unique group are passed as input to the aggregate function.

Calling an aggregate function in a pipe-operator expression outside of
an aggregating context is an error.
