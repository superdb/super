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
```mdtest-spq superfused
# spq
superfuse
# input
{a:1}
{b:2}
# expected output
{a:super(1::(int64|missing),<int64>),b:super(missing::(int64|missing),<missing>)}
{a:super(missing::(int64|missing),<missing>),b:super(2::(int64|missing),<int64>)}
```

---

_Fuse records with type variation_
```mdtest-spq superfused
# spq
superfuse
# input
{a:1}
{a:"foo"}
# expected output
{a:super(1::(int64|string),<int64>)}
{a:super("foo"::(int64|string),<string>)}
```

---

_Fuse records with complex type variation_
```mdtest-spq superfused {data-layout="stacked"}
# spq
superfuse
# input
{a:[1,2]}
{a:["foo","bar"],b:10.0.0.1}
# expected output
{a:super([super(1::(int64|string),<int64>),super(2::(int64|string),<int64>)],<[int64]>),b:super(missing::(ip|missing),<missing>)}
{a:super([super("foo"::(int64|string),<string>),super("bar"::(int64|string),<string>)],<[string]>),b:super(10.0.0.1::(ip|missing),<ip>)}
```
