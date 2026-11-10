# aggregate

[🎲](../intro.md#data-order)&ensp; execute aggregate functions with optional grouping expressions

## Synopsis

```
[aggregate] <agg> [, <agg> ... ] [ by <grouping> [, <grouping> ... ] ]
[aggregate] by <grouping> [, <grouping> ... ]
```
where `<agg>` references an [aggregate function](../aggregates/intro.md)
optionally structured as a  [field assignment](intro.md#field-assignment)
having the form:
```
[ <field> := ] <agg-func> ( [ all | distinct ] <expr> ) [ filter ( <pred> ) ]
```
and `<grouping>` is a grouping expression [field assignment](intro.md#field-assignment)
having the form:
```
[ <field> := ] <expr>
```

## Description

The `aggregate` operator applies
[aggregate functions](../aggregates/intro.md) to
partitioned groups of its input values to reduce each group to one output value
where the result of each aggregate function appears as a field of the result.

Each group corresponds to the unique values of the `<grouping>` expressions.
When there are no `<grouping>` expressions, the aggregate functions are applied
to the entire input optionally filtered by `<pred>`.

In the first form, the `aggregate` operator consumes all of its input,
applies one or more aggregate functions `<agg>` to each input value
optionally filtered by a `filter` clause and/or organized with the grouping
expressions specified after the `by` keyword, and at the end of input produces one
or more aggregations for each unique set of grouping key values.

In the second form, `aggregate` consumes all of its input, then outputs each
unique combination of values of the grouping expressions specified after the `by`
keyword without applying any aggregate functions.

The `aggregate` keyword is optional since it can be used as a
[shortcut](intro.md#shortcuts).

Each aggregate function `<agg-func>` may be optionally followed by a `filter` clause,
which applies a Boolean expression `<pred>` that indicates, for each input value,
whether to include it in the values operated upon by the aggregate function.
`filter` clauses are analogous
to the [`where`](where.md) operator but apply their filter to the input
argument stream to the aggregate function.

The output values are records formed from the
[field assignments](intro.md#field-assignment)
first from the grouping expressions then from the aggregate functions
in left-to-right order.
As in a record expression, a grouping key whose value
is a plain `none` (e.g., `ok(x)` when `x` is absent) disappears from its
output record, so the values whose key is `none` form a group whose record
has no field for that key.

When the result of `aggregate` is a single value (e.g., a single aggregate
function without grouping expressions or a single grouping expression without aggregates)
and there is no field name specified, then
the output is that single value rather than a single-field record
containing that value.

If the cardinality of grouping expressions causes the memory footprint to exceed
a limit, then each aggregate's partial results are spilled to temporary storage
and the results merged into final results using an external merge sort.

>[!NOTE]
> Spilling is not yet implemented for the vectorized runtime.

## Examples

---

_Average the input sequence_
```mdtest-spq
# spq
aggregate avg(this)
# input
1
2
3
4
# expected output
2.5
```

---

_To format the output of a single-valued aggregation into a record, simply specify
an explicit field for the output_
```mdtest-spq
# spq
aggregate mean:=avg(this)
# input
1
2
3
4
# expected output
{mean:2.5}
```

---

_When multiple aggregate functions are specified, even without explicit field names,
a record result is generated with field names implied by the functions_
```mdtest-spq
# spq
aggregate avg(this),sum(this),count()
# input
1
2
3
4
# expected output
{avg:2.5,sum:10,count:4}
```

---

_Sum the input sequence, leaving out the `aggregate` keyword_
```mdtest-spq
# spq
sum(this)
# input
1
2
3
4
# expected output
10
```

---

_Create integer sets by key and sort the output to get a deterministic order_
```mdtest-spq
# spq
set:=union(v) by key:=k | sort
# input
{k:"foo",v:1}
{k:"bar",v:2}
{k:"foo",v:3}
{k:"baz",v:4}
# expected output
{key:"bar",set:set[2]}
{key:"baz",set:set[4]}
{key:"foo",set:set[1,3]}
```

---

_Use a `filter` clause_
```mdtest-spq
# spq
set:=union(v) filter (v > 1) by key:=k | sort
# input
{k:"foo",v:1}
{k:"bar",v:2}
{k:"foo",v:3}
{k:"baz",v:4}
# expected output
{key:"bar",set:set[2]}
{key:"baz",set:set[4]}
{key:"foo",set:set[3]}
```

---

_Use a separate `filter` clause on each aggregate function_
```mdtest-spq
# spq
set:=union(v) filter (v > 1),
array:=collect(v) filter (k=="foo")
  by key:=k
| sort
# input
{k:"foo",v:1}
{k:"bar",v:2}
{k:"foo",v:3}
{k:"baz",v:4}
# expected output
{key:"bar",set:set[2],array:[]}
{key:"baz",set:set[4],array:[]}
{key:"foo",set:set[3],array:[1,3]}
```

---

_Results are included for `by` groupings that generate null results when `filter`
clauses are used inside `aggregate`_
```mdtest-spq
# spq
sum(v) filter (k=="bar") by key:=k | sort
# input
{k:"foo",v:1}
{k:"bar",v:2}
{k:"foo",v:3}
{k:"baz",v:4}
# expected output
{key:"bar",sum:2}
{key:"baz",sum:null}
{key:"foo",sum:null}
```

---

_To avoid null results for `by` groupings as just shown, filter before `aggregate`_
```mdtest-spq
# spq
k=="bar" | sum(v) by key:=k | sort
# input
{k:"foo",v:1}
{k:"bar",v:2}
{k:"foo",v:3}
{k:"baz",v:4}
# expected output
{key:"bar",sum:2}
```

---

_Output just the unique key values_
```mdtest-spq
# spq
by k | sort
# input
{k:"foo",v:1}
{k:"bar",v:2}
{k:"foo",v:3}
{k:"baz",v:4}
# expected output
"bar"
"baz"
"foo"
```
