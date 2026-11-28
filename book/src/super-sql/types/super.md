# Super Types

A _super type_ has the form
```
super( <type> )
```
where `<type>` is called the _fused type_.  A _super value_ is a value
encoded in its fused type together with a type called its _subtype_,
which inverts the encoding to recover the original value.
Super types let values of many different types be stored and processed as
values of a single type without losing their original types.

Super types conform with the definition of the
[super type](../../formats/model.md#28-super) in the ADM
and follow the [syntax](../../formats/sup.md#259-super-type) of
super types in the [SUP format](../../formats/sup.md).
A super value has the form
```
super( <value>, <type-value> )
```
where `<value>` is the value encoded in the fused type and `<type-value>`
is its subtype, e.g.,
```
super(1::(int64|string),<int64>)
```

## Super Types as Unions

A super type is a form of [union](union.md).  A union type lists its member
types, and a union value is a value of one of those types along with a tag
that says which one.  A super type instead fuses its member types into a
single fused type, and a super value is a value encoded in the fused type
along with its subtype, which plays the role of the tag: it tells you the
original type of the value.

For example, the values `1` and `"x"` might be stored either as values of the
union type `int64|string` or as values of the super type
`super(int64|string)`.  Here the two forms carry the same information, but
fusion pays off for complex types.  The records `{a:1}` and `{b:"x"}` fuse
into the super type `super({a:int64|missing,b:string|missing})`, so they can
be processed as one record type with fields `a` and `b`, while their
subtypes `{a:int64}` and `{b:string}` remember exactly which fields each
record had.

Unlike a union tag, which selects from a fixed list of members, a subtype
can be any type whose values can be encoded in the fused type.

## Why Super Types

[Type fusion](../type-fusion.md) merges a set of types into one type that
can represent all of them, e.g., the records `{a:1}` and `{b:"x"}` fuse into
the type `{a:int64|missing,b:string|missing}`.  A plain fused type, as computed
by the [fuse](../aggregates/fuse.md) aggregate function, is lossy: once the
records are cast to it, there is no way to tell which fields each record
originally had.

A super type keeps that information.  Converting the records to a super
type encodes each one in the single fused type along with its
original type, so the records can be processed together as one type and
still be recovered exactly.  This is what lets SuperDB statically
[type check](../intro.md) queries over heterogeneous data and store it
efficiently in columnar form, while every value keeps its own type.

## Creating Super Values

The [superfuse](../aggregates/superfuse.md) aggregate function computes
the super type of its input:
```mdtest-spq {data-layout="stacked"}
# spq
aggregate superfuse(this)
# input
{a:1}
{b:"x"}
# expected output
<{a:super(int64|missing),b:super(string|missing)}>
```
and the [superfuse](../operators/superfuse.md) operator converts all of its
input to that type.  A value can also be converted to a super type with
[upcast](../functions/types/upcast.md), and the
[super_type](../functions/types/super_type.md) function forms a super type
from any type.

Casting a value to the type `any` produces a value of type `super(all)`,
which holds a value of any type along with its type.  In SUP, such a value is
written `<value>::any`.

## Displaying Super Values

Super values are displayed as their original values by default, i.e.,
super values in query results are automatically
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
The `-superfused` option of [super](../../command/super.md) displays the
super values themselves:
```mdtest-command
super -superfused -s -c "values {a:1},{b:'x'} | superfuse"
```
```mdtest-output
{a:super(1::(int64|missing),<int64>),b:super(missing::(string|missing),<missing>)}
{a:super(missing::(int64|missing),<missing>),b:super("x"::(string|missing),<string>)}
```

## Super Value Semantics

A super value behaves like the value it represents.
[typeof](../functions/types/typeof.md), [kind](../functions/types/kind.md),
[is](../functions/types/is.md),
[type assertions](../expressions/type-assertions.md), and
[match](../expressions/match.md) all see the subtype, while
[supertypeof](../functions/types/supertypeof.md) returns the super type:
```mdtest-spq {data-layout="stacked"}
# spq
superfuse
| values {typeof:typeof(a), supertypeof:supertypeof(a), is_int:is(a,<int64>)}
# input
{a:1}
{a:"x"}
# expected output
{typeof:<int64>,supertypeof:<super(int64|string)>,is_int:true}
{typeof:<string>,supertypeof:<super(int64|string)>,is_int:false}
```

Accessing a field of a superfused record yields the field's original value,
and a field that was absent from the original record is
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

The type checker checks expressions against the fused type, since that is
the type the query sees for every value.  For example, `a+1` is an error when
`a` has type `super(int64|string)` because `a` might be a string, so the query
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
super values back into their original values, and
[defuse](../functions/types/defuse.md) undoes the unions and optional
fields of a plain fused type.  Since query results are automatically
defused for display, these functions are mainly useful within a query that
needs the original values themselves rather than super values, e.g., so that
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
