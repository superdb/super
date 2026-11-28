# super_type

form a super type from a type

## Synopsis

```
super_type(t: type) -> type
```

## Description

The `super_type` function returns the super type of `t`, e.g.,
`super_type(<int64|string>)` is `<super(int64|string)>`.  See
[superfuse](../../operators/superfuse.md) for more about super types.

## Examples

---

_Form a super type_

```mdtest-spq
# spq
values super_type(this)
# input
<int64|string>
# expected output
<super(int64|string)>
```
