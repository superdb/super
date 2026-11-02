# option_type

form an option type from a type

## Synopsis

```
option_type(t: type) -> type
```

## Description

The `option_type` function returns the option type of `t`, e.g.,
`option_type(<int64>)` is `<option(int64)>`.  An option type is the type
of an optional field, so it is useful as the type of a field in the
argument to [record_type](record_type.md).

If `t` is an option type, `missing`, or `none`, an error is returned.

## Examples

---

_Form a record type with an optional field_

```mdtest-spq
# spq
values record_type({x:<int64>,y:option_type(this)})
# input
<string>
# expected output
<{x:int64,y?:string}>
```
