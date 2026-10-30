# superdefuse

undo the effects of complete fusion

## Synopsis

```
superdefuse(val any) -> any
```

## Description

The `superdefuse` function converts a value `val` containing any fusion types
into its original type by downcasting all instances of fusion values to their
subtype equivalent.

## Examples

---

_Remove union types_

```mdtest-spq {data-layout="stacked"}
# spq
superdefuse(this)
# input
fusion({a:1::(int64|string)},<{a:int64}>)
fusion({a:"foo"::(int64|string)},<{a:string}>)
# expected output
{a:1}
{a:"foo"}
```

---

_Retain optional fields using complete fusion_

```mdtest-spq {data-layout="stacked"}
# spq
superfuse | superdefuse(this)
# input
{x:1}
{x:2,y:3}
{x:4,z?:none::int64}
# expected output
{x:1}
{x:2,y:3}
{x:4,z?:none::int64}
```
