# map_type

form a map type from key and value types

## Synopsis

```
map_type(r: {key:type,value:type}) -> type
```

## Description

The `map_type` function returns the [map type](../../types/map.md) whose
keys have type `r.key` and whose values have type `r.value`, e.g.,
`map_type({key:<string>,value:<int64>})` is `<map{string:int64}>`.

The argument must be a record with exactly the fields `key` and `value`,
in that order.

## Examples

---

_Form a map type_

```mdtest-spq
# spq
values map_type({key:<string>,value:typeof(this)})
# input
1
"foo"
# expected output
<map{string:int64}>
<map{string:string}>
```
