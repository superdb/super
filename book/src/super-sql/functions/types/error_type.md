# error_type

form an error type from a type

## Synopsis

```
error_type(t: type) -> type
```

## Description

The `error_type` function returns the [error type](../../types/error.md)
whose values have type `t`, e.g., `error_type(<string>)` is
`<error(string)>`.

## Examples

---

_Form an error type_

```mdtest-spq
# spq
values error_type(this)
# input
<{msg:string}>
# expected output
<error({msg:string})>
```
