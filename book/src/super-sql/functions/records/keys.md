# keys

return the field names of a record or the keys of a map

## Synopsis

```
keys(val: record|map|type) -> [any]
```

## Description

The `keys` function returns an array of
* the names of the top-level fields of `val` in left-to-right order
  if `val` is a record,
* the keys of `val` in an undefined order if `val` is a map, or
* the names of the top-level fields of the type in left-to-right order if
  `val` is a type value of a record type.

[values](values.md) returns the corresponding field values or map values.

An error is returned if `val` is not a record, map, or type value of a record
type.

## Examples

---

_The field names of records and record types and the keys of a map_

```mdtest-spq
# spq
values keys(this)
# input
{a:1,b:{c:2}}
map{"x":1}
<{a:int64,b:{c:string}}>
# expected output
["a","b"]
["x"]
["a","b"]
```

---

_A record, map, or record type is expected_

```mdtest-spq
# spq
values keys(this)
# input
1
<int64>
# expected output
error({message:"keys: not a record or map",on:1})
error({message:"keys: not a record type",on:<int64>})
```
