# upcast

convert a value to a supertype

## Synopsis

```
upcast(val: any, target: type) -> any
```

## Description

The `upcast` function is like [cast](cast.md) but does not perform any
type coercion and converts a value `val` from its type to any supertype of its type
as indicated by the `target` type argument.

When a record value does not contain a field in the super type, the super type's
corresponding field must be optional and the field will be absent (i.e., "none").

A type is a supertype of a subtype if all paths through the subtype are valid
paths through the supertype.

Upcasting is used by the [fuse](../../operators/fuse.md) operator.

When an upcast is successful, the return value of `cast` always has the target type.

If an error is encountered, the offending value and target type are returned
in a structured error.

## Examples

---

_Upcast showing an absent field versus null_

```mdtest-spq {data-layout="stacked"}
# spq
values
  upcast({x:1},<{x:int64,y?:string}>),
  upcast({x:1},<{x:int64,y?:string|null}>)
# input

# expected output
{x:1,y?:none::string}
{x:1,y?:none::option(string|null)}
```

---
