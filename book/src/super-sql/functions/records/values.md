# values

return the field values of a record or the values of a map

## Synopsis

```
values(val: record|map) -> [any]
```

## Description

The `values` function returns an array of
* the values of the top-level fields of `val` in left-to-right order
  if `val` is a record, or
* the values of `val` in an undefined order if `val` is a map.

An absent optional field of a record has no value.  If the values have
different types, the inner type of the array is a union of the types.

[keys](keys.md) returns the corresponding field names or map keys.

An error is returned if `val` is not a record or map.

>[!NOTE]
> At the start of a pipe operator, `values(...)` is the
> [values](../../operators/values.md) operator, not this function,
> so write `values values(this)` rather than `values(this)` as an operator.

## Examples

---

_The field values of a record and the values of a map_

```mdtest-spq
# spq
values values(this)
# input
{a:1,b:"x",c:{d:2}}
map{"x":1}
# expected output
[1,"x",{d:2}]
[1]
```

---

_A record or map is expected_

```mdtest-spq
# spq
values values(this)
# input
<{a:int64}>
# expected output
error({message:"values: not a record or map",on:<{a:int64}>})
```
