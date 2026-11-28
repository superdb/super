# array_agg

aggregate values into array

## Synopsis

```
array_agg(any) -> [any]
```

## Description

The _array_agg_ aggregate function organizes its input into an array.
If the aggregated values vary in type, the return type will be an array
of union of the types encountered.  If no values are aggregated, the
return value is [none](../types/none.md) or, in a SQL query, `null`,
following the SQL standard.

>[!NOTE]
>See [collect](collect.md) for a variant that returns an empty array when no
>values are aggregated.

## Examples

Simple sequence aggregated into an array (see [collect](collect.md) docs for more examples):
```mdtest-spq
# spq
array_agg(this)
# input
1
2
3
4
# expected output
[1,2,3,4]
```

Contrast `array_agg` with `collect` when no values have been aggregated:
```mdtest-spq
# spq
aggregate
  array_agg(a) filter (a > 1),
  collect(a) filter (a > 1)
  by k
| sort
# input
{a:1,k:1}
{a:2,k:2}
{a:3,k:2}
# expected output
{k:1,collect:[]}
{k:2,array_agg:[2,3],collect:[2,3]}
```
