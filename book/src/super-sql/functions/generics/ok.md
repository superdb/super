# ok

convert errors to none

## Synopsis

```
ok(val: any) -> any
```

## Description

The `ok` function is inspired by the Rust language pattern of the same name
and returns `none` when `val` is `none` or an error and otherwise returns `val`
unmodified.

This function is useful when errors are expected and are to be explicitly managed
with `none` operators.  The `none` disappears from a record expression, an
array, or a map and is dropped from the output of the
[values](../../operators/values.md) operator, so `??` is needed only to replace
it with another value.

## Examples

---

```mdtest-spq
# spq
values ok(10/this) ?? "bad value"
# input
2
0
# expected output
5
"bad value"
```

---

```mdtest-spq
# spq
count() by key.ok() ?? "MISSING" | sort this
# input
{key:1}
{key:1}
{key:2}
{}
# expected output
{key:1,count:2}
{key:2,count:1}
{key:"MISSING",count:1}
```
