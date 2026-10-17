# blend

[✅](../intro.md#data-order)&ensp; upcast all input values into a blended type

## Synopsis

```
blend
```

## Description

The `blend` operator computes a [blended type](../aggregates/blend.md)
over all of its input then upcasts all values in the input to the blended type.

This is logically equivalent to:
```
from input | values upcast(this, (from input | aggregate blend(this)))
```
Because all values of the input must be read to compute the blended type,
`blend` may spill its input to disk when memory limits are exceeded.

>[!NOTE]
> Spilling is not yet implemented for the vectorized runtime.

## Examples

---

_Blend two records_
```mdtest-spq
# spq
blend
# input
{a:1}
{b:2}
# expected output
{a:1::(int64|missing),b:none::(int64|missing)}
{a:none::(int64|missing),b:2::(int64|missing)}
```

---

_Blend records with type variation_
```mdtest-spq
# spq
blend
# input
{a:1}
{a:"foo"}
# expected output
{a:1::(int64|string)}
{a:"foo"::(int64|string)}
```

---

_Blend records with complex type variation_
```mdtest-spq {data-layout="stacked"}
# spq
blend
# input
{a:[1,2]}
{a:["foo","bar"],b:10.0.0.1}
# expected output
{a:[1,2]::[int64|string],b:none::(ip|missing)}
{a:["foo","bar"]::[int64|string],b:10.0.0.1::(ip|missing)}
```
