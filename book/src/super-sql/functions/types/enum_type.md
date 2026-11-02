# enum_type

form an enum type from an array of symbols

## Synopsis

```
enum_type(a: [string]) -> type
```

## Description

The `enum_type` function returns the [enum type](../../types/enum.md)
whose symbols are the strings in the array `a` in order, e.g.,
`enum_type(["red","green"])` is `<enum(red,green)>`.

If `a` is empty or has a duplicate symbol, an error is returned.

## Examples

---

_Form an enum type_

```mdtest-spq
# spq
values enum_type(this)
# input
["red","green","blue"]
# expected output
<enum(red,green,blue)>
```
