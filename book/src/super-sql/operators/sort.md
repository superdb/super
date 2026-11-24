# sort

[🔀](../intro.md#data-order)&ensp; sort values

## Synopsis

```
sort [-r] [<expr> [asc|desc] [nulls {first|last}] [, <expr> [asc|desc] [nulls {first|last}] ...]]
order by [-r] [<expr> [asc|desc] [nulls {first|last}] [, <expr> [asc|desc] [nulls {first|last}] ...]]
```

## Description

The `sort` operator sorts its input by reading all values until the end of input,
sorting the values according to the provided sort expression(s), and emitting
the values in the sorted order.

The `sort` operator can also be invoked as `order by`.

The sort expressions act as primary key, secondary key, and so forth. By
default, the sort order is ascending, from lowest value to highest. If
`desc` is specified in a sort expression, the sort order for that key is
descending.

SuperSQL follows the SQL convention that, by default, `null` values appear last
in either case of ascending or descending sort.  This can be overridden
by specifying `nulls first` in a sort expression.

If no sort expression is provided, a sort key is guessed based on heuristics applied
to the values present.
The heuristic examines the first input record and finds the first field in
left-to-right order that is an integer, or if no integer field is found,
the first field that is floating point. If no such numeric field is found, `sort` finds
the first field in left-to-right order that is _not_ of the `timestamp` data type.
Note that there are some cases (such as the output of a grouped aggregation performed on heterogeneous data) where the first input record to `sort`
may vary even when the same query is executed repeatedly against the same data.
If you require a query to show deterministic output on repeated execution,
explicit sort expressions must be provided.

If `-r` is specified, the sort order for each key is reversed without altering
the position of nulls. For clarity
when sorting by named fields, specifying `desc` is recommended instead of `-r`,
particularly when multiple sort expressions are present. However, `sort -r`
provides a shorthand if the heuristics described above suffice but reversed
output is desired.

If not all data fits in memory, values are spilled to temporary storage
and sorted with an external merge sort.

>[!NOTE]
> Spilling is not yet implemented for the vectorized runtime.

SuperSQL's `sort` is [stable](https://en.wikipedia.org/wiki/Sorting_algorithm#Stability)
such that values with identical sort keys always have the same relative order
in the output as they had in the input, such as provided by the `-s` option in
Unix's "sort" command-line utility.

During sorting, values are compared via byte order.  Between values of type
`string`, this is equivalent to
[C/POSIX collation](https://www.postgresql.org/docs/current/collation.html#COLLATION-MANAGING-STANDARD)
as found in other SQL databases such as Postgres.  As in Postgres, `NaN` is
sorted after any other number, including `+Inf`.

Note that a total order is defined over the space of all values even
between values of different types so sort order is always well-defined even
when comparing heterogeneously typed values.

## Examples

---

_A simple sort with a null_
```mdtest-spq
# spq
sort this
# input
2
null
1
3
# expected output
1
2
3
null
```

---

_With no sort expression, sort will sort by `this` for non-records_
```mdtest-spq
# spq
sort
# input
2
null
1
3
# expected output
1
2
3
null
```

---

_The "nulls last" default may be overridden_
```mdtest-spq
# spq
sort this nulls first
# input
2
null
1
3
# expected output
null
1
2
3
```

---

_With no sort expression, sort's heuristics will find a numeric key_
```mdtest-spq
# spq
sort
# input
{s:"bar",k:2}
{s:"bar",k:3}
{s:"foo",k:1}
# expected output
{s:"foo",k:1}
{s:"bar",k:2}
{s:"bar",k:3}
```

---

_It's best practice to provide the sort key_
```mdtest-spq
# spq
sort k
# input
{s:"bar",k:2}
{s:"bar",k:3}
{s:"foo",k:1}
# expected output
{s:"foo",k:1}
{s:"bar",k:2}
{s:"bar",k:3}
```

---

_Sort with a secondary key_
```mdtest-spq
# spq
sort k,s
# input
{s:"bar",k:2}
{s:"bar",k:3}
{s:"foo",k:2}
# expected output
{s:"bar",k:2}
{s:"foo",k:2}
{s:"bar",k:3}
```

---

_Sort by secondary key in reverse order when the primary keys are identical_
```mdtest-spq
# spq
sort k,s desc
# input
{s:"bar",k:2}
{s:"bar",k:3}
{s:"foo",k:2}
# expected output
{s:"foo",k:2}
{s:"bar",k:2}
{s:"bar",k:3}
```

_Sort with a numeric expression_
```mdtest-spq
# spq
sort x+y
# input
{s:"sum 2",x:2,y:0}
{s:"sum 3",x:1,y:2}
{s:"sum 0",x:-1,y:-1}
# expected output
{s:"sum 0",x:-1,y:-1}
{s:"sum 2",x:2,y:0}
{s:"sum 3",x:1,y:2}
```

---

_Case sensitivity affects sorting "lowest value to highest" in string values_
```mdtest-spq
# spq
sort
# input
{word:"hello"}
{word:"Hi"}
{word:"WORLD"}
# expected output
{word:"Hi"}
{word:"WORLD"}
{word:"hello"}
```

---

_Case-insensitive sort by using a string expression_
```mdtest-spq
# spq
sort lower(word)
# input
{word:"hello"}
{word:"Hi"}
{word:"WORLD"}
# expected output
{word:"hello"}
{word:"Hi"}
{word:"WORLD"}
```

---

_Shorthand to reverse the sort order for each key_
```mdtest-spq
# spq
sort -r
# input
2
null
1
3
# expected output
3
2
1
null
```
