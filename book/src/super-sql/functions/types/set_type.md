# set_type

form a set type from a type

## Synopsis

```
set_type(t: type) -> type
```

## Description

The `set_type` function returns the [set type](../../types/set.md) whose
elements have type `t`, e.g., `set_type(<ip>)` is `<set[ip]>`.

## Examples

---

_Form a set type_

```mdtest-spq
# spq
values set_type(this)
# input
<ip>
# expected output
<set[ip]>
```
