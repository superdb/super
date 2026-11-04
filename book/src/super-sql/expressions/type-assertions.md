# Type Assertions

A type assertion has the form
```
<expr> . ( <type> )
```
where `<expr>` is any [expression](intro.md) and `<type>` is any
[type](../types/intro.md).  Its value is the value of `<expr>` when that
value has type `<type>` and is otherwise `none`,
which disappears from a record, array, or map, is dropped from the output of the
[values](../operators/values.md) operator, and is typically handled with
the `??` operator, e.g.,
```
v.(string) ?? "default"
```

A type assertion is a postfix operator that binds like the
[dot operator](dot.md), so assertions and field accesses may be chained,
e.g., `a.b.(string)` or `x.(Push)?.ref`.

A value has the asserted type when
* its type is `<type>`, including a union, option, or named type, in which
  case the value keeps its type,
* `<type>` is a [union](../types/union.md) type and the value's type is one
  of its member types, in which case the value keeps its member type,
* it is a value of a union type whose member has the asserted type,
* it is an option value whose some value has the asserted type, or
* it is a fusion value whose subtype is the asserted type.

An option's `none` has an option type, so it's the value of an assertion of
its option type, e.g., `x.(option(int64))`, and is otherwise `none`.

[Named types](../types/named.md) are distinct, e.g., a value of type
`port=uint16` has type `port` but not type `uint16`.

Unlike a [cast](cast.md), a type assertion never converts a value, so
`"1".(int64)` is `none` while `"1"::int64` is `1`.  An
[error](../types/error.md) or `missing` value is passed through rather
than becoming `none` (unless it has the asserted type) so that, e.g., a
misspelled field name isn't hidden by a default value.

>[!NOTE]
> A number literal must be parenthesized, e.g., `(1).(int64)`, since `1.` is
> a floating-point literal.

## Examples

---

_Assert a type with a default for other types_

```mdtest-spq
# spq
values this.(string) ?? "not a string"
# input
"hello"
1
{s:"x"}
# expected output
"hello"
"not a string"
"not a string"
```

---

_A type assertion doesn't convert values (unlike a cast)_

```mdtest-spq
# spq
values {assert:this.(int64) ?? -1, cast:this::int64}
# input
"1"
2
# expected output
{assert:-1,cast:1}
{assert:2,cast:2}
```

---

_Assert a union type to accept any of its member types_

```mdtest-spq
# spq
values this.(int64|float64) ?? 0
# input
1
2.5
"x"
# expected output
1
2.5
0
```

---

_The member of a union value and the some value of an option are asserted_

```mdtest-spq
# spq
values this.(int64) ?? -1
# input
1::(int64|string)
"a"::(int64|string)
some(2)
none::int64
# expected output
1
-1
2
-1
```

---

_An option value is asserted as is or on its some value_

```mdtest-spq
# spq
values {opt:typeof(this.(option(int64))), val:typeof(this.(int64))}
# input
some(1)
none::int64
# expected output
{opt:<option(int64)>,val:<int64>}
{opt:<option(int64)>,val:<none>}
```

---

_Select a variant of a sum type and access its fields_

```mdtest-spq
# spq
type Push={kind:string,ref:string}
type Watch={kind:string,action:string}
values this::(Push|Watch)
| values this.(Push)?.ref ?? "not a push"
# input
{kind:"Push",ref:"main"}
{kind:"Watch",action:"started"}
# expected output
"main"
"not a push"
```

---

_A failed assertion is none so it disappears from a record_

```mdtest-spq
# spq
values {s:x.(string),n:x.(int64)}
# input
{x:"a"}
{x:1}
# expected output
{s:"a"}
{n:1}
```
