# Fusions

A _fusion value_ is a value of one type, called the fusion's _supertype_,
together with the type of the value it represents, called its _subtype_.
Fusions let values of many different types be stored and processed as
values of a single type without losing their original types.

Fusions conform with the definition of the
[fusion type](../../formats/model.md#28-fusion) in the super-structured data
model and follow the [syntax](../../formats/sup.md#259-fusion-type) of
fusions in the [SUP format](../../formats/sup.md), i.e., a fusion type has
the form
```
fusion( <type> )
```
where `<type>` is the supertype, and a fusion value has the form
```
fusion( <value>, <type-value> )
```
where `<value>` is a value of the supertype and `<type-value>` is its subtype,
e.g.,
```
fusion(1::(int64|string),<int64>)
```

## Why Fusions

[Type fusion](../type-fusion.md) merges a set of types into one type that
can represent all of them, e.g., the records `{a:1}` and `{b:"x"}` fuse into
the type `{a:int64|missing,b:string|missing}`.  A plain fused type, as computed
by the [fuse](../aggregates/fuse.md) aggregate function, is lossy: once the
records are cast to it, there is no way to tell which fields each record
originally had.

A fusion type keeps that information.  Converting the records to a fusion
type stores each one as a value of the single fused type along with its
original type, so the records can be processed together as one type and
still be recovered exactly.  This is what lets SuperDB statically
[type check](../intro.md) queries over heterogeneous data and store it
efficiently in columnar form, while every value keeps its own type.

## Creating Fusion Values

The [superfuse](../aggregates/superfuse.md) aggregate function computes
the fusion type of its input:
```mdtest-spq
# spq
aggregate superfuse(this)
# input
{a:1}
{b:"x"}
# expected output
<{a:fusion(int64|missing),b:fusion(string|missing)}>
```
and the [superfuse](../operators/superfuse.md) operator converts all of its
input to that type.  A value can also be converted to a fusion type with
[upcast](../functions/types/upcast.md), and the
[fusion_type](../functions/types/fusion_type.md) function forms a fusion type
from any type.

Casting a value to the type `any` produces a value of type `fusion(all)`,
which holds a value of any type along with its type.  In SUP, such a value is
written `<value>::any`.

## Displaying Fusion Values

Fusion values are displayed as their underlying values by default, i.e.,
fusion values in query results are automatically
[defused](../functions/types/superdefuse.md):
```mdtest-spq
# spq
superfuse
# input
{a:1}
{b:"x"}
# expected output
{a:1}
{b:"x"}
```
The `-fusion` option of [super](../../command/super.md) displays the fusion
values themselves:
```mdtest-spq fusion
# spq
superfuse
# input
{a:1}
{b:"x"}
# expected output
{a:fusion(1::(int64|missing),<int64>),b:fusion(missing::(string|missing),<missing>)}
{a:fusion(missing::(int64|missing),<missing>),b:fusion("x"::(string|missing),<string>)}
```

## Fusion Value Semantics

A fusion value behaves like the value it represents.
[typeof](../functions/types/typeof.md), [kind](../functions/types/kind.md),
[is](../functions/types/is.md),
[type assertions](../expressions/type-assertions.md), and
[match](../expressions/match.md) all see the subtype, while
[supertypeof](../functions/types/supertypeof.md) returns the fusion type:
```mdtest-spq {data-layout="stacked"}
# spq
superfuse
| values {typeof:typeof(a), supertypeof:supertypeof(a), is_int:is(a,<int64>)}
# input
{a:1}
{a:"x"}
# expected output
{typeof:<int64>,supertypeof:<fusion(int64|string)>,is_int:true}
{typeof:<string>,supertypeof:<fusion(int64|string)>,is_int:false}
```

Accessing a field of a fused record yields the field's original value, and
a field that was absent from the original record is
[missing](none.md#missing), so the type checker requires the query to
handle it, e.g., with `?.` and `??`:
```mdtest-spq
# spq
superfuse | values {a:this?.a ?? 0, b:this?.b ?? ""}
# input
{a:1}
{b:"x"}
# expected output
{a:1,b:""}
{a:0,b:"x"}
```

The type checker checks expressions against the supertype, since that is
the type the query sees for every value.  For example, `a+1` is an error when
`a` has type `fusion(int64|string)` because `a` might be a string, so the query
must say what to do, e.g., with a type assertion:
```mdtest-spq
# spq
superfuse | values (a.(int64) ?? 0) + 1
# input
{a:1}
{a:"x"}
# expected output
2
1
```

## Defusing

The [superdefuse](../functions/types/superdefuse.md) function converts
fusion values back into their original values, and
[defuse](../functions/types/defuse.md) undoes the unions and optional
fields of a plain fused type.  Since query results are automatically
defused for display, these functions are mainly useful within a query that
needs the original values themselves rather than fusion values, e.g., so that
`supertypeof` returns the original type:
```mdtest-spq
# spq
superfuse | values supertypeof(superdefuse(this))
# input
{a:1}
{b:"x"}
# expected output
<{a:int64}>
<{b:string}>
```
