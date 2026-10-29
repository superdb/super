# sum_type

form a union of named types from a map

## Synopsis

```
sum_type(m: map{string:type}) -> type
```

## Description

The `sum_type` function returns a [union type](../../types/union.md)
whose members are [named types](../../types/named.md), one for each entry
of the map `m`, where each entry's key is the name and its value is the type,
i.e., `sum_type(map{"A":<T1>,"B":<T2>})` is `<A=T1|B=T2>`.  This forms
a sum type (or tagged union), where the name of each member identifies its
variant even when two variants have the same underlying type.

If `m` has a single entry, `sum_type` returns its named type.

If a key of `m` is already bound to a different type, an error is returned.

## Examples

---

_Form a sum type from a map of types_

```mdtest-spq
# spq
values sum_type(this)
# input
map{"Circle":<{radius:float64}>,"Rect":<{width:float64,height:float64}>}
# expected output
type Circle={radius:float64}
type Rect={width:float64,height:float64}
<Circle|Rect>
```

---

_Compute a sum type of events from their blended types_

```mdtest-spq
# spq
blend(this) by kind
| collect_map(map{kind:blend})
| values sum_type(this)
# input
{kind:"Push",ref:"main"}
{kind:"Watch",action:"started"}
{kind:"Push",ref:"dev"}
# expected output
type Push={kind:string,ref:string}
type Watch={kind:string,action:string}
<Push|Watch>
```
