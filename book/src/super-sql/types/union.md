# Unions

The union type provides the foundation for
[sum types](https://en.wikipedia.org/wiki/Tagged_union) in SuperSQL.

Unions conform with the definition of the
[union type](../../formats/model.md#25-union) in the
super-structured data model and follow the
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

However, SuperSQL is polymorphic so there is no requirement to explicitly
discriminate the member type of a union value.  When an expression
operator or function references a union value in computation,
then the underlying value in its member type is _automatically_ expressed
from the union value.

For example, this predicate is true
```
values 1::(int64|string)==1
```
because the union value is automatically expressed as `1::int64`
by the comparison operator.

Likewise
```
values 1::(int64|string)+2::(int64|string)
```
results in `3::int64`.  Note that because of automatic expression,
the union type is not retained here.

Passing a union value to a function, however, does not involve
evaluation and thus automatic expression does not occur here, e.g.,
```
values typeof(1::(int64|string))
```
is `<int64|string>` because the union value is _not_ automatically
expressed as `1::int64` when it is passed to the
[typeof](../functions/types/typeof.md) function.

When desired, the [`under`](../functions/generics/under.md) function may be
used to express the underlying value explicitly.  For example,
```
values typeof(under(1::(int64|string)))
```
results in `<int64>`.

## Union Dispatch

Languages with sum types often include a construct to dispatch the
union to a case for each of its possible types.

Because SuperSQL is polymorphic, union dispatch is not generally needed.
Instead, union values are simply operated upon and the "right thing happens".
An exception is [arithmetic](../expressions/arithmetic.md#type-checking),
which requires a union whose types aren't all numbers to be narrowed first.

That said, union dispatch may be accomplished with the
[`switch`](../operators/switch.md) operator or a
[`case`](../expressions/conditional.md) expression.

For example, `switch` can be used to route union values to different
branches of a query:
```
values
  {u:1::(int64|string|ip)},
  {u:"foo"::(int64|string|ip)},
  {u:192.168.1.1::(int64|string|ip)}
| switch typeof(under(u))
    case <int64> ( ... )
    case <string> ( ... )
    case <ip> ( ... )
    default ( values error({message: "unknown type", on:this}) )
| ...
```
>[!NOTE]
> Note the presence of a default case above.  In statically typed languages with
> sum types, the compiler can ensure that all possible cases for a union are covered
> and report an error otherwise.  In this case, there would be no need for a default.
> A future version of SuperSQL will include more comprehensive compile-time type
> checking and will include a mechanism for explicit union dispatch with
> static type checking.

A `case` expression can also be used to dispatch union values inside of
an expression as in
```
values
  {u:1::(int64|string|ip)},
  {u:"foo"::(int64|string|ip)},
  {u:192.168.1.1::(int64|string|ip)}
| values
    case typeof(under(u))
      when <int64> then u+1
      when <string> then upper(u)
      when <ip> then network_of(u)
      else "unknown"
    end
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
