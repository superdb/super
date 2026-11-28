# collect

aggregate values into array

## Synopsis

```
collect(any) -> [any]
```

## Description

The _collect_ aggregate function organizes its input into an array.
If the aggregated values vary in type, the return type will be an array
of union of the types encountered.  If no values are aggregated, the
return value is an empty array.

>[!NOTE]
>See [array_agg](array_agg.md) for a variant that returns
>[none](../types/none.md) (or, in a SQL query, `null`, following the SQL
>standard) when no values are aggregated.

## Examples

Simple sequence collected into an array:
```mdtest-spq
# spq
collect(this)
# input
1
2
3
4
# expected output
[1,2,3,4]
```

Mixed types create a union type for the array elements:
```mdtest-spq
# spq
collect(this) | values this,typeof(this)
# input
1
2
3
4
"foo"
# expected output
[1,2,3,4,"foo"]
<[int64|string]>
```

Create arrays of values bucketed by key:
```mdtest-spq
# spq
collect(a) by k | sort
# input
{a:1,k:1}
{a:2,k:1}
{a:3,k:2}
{a:4,k:2}
# expected output
{k:1,collect:[1,2]}
{k:2,collect:[3,4]}
```

Contrast `collect` with `array_agg` when no values have been aggregated:
```mdtest-spq
# spq
aggregate
  collect(a) filter (a > 1),
  array_agg(a) filter (a > 1)
  by k
| sort
# input
{a:1,k:1}
{a:2,k:2}
{a:3,k:2}
# expected output
{k:1,collect:[]}
{k:2,collect:[2,3],array_agg:[2,3]}
```
