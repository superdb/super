# fusion_type

form a fusion type from a type

## Synopsis

```
fusion_type(t: type) -> type
```

## Description

The `fusion_type` function returns the fusion type of `t`, e.g.,
`fusion_type(<int64|string>)` is `<fusion(int64|string)>`.  See
[superfuse](../../operators/superfuse.md) for more about fusion types.

## Examples

---

_Form a fusion type_

```mdtest-spq
# spq
values fusion_type(this)
# input
<int64|string>
# expected output
<fusion(int64|string)>
```
