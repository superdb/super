# supertypeof

the type of a value including its super types

## Synopsis

```
supertypeof(val: any) -> type
```

## Description

The `supertypeof` function returns the [type](../../types/intro.md) of
its argument `val` like [typeof](typeof.md) except that `val` is not
[superdefused](superdefuse.md) before its type is computed.  For a value
whose type includes [super types](../../types/super.md), e.g., a value
produced by [superfuse](../../operators/superfuse.md), the returned type
includes the super types whereas `typeof` returns the type of the original
value.  For any other value, `supertypeof` is the same as `typeof`.

`supertypeof` is useful for testing and debugging since super types are
otherwise visible only in output with the `-superfused` flag.

## Examples

---

_The type of a superfused value includes its super types_

```mdtest-spq {data-layout="stacked"}
# spq
superfuse | values {super:supertypeof(this),type:typeof(this)}
# input
{a:1}
{a:"x",b:2}
# expected output
{super:<{a:super(int64|string),b:super(int64|missing)}>,type:<{a:int64}>}
{super:<{a:super(int64|string),b:super(int64|missing)}>,type:<{a:string,b:int64}>}
```
