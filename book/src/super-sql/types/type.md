# Type Values

Types in SuperSQL are _first class_ and conform
with the [`type`](../../formats/model.md#1-primitive-types) type in the
ADM.
The `type` type represents the type of a type value.

A type value is formed by enclosing a type specification in
angle brackets (`<` followed by the type followed by `>`).

For example, the integer type `int64` is expressed as a value
with the syntax `<int64>`.

The syntax for primitive type names are listed in the
[data model specification](../../formats/model.md#1-primitive-types)
and have the same syntax in SuperSQL.  Complex types also follow
the [SUP syntax for types](../../formats/sup.md#25-types).

Note that the type of a type value is simply `type`.

Here are a few examples of complex types:
* a simple record type - `{x:int64,y:int64}`
* an array of integers - `[int64]`
* a set of strings - `|[string]|`
* a map of strings keys to integer values - `|{string,int64}|`
* a union of string and integer  - `string|int64`

Complex types may be composed in a nested fashion,
as in `[{s:string}|{x:int64}]` which is an array of type
`union` of two types of records.

The [`typeof`](../functions/types/typeof.md) function returns a value's type as
a value, e.g., `typeof(1)` is `<int64>` and `typeof(<int64>)` is `<type>`.

Note the somewhat subtle difference between a record value with a field `t` of
type `type` whose value is type `string`
```
{t:<string>}
```
and a record type used as a value
```
<{t:string}>
```

First-class types are quite powerful because types can
serve as grouping keys or be used in [_data shaping_](../type-fusion.md) logic.
A common workflow for data introspection is to first perform a search of
exploratory data and then count the shapes of each type of data as follows:
```
search ... | count() by typeof(this)
```

## Examples
---
_Various type examples using f-string and typeof_

``` mdtest-spq
# spq
values f"{this} has type {typeof(this)}"
# input
1
"foo"
1.5
192.168.1.1
192.168.1.0/24
[1,"bar"]
set[1,2,3]
2025-08-21T21:22:18.046568Z
1d3h
<int64>
# expected output
"1 has type <int64>"
"foo has type <string>"
"1.5 has type <float64>"
"192.168.1.1 has type <ip>"
"192.168.1.0/24 has type <net>"
"[1,\"bar\"] has type <[int64|string]>"
"set[1,2,3] has type <set[int64]>"
"2025-08-21T21:22:18.046568Z has type <timestamp>"
"1d3h has type <duration>"
"<int64> has type <type>"
```
---
_Count the different types in the input_

``` mdtest-spq
# spq
count() by typeof(this) | sort this
# input
1
2
"foo"
10.0.0.1
<string>
# expected output
{typeof:<int64>,count:2}
{typeof:<string>,count:1}
{typeof:<ip>,count:1}
{typeof:<type>,count:1}
```
