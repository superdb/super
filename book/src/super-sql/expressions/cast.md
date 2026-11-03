# Casts

Casting is the process of converting a value from its current data type
to another type using an explicit expression having the form
```
<expr> :: <type>
```
where `<expr>` is any [expression](intro.md) and `<type>` is any
[type](../types/intro.md) that is
compatible with `<expr>`.  When `<expr>` and `<type>` are incompatible,
[structured errors](../types/error.md) result as described below.

The SQL syntax
```
CAST(<expr> AS <type>)
```
is also supported.

To cast to the value form of a type, i.e., a [type value](../types/type.md),
the [cast](../functions/types/cast.md) function may be used.

When a cast is successful, the return value of `cast` always has the target type.

If errors are encountered, then some or all of the resulting value
will be embedded with structured errors and the result does not have
the target type.

The target type cannot contain an error type.  The [error](../functions/errors/error.md) function
should instead be used to create error values.

## Primitive Values

Some primitive values can be cast to other primitive types, but not all possibilities are
permitted and instead result in structured errors.
Except for [union](../types/union.md) and
[named](../types/named.md) types,
primitive values cannot be cast to complex types.

The casting rules for primitives are as follows:
* A [number](../types/numbers.md) may be cast to
  * another [number](../types/numbers.md) type as long as the numeric value
    is not outside the scope of the target type,
    which results in a structured error,
  * type [string](../types/string.md),
  * type [bool](../types/bool.md) where zero is `false` and non-zero is `true`,
  * type [duration](../types/time.md) where the number is presumed to be nanoseconds,
  * type [timestamp](../types/time.md) where the number is presumed to be nanoseconds since epoch, or
  * a [union](#union-types) or [named type](#named-types).
* A [string](../types/string.md) may be cast to any other primitive type as long as
the string corresponds to a valid SuperSQL primitive literal.  Time strings
in particular may represent typical timestamp formats.  When cast to the
[bytes](../types/bytes.md) type,
the result is the byte encoding of the UTF-8 string.  A string may also be cast to
a [union](#union-types) or [named](#named-types) type.
To parse a literal
string that is in the SUP or JSON format without having to specify the target type, use
the [parse_sup](../functions/parsing/parse_sup.md) function.
* A [bool](../types/bool.md) may be cast to
  * a number type where `false` is zero and `true` is `1`,
  * type [string](../types/string.md), or
  * a [union](#union-types) or [named type](#named-types).
* A [timestamp](../types/time.md) value may be cast to
  * a [number](../types/numbers.md) type where the numeric value is nanoseconds since epoch,
  * type [string](../types/string.md), or
  * a [union](#union-types) or [named type](#named-types).

A null value of type [null](../types/null.md) may be cast to any type.

>[!NOTE]
> A future version of this documentation will provide detailed documentation for
> acceptable date/time strings.

## Complex Values

When a complex value has multiple levels of nesting,
casting is applied recursively into the value hierarchy.
For example, cast is recursively
applied to each element in an array of records, then recursively applied to each
of those records.

If there is a mismatch between the type of the input value and target type then
structured errors appear within the portion of a nested value that
is not castable.

The casting rules for complex values are as follows:

* A [record](../types/record.md) may be cast to
  * a [record](../types/record.md) type where any fields not present in the
    target type are omitted, any fields not present in the input value while present in the
    target type are set to null, and the value of each input field present
    in both the input and target are recursively cast to the target's type of
    that field,
  * a [map](../types/map.md) type with `string` keys where each field of the
    input value is an entry whose key is the field's name and whose value is
    the field's value recursively cast to the value type of the target map
    type (an absent optional field has no entry),
  * a [string](../types/string.md) type where the string is the input value
    serialized in the [SUP](../../formats/sup.md) format, or
  * a [union](#union-types) or [named type](#named-types).
* An [array](../types/array.md) may be cast to
  * an [array](../types/array.md) type where the elements of the input value are
    recursively cast to the element type of the target array type,
  * a [set](../types/set.md) type where the elements of the input value are
    recursively cast to the element type of the target set type and any duplicate
    values are automatically removed,
  * a [string](../types/string.md) type where the string is the input value
    serialized in the [SUP](../../formats/sup.md) format, or
  * a [union](#union-types) or [named type](#named-types).
* A [set](../types/set.md) may be cast to
  * a [set](../types/set.md) type where the elements of the input value are
    recursively cast to the element type of the target set type,
  * an [array](../types/array.md) type where the elements of the input value are
    recursively cast to the element type of the target array type,
  * a [string](../types/string.md) type where the string is the input value
    serialized in the [SUP](../../formats/sup.md) format, or
  * a [union](#union-types) or [named type](#named-types).
* A [map](../types/map.md) may be cast to
  * a [map](../types/map.md) type where the keys and values of the input value are
    recursively cast to the key and value type of the target map type,
  * a [record](../types/record.md) type, when the keys of the input value are
    strings, where each field's value is the value of the entry whose key is
    the field's name recursively cast to the field's type, entries whose
    keys aren't fields of the target type are omitted, and a field with no
    entry is absent as when casting a record,
  * a [string](../types/string.md) type where the string is the input value
    serialized in the [SUP](../../formats/sup.md) format, or
  * a [union](#union-types) or [named type](#named-types).
* An [enum](../types/enum.md) may be cast to
  * an [enum](../types/enum.md) type where the target type includes the symbol
    of the value being cast,
  * a [string](../types/string.md) type where the string is the input value
    serialized in the [SUP](../../formats/sup.md) format, or
  * a [union](#union-types) or [named type](#named-types).

## Union Types

When casting a value to a union type, the member type of the union is selected
to find a _best fit_ of the available types.  If no fit exists, a structured
error is returned.

If the input type is present in the member types, then the best fit is that type.

A record is cast to a map member of the union only if no other member fits,
e.g., so that any JSON value may be cast to a recursive JSON type like
`type JSON = null|bool|int64|float64|string|[JSON]|map{string:JSON}`.

Otherwise, the best fit is determined from the input type as follows:

>[!NOTE]
> A future version of this documentation will provide detailed documentation for
> the best-fit selection algorithm.

## Named Types

When casting to a named type, the cast is carried out using its underlying type
then the named type is reattached to the result.

## Errors

Casting a value to an incompatible type results in a structured error of the form:
```
{message:"cannot cast to <target>", on:<val>}
```
When errors appear within a complex value, the returned
value may not be wrapped in a structured error and the problematic portions
of the cast can be debugged by inspecting the result for precisely where
the errors arose.

For example, notice the error returned by the following function call.
```mdtest-spq {data-layout='no-labels'} {style='margin:auto;width:85%'}
# spq
cast({a:"1",b:2}, <{a:int64,b:ip}>)
# input

# expected output
{a:1,b:error({message:"cannot cast to ip",on:2})}
```
That is, the value for `a` was successfully cast from string `"1`" to integer `1` but
the value for `b` could not be cast to an IP address so a structured error is
instead embedded as the value for `b`.

## Examples

---

_Cast various primitives to type `ip`_

```mdtest-spq {data-layout="stacked"}
# spq
values this::ip
# input
"10.0.0.1"
1
"foo"
# expected output
10.0.0.1
error({message:"cannot cast to ip",on:1})
error({message:"cannot cast to ip",on:"foo"})
```

---

_Cast array of strings to array of IPs_

```mdtest-spq
# spq
values this::[ip]
# input
["10.0.0.1","10.0.0.2"]
# expected output
[10.0.0.1,10.0.0.2]
```

---

_Cast a record to a different record type_

```mdtest-spq
# spq
values this::{b:string}
# input
{a:1,b:2}
{b:4}
# expected output
{b:"2"}
{b:"4"}
```

---

_Multiple syntax options for casting_

```mdtest-spq
# spq
type port=uint16
values
  80::port,
  CAST(80 AS port),
  cast(80::uint16, 'port'),
  cast(cast(80, <uint16>), 'port')
# input

# expected output
type port=uint16
80::port
80::port
80::port
80::port
```

---

_Casting time strings is fairly flexible_

```mdtest-spq
# spq
values this::timestamp
# input
"May 8, 2009 5:57:51 PM"
"oct 7, 1970"
# expected output
2009-05-08T17:57:51Z
1970-10-07T00:00:00Z
```

---
_Cast to a declared type_

```mdtest-spq
# spq
type port = uint16
values this::port
# input
80
8080
# expected output
type port=uint16
80::port
8080::port
```

---

_Cast nested records_

```mdtest-spq {data-layout="stacked"}
# spq
values this::{ts:timestamp,r:{x:float64,y:float64}}
# input
{ts:"1/1/2022",r:{x:"1",y:"2"}}
{ts:"1/2/2022",r:{x:3,y:4}}
# expected output
{ts:2022-01-01T00:00:00Z,r:{x:1.,y:2.}}
{ts:2022-01-02T00:00:00Z,r:{x:3.,y:4.}}
```
