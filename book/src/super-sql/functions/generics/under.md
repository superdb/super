# under

the underlying value

## Synopsis

```
under(val: any) -> any
```

## Description

The _under_ function returns the value underlying the argument `val`:
* for [unions](../../types/union.md), it returns the value as its elemental type of the union,
* for [errors](../../types/error.md), it returns the value that the error wraps,
* for [name-typed](../../types/named.md) values, it returns the value with the named type's underlying type,
* for [type values](../../types/type.md), it returns
  * the underlying type of a named type,
  * the element type of an array or set type,
  * the type wrapped by an option or error type,
  * an array of the types of a union type, and
  * a record `{key:<K>,value:<V>}` of the key and value types of a map type,
  and otherwise returns the type value unmodified; otherwise,
* it returns `val` unmodified.

## Examples

---

_Unions are unwrapped_
```mdtest-spq
# spq
values this
# input
1::(int64|string)
"foo"::(int64|string)
# expected output
1::(int64|string)
"foo"::(int64|string)
```

---

```mdtest-spq
# spq
values under(this)
# input
1::(int64|string)
"foo"::(int64|string)
# expected output
1
"foo"
```

---

_Errors are unwrapped_
```mdtest-spq
# spq
values this
# input
error("foo")
error({err:"message"})
# expected output
error("foo")
error({err:"message"})
```

---

```mdtest-spq
# spq
values under(this)
# input
error("foo")
error({err:"message"})
# expected output
"foo"
{err:"message"}
```

---

_Values of named types are unwrapped_
```mdtest-spq
# spq
values this
# input
type port=uint16
80::port
# expected output
type port=uint16
80::port
```

---

```mdtest-spq
# spq
values under(this)
# input
type port=uint16
80::port
# expected output
80::uint16
```

---

_Values that are not wrapped are unmodified_
```mdtest-spq
# spq
values under(this)
# input
1
"foo"
<int16>
{x:1}
# expected output
1
"foo"
<int16>
{x:1}
```

---

_Type values are unwrapped one level_
```mdtest-spq
# spq
values under(this)
# input
<[int64]>
<set[ip]>
<int64|string>
<map{string:[int64]}>
<option(string)>
<{x:int64}>
# expected output
<int64>
<ip>
[<int64>,<string>]
{key:<string>,value:<[int64]>}
<string>
<{x:int64}>
```
