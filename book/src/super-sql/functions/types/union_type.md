# union_type

form a union type from an array of types

## Synopsis

```
union_type(a: [type]) -> type
```

## Description

The `union_type` function returns the [union type](../../types/union.md)
whose members are the type values in the array `a`, e.g.,
`union_type([<int64>,<string>])` is `<int64|string>`.

Duplicate types are removed and a union type in `a` contributes its
members since a union can't contain a union.  If `a` has a single
distinct type, `union_type` returns that type.  If `a` is empty, an
error is returned.

## Examples

---

_Form a union type_

```mdtest-spq
# spq
values union_type(this)
# input
[<int64>,<string>]
[<int64|string>,<bool>,<int64>]
# expected output
<int64|string>
<int64|bool|string>
```

---

_Form a union of the types of a set of values_

```mdtest-spq
# spq
collect(typeof(this)) | values union_type(this)
# input
1
"foo"
2
# expected output
<int64|string>
```
