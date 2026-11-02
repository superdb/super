# record_type

form a record type from a record of types

## Synopsis

```
record_type(r: record) -> type
```

## Description

The `record_type` function returns the [record
type](../../types/record.md) with the fields of `r` in order, where the
type of each field is the type value of that field of `r`, e.g.,
`record_type({x:<int64>,y:<string>})` is `<{x:int64,y:string}>`.

Each field of `r` must be a type value.  A field whose type value is an
[option type](option_type.md) is an optional field of the record type.

## Examples

---

_Form a record type_

```mdtest-spq
# spq
values record_type(this)
# input
{x:<int64>,y:<[string]>}
# expected output
<{x:int64,y:[string]}>
```

---

_Form a record type from the types of a value's fields with an optional field_

```mdtest-spq
# spq
values record_type({a:typeof(a),b:option_type(typeof(b))})
# input
{a:1,b:"x"}
# expected output
<{a:int64,b?:string}>
```
