# superfuse

compute a complete fused type of input values

## Synopsis

```
superfuse(any) -> type
```

## Description

The _superfuse_ aggregate function applies [type fusion](../type-fusion.md)
to its input and returns the fused type.  A superfused type differs
from a [fused type](fuse.md) as it includes fusion types in the nested type hierarchy
wherever type changes were made to combine types in the type fusion process.

## Examples

Superfuse two records:
```mdtest-spq
# spq
superfuse(this)
# input
{a:1,b:2}
{a:2,b:"foo"}
# expected output
<{a:int64,b:fusion(int64|string)}>
```
