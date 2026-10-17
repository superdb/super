# unblend

undo the effects of blend

## Synopsis

```
unblend(val any) -> any
```

## Description

The `unblend` function converts a value `val` from a blended representation
to a plain representation by
* replacing any value nested within `val` that is a union type with its
  underlying non-union value, and
* eliminating all optional fields from record types within `val` by
  replacing each optional field with value with its non-optional equivalent
  and eliminating entirely any optional fields without a value.

## Examples

---

_Remove union types_

```mdtest-spq {data-layout="stacked"}
# spq
unblend(this)
# input
{a:1::(int64|string)}
{a:"foo"::(int64|string)}
# expected output
{a:1}
{a:"foo"}
```

---

_Create then remove optional fields_

```mdtest-spq {data-layout="stacked"}
# spq
blend | unblend(this)
# input
{x:1}
{x:2,y:3}
{x:4,z?:none::int64}
# expected output
{x:1}
{x:2,y:3}
{x:4,z?:none::int64}
```
