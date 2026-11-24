# infer

[🎲](../intro.md#data-order)&ensp; infer types for strings and cast values to inferred type

## Synopsis

```
infer [ <limit> ]
```

## Description

The `infer` operator samples its input and attempts to infer native data types
for any string values appearing in the input.

The `<limit>` argument is an optional compile-time constant expression that
must evaluate to a positive integer. If `<limit>` is not provided,
it defaults to `100`.  If `<limit>` is set to 0, then all of the input
is consumed to perform the analysis.

The sampling process collects at least `<limit>` input values for each input type,
then computes an inferred type for the sample, where the inferred type is identical
to the input type except for any embedded string types inferred to be of a candidate type.
Such inference occurs when all of the values contained by that string type
are uniformly coercible to the candidate type, which may be one of:
* [int64](../types/numbers.md#signed-integers),
* [float64](../types/numbers.md#floating-point),
* [ip](../types/network.md),
* [net](../types/network.md),
* [timestamp](../types/time.md), or
* [bool](../types/bool.md).

`int64` inference takes precedence over `float64`.  All of the other candidate types
are unambiguous with one another.

If end of input is reached before collecting the desired sample size, then
the inference is conducted on the available values.

Once a type is inferred for a given sample, the values are cast to that type
and output by the operator.  If the inferred type is unchanged, then the values
are output unmodified.

The operator may reorder values as they are collected into a sample and analyzed.
Thus, the order of output is undefined.

## Examples

---

_Simple string inference_
```mdtest-spq
# spq
infer | sort this
# input
{a:1}
{a:"2"}
# expected output
{a:1}
{a:2}
```

---

_Infer mixed-type arrays_
```mdtest-spq
# spq
infer | sort this
# input
[1,"2"]
["3","4","5"]
# expected output
[1,2]
[3,4,5]
```

---

_Inference fails when data is inconsistent_
```mdtest-spq
# spq
infer | sort this
# input
[1,"2"]
[3,"4","5","foo"]
# expected output
[1,"2"]
[3,"4","5","foo"]
```
---
_Some messier data_
```mdtest-spq
# spq
infer | sort this
# input
{x:"1"}
{y:"2"}
{ts:"Jun 1, 2025"}
{a:["true","false"]}
{b:["1","2"]}
{ts:"Jun 2, 2025"}
{y:"foo"}
# expected output
{a:[true,false]}
{b:[1,2]}
{ts:2025-06-02T00:00:00Z}
{ts:2025-06-01T00:00:00Z}
{x:1}
{y:"2"}
{y:"foo"}
```
---
