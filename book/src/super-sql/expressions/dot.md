# Dot

Records and maps with string keys are dereferenced with the dot operator `.`
as is customary in other languages.  The syntax is
```
<expr> . <id>
```
where `<expr>` is an [expression](intro.md) resulting in a dereferenceable value
and `<id>` is an [identifier](../queries.md#identifiers) representing the
field name of a record or a string key of a map.

Dereferenceable values include [records](../types/record.md),
[maps](../types/map.md) with keys of type string, and
[type values](../types/type.md).

The result of a dot expression is
* the value of the indicated field for a record type,
* the value of the indicated entry for a map with string keys, or
* the type value of the indicated field for a type value.

When navigating a type value, the dot operator looks through named,
optional, fusion, and error types and into the element type of
an array or set and the value type of a map, e.g., `<[{a:int64}]>.a`
is `<int64>`.  For a union type, the result is the union of the field's
types in the union's types that have the field, e.g.,
`<{a:int64}|{a:string}|int64>.a` is `<int64|string>`.

When a field or key is referenced in a dereferenceable type but that
field or key is not present, then `error("missing")` is the result.

If a non-dereferenceable type is operated upon with the dot operator,
then a compile-time error results for statically typed data and a
[structured error](../types/error.md) results for dynamic data.

Note that identifiers containing unusual characters can be represented
by enclosing the identifier in backtick quotes, e.g.,
```
x.`a b`
```
references the field or key whose name is `a b`.

Alternatively, [index syntax](index.md) may be used to access the field as a string
value, e.g.,
```
x["a b"]
```

If a field name is not representable as an identifier,
then [indexing](index.md)
may be used with a quoted string to represent any valid field name.
Such field names can be accessed using [this](../intro.md#pipe-scoping)
with an index-style reference, e.g., `this["field with spaces"]`.

## Examples

---

_Dereference a map, a record, and a record type_

```mdtest-spq
# spq
values this.x.y
# input
map{"x":{y:1},"y":2}
{x:{y:1},y:2}
<{x:{y:int64},y:int64}>
# expected output
1
1
<int64>
```

---

_Navigate into a type value_

```mdtest-spq
# spq
values this.a.b
# input
<{a:{b:string}}>
<{a:[{b:ip}]}>
<{a:map{string:{b:int64}}}>
<{a:{b:int64}|{b:string}|{c:bool}}>
<{a:int64}>
# expected output
<string>
<ip>
<int64>
<int64|string>
error({message:"no such field 'b'",on:<int64>})
```

---

_Use backtick quotes or index syntax for identifiers with special characters_

```mdtest-spq
# spq
values x.`a b`, x["a b"]
# input
{x:{"a b":123}}
# expected output
123
123
```
