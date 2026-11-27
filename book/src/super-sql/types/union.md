# Unions

The union type provides the foundation for
[sum types](https://en.wikipedia.org/wiki/Tagged_union) in SuperSQL.

Unions conform with the definition of the
[union type](../../formats/model.md#25-union) in the
ADM and follow the
[syntax](../../formats/sup.md#255-union-type)
of unions in the [SUP format](../../formats/sup.md), i.e.,
a union type has the form
```
<type> | <type>, ...
```
where `<type>` is any type and the set of types are unique.

A union literal can be created by casting a literal that is a member
of a union type to that union type, e.g.,
```
1::(int64|string)
```
When the type of the value cast to a union is not a member of that union,
an attempt is made to coerce the value to one of available member types.

To precisely such control coercion, an explicit first cast may be used as in
```
1::int8::(int8|int64|string)
```
Union values can be created by reading external data (SUP files,
database data, JSON objects, etc),
by constructing instances with a [type cast](../expressions/cast.md)
as above, or with other SuperSQL functions or expressions that produce unions
like the [`fuse`](../operators/fuse.md) operator.

Union values are also created when
[array](array.md#array-expressions),
[set](set.md#set-expressions), and
[map](map.md#map-expressions) expressions encounter mix-typed
elements that automatically express as union values.

For example,
```
values typeof([1,"foo"])
```
results in `<[int64|string]>`.

## Union Value Semantics

Internally, every union value includes a tag indicating which
of its member types the value belongs to along with that actual value.

In many languages, such tags are explicit names called a _discriminant_
and the underlying value can be accessed with a dot operator, e.g.,
`u.a` where `u` is a union value and `a` is the discriminant.  When
the instance of `u` does not correspond to the type indicated by `a`,
the result might be `null`.

In other languages, the _discriminant_ is the type name, e.g.,
`u.(int64)`.

SuperSQL uses the type name as the discriminant: a
[type assertion](../expressions/type-assertions.md) like `u.(int64)` yields
the underlying value when `u` holds an `int64` and `none` otherwise.

Many functions and operators accept union values directly and operate on
the underlying value, e.g., [upper](../functions/strings/upper.md) applied
to `"foo"::(int64|string)` is `"FOO"`.
[Arithmetic](../expressions/arithmetic.md#type-checking) and
[comparisons](../expressions/comparisons.md#type-checking) are type checked
against every type in the union, though.  When every type works with the
other operand, the union value is used as is, e.g., this predicate is true
```mdtest-spq {data-layout='no-labels'} {style='margin:auto;width:85%'}
# spq
values 1::(int64|float64)==1
# input

# expected output
true
```
but when some type in the union doesn't work, it's a type error, because the
query hasn't said what to do with a value of that type:
```mdtest-spq fails {data-layout='no-labels'} {style='margin:auto;width:85%'}
# spq
values 1::(int64|string)==1
# input

# expected output
type mismatch: int64|string == int64 at line 1, column 8:
values 1::(int64|string)==1
       ~~~~~~~~~~~~~~~~~~~~
```
A type assertion narrows the union to the type that works, and the query
must then say what to do when the value has another type, e.g., with `??`:
```mdtest-spq {data-layout='no-labels'} {style='margin:auto;width:85%'}
# spq
values (this.(int64) ?? 0) + 1
# input
1::(int64|string)
"foo"::(int64|string)
# expected output
2
1
```

The [typeof](../functions/types/typeof.md) function, however, returns the
union type itself rather than the type of the underlying value, e.g.,
```
values typeof(1::(int64|string))
```
is `<int64|string>`.

When desired, the [`under`](../functions/generics/under.md) function may be
used to express the underlying value explicitly.  For example,
```
values typeof(under(1::(int64|string)))
```
results in `<int64>`.

## Union Dispatch

Languages with sum types often include a construct to dispatch the
union to a case for each of its possible types.  In SuperSQL, this is the
[match](../expressions/match.md) expression, which selects a result
according to the type of a value.  Within each case, `this` is the
underlying value with the matched type, so it can be operated on without
a type error:
```mdtest-spq {data-layout="stacked"}
# spq
values
  match u
  when int64 then this+1
  when string then upper(this)
  when ip then network_of(this)
  end
# input
{u:1::(int64|string|ip)}
{u:"foo"::(int64|string|ip)}
{u:192.168.1.1::(int64|string|ip)}
# expected output
2
"FOO"
192.168.1.0/24
```
A value that matches no case yields `none`, or the result of an
`else` clause if there is one.

To route union values to different branches of a query instead, use the
[`switch`](../operators/switch.md) operator, e.g.,
```
switch typeof(under(u))
  case <int64> ( ... )
  case <string> ( ... )
  case <ip> ( ... )
```

## Examples
---
_Cast primitive values to a union type_
```mdtest-spq
# spq
values this::(int64|string)
# input
1
"foo"
# expected output
1::(int64|string)
"foo"::(int64|string)
```

_Explicitly express the underlying union value using
[`under`](../functions/generics/under.md)_
```mdtest-spq
# spq
values under(this)
# input
1::(int64|string)
# expected output
1
```

---

_Take the type of mixed-type array showing its union-typed construction_
```mdtest-spq
# spq
typeof(this)
# input
[1,"foo"]
# expected output
<[int64|string]>
```
