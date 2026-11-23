# compare

compare values even when types vary

## Synopsis

```
compare(a: any, b: any [, nullsMax: bool]) -> int64
```

## Description

The `compare` function returns an integer comparing two values. The result will
be 0 if a is equal to b, +1 if a is greater than b, and -1 if a is less than b.
`compare` differs from [comparison expressions](../../expressions/comparisons.md),
for which values of different types are a
[type mismatch](../../expressions/comparisons.md#type-checking),
in that it is polymorphic and works for values of any types (e.g., `compare(1, "1")`).
Its order can also differ from that of comparison expressions for values they
can compare, e.g., it orders maps, which comparison expressions don't, and
it orders arrays of different element types (e.g., `[1.5]` and `[2]`) by
their types rather than by their elements.

Values are compared via byte order.  Between values of type `string`, this is
equivalent to [C/POSIX collation](https://www.postgresql.org/docs/current/collation.html#COLLATION-MANAGING-STANDARD)
as found in other SQL databases such as Postgres.  As with the comparison
expressions, `NaN` is equal to `NaN` and greater than any other number.

>[!NOTE]
> A future version of SuperSQL will collate values polymorphically using a well-defined
> total order that embodies the super-structured
> [type order](../../../formats/model.md).

`nullsMax` is an optional value (true by default) that determines whether `null`
is treated as the minimum or maximum value.

## Examples

---

```mdtest-spq
# spq
values compare(a, b)
# input
{a:2,b:1}
{a:2,b:"1"}
# expected output
1
-1
```
