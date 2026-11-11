# None and Missing

SuperSQL has two types for "nothing here," each with just one value:
* `none`, a value meaning that there is no value, and
* `missing`, which marks a field that's absent from some of the values
  described by one type.

They answer different questions.  A `none` says _the answer is nothing_,
while `missing` says _the field isn't there_.  You compute with `none`.
You don't compute with `missing`: SuperSQL reports it so you can say
what you want, usually by converting it to `none` with
[ok](../functions/generics/ok.md).

## None

A plain `none` is the result of an expression that produces nothing, e.g.,
* [ok](../functions/generics/ok.md) of an error,
* `?.` applied to `none`,
* a [type assertion](../expressions/type-assertions.md) that fails,
* a [match](../expressions/match.md) with no matching case and no `else`,
  or
* a pipe subquery that produces no values.

A `none` is handled quietly rather than reported:
* it's dropped from the output of the [values](../operators/values.md)
  operator, so it's never passed to a downstream operator,
* it disappears from a record expression, an array, a set, or a map,
* [aggregate functions](../aggregates/intro.md) skip it,
* `??` replaces it with another value, and
* `?.` passes it through a field access.

For example,
```mdtest-spq
# spq
values {a:this, b:ok(10/this)}, ok(10/this) ?? "bad value"
# input
2
0
# expected output
{a:2,b:5}
5
{a:0}
"bad value"
```

The `none` of an [option type](../../formats/model.md#29-option), e.g.,
`none::int64`, isn't a plain `none`.  It's a value of its option type, and does
it does not disappear from record fields or from array/set/map values like
a plain `none` does.

## Missing

A `missing` never comes from an expression.  It appears only in data whose
different shapes are described by one type, e.g., the records produced by
the [fuse](../operators/fuse.md) operator:
```mdtest-spq
# spq
fuse
# input
{a:1}
{b:"x"}
# expected output
{a:1::(int64|missing),b:missing::(string|missing)}
{a:missing::(int64|missing),b:"x"::(string|missing)}
```
Here, the type `int64|missing` says that `a` is absent from some of the
values, and each record holds `missing` for the field it doesn't have.

Reading a field where it's absent is an [error](error.md), just as it is for
a field that doesn't exist at all:
```mdtest-spq
# spq
fuse | values b
# input
{a:1}
{b:"x"}
# expected output
error("no such field b")
"x"::(string|missing)
```

When the types of a query's input are known, a field that may be absent is
reported before the query runs:
```mdtest-spq fails
# spq
values {a:1},{b:2} | values a ?? 0
# input

# expected output
field may be missing (consider ok()) at line 1, column 29:
values {a:1},{b:2} | values a ?? 0
                            ~
```
Note that `??` and `?.` handle `none`, not `missing`.  To handle a field
that may be absent, convert it to `none` with
[ok](../functions/generics/ok.md), then drop the `none` or replace it with
`??`:
```mdtest-spq
# spq
values {a:1},{b:2} | values {input:this, ok:a.ok(), with_default:a.ok() ?? 0}
# input
# expected output
{input:{a:1},ok:1,with_default:1}
{input:{b:2},with_default:0}
```

## Why Two Types?

A `none` from `ok()` or a failed type assertion is something the query asked
for, so it's safe to drop it or replace it without comment.

A field that may be absent is different.  It usually means the data doesn't
look the way the query expects, e.g., the field name is misspelled, or only
some of the input has the field.  If an absent field quietly became `none`,
such mistakes would silently drop values from the output.

Keeping the two apart lets SuperSQL drop `none` quietly while still catching
every read of a field that may not be there.  Writing `ok()` makes the choice
explicit: where the field is absent, treat it as nothing.

This resembles the distinction between `null` and `missing` in
[SQL++](https://asterixdb.apache.org/docs/0.9.7.1/sqlpp/manual.html), but in SuperSQL a field that isn't
there is an error rather than a value (see [Errors](error.md#missing)),
and `missing` only marks such fields in the types and values of data.

>[!NOTE]
> The [SUP](../../formats/sup.md#26-null-missing-and-none-values) format spells
> the missing value `missing` so that data with absent fields can be written
> and read, but `missing` isn't intended for use in queries.
