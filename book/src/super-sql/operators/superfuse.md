# superfuse

[✅](../intro.md#data-order)&ensp; upcast all input values into a fused type

## Synopsis

```
superfuse
```

## Description

The `superfuse` operator computes a [fused type](../aggregates/superfuse.md)
over all of its input then upcasts all values in the input to the fused type.

This is logically equivalent to:
```
from input | values upcast(this, (from input | aggregate superfuse(this)))
```

Because all values of the input must be read to compute the fused type,
`superfuse` may spill its input to disk when memory limits are exceeded.

>[!NOTE]
> Spilling is not yet implemented for the vectorized runtime.

## Examples

---

_Fuse two records_
```mdtest-spq fusion
# spq
superfuse
# input
{a:1}
{b:2}
# expected output
{a:fusion(1::(int64|missing),<int64>),b:fusion(missing::(int64|missing),<missing>)}
{a:fusion(missing::(int64|missing),<missing>),b:fusion(2::(int64|missing),<int64>)}
```

---

_Fuse records with type variation_
```mdtest-spq fusion
# spq
superfuse
# input
{a:1}
{a:"foo"}
# expected output
{a:fusion(1::(int64|string),<int64>)}
{a:fusion("foo"::(int64|string),<string>)}
```

---

_Fuse records with complex type variation_
```mdtest-spq fusion {data-layout="stacked"}
# spq
superfuse
# input
{a:[1,2]}
{a:["foo","bar"],b:10.0.0.1}
# expected output
{a:fusion([fusion(1::(int64|string),<int64>),fusion(2::(int64|string),<int64>)],<[int64]>),b:fusion(missing::(ip|missing),<missing>)}
{a:fusion([fusion("foo"::(int64|string),<string>),fusion("bar"::(int64|string),<string>)],<[string]>),b:fusion(10.0.0.1::(ip|missing),<ip>)}
```
