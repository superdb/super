# array_type

form an array type from a type

## Synopsis

```
array_type(t: type) -> type
```

## Description

The `array_type` function returns the [array type](../../types/array.md)
whose elements have type `t`, e.g., `array_type(<string>)` is
`<[string]>`.

## Examples

---

_Form an array type_

```mdtest-spq
# spq
values array_type(this)
# input
<string>
<{x:int64}>
# expected output
<[string]>
<[{x:int64}]>
```
