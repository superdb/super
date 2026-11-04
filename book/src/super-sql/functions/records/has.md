# has

test whether a record or record type has a field or a map has a key

## Synopsis

```
has(val: any, key: any) -> bool
```

## Description

The `has` function returns true if
* `val` is a record whose type has a field named `key`,
* `val` is a type value of a record type with a field named `key`, or
* `val` is a map with the key `key`

and false otherwise, e.g., for a value that isn't a record, type value, or
map.

For a record, the result depends only on its type, not on the value of the
field, so `has` is true for a field whose value is null, an error, or an
option's none.  A value of a named type or a union type is tested by its
underlying type.

For a map, `key` may be of any type and is compared with the map's keys after
removing named and union types, so a key of a different type (e.g., an
`int32` for a map with `int64` keys) is never found.

An error is returned if `val` is a record and `key` isn't a string.

## Examples

---

_Test records for fields_

```mdtest-spq
# spq
values {a:has(this,"a"),b:has(this,"b")}
# input
{a:1}
{b:null}
{a:error("x"),b:2}
1
# expected output
{a:true,b:false}
{a:false,b:true}
{a:true,b:true}
{a:false,b:false}
```

---

_Filter on the presence of a field_

```mdtest-spq
# spq
where has(this,"b")
# input
{a:1}
{b:2}
# expected output
{b:2}
```

---

_Test a record type_

```mdtest-spq
# spq
values has(this,"a")
# input
<{a:int64}>
<{b:int64}>
# expected output
true
false
```

---

_Test a map for a key_

```mdtest-spq
# spq
values has(this,"a"), has(this,1)
# input
map{"a":1}
map{1:"x"}
# expected output
true
false
false
true
```

---

_The field name may be computed_

```mdtest-spq
# spq
values has(this, f)
# input
{a:1,f:"a"}
{a:1,f:"b"}
# expected output
true
false
```
