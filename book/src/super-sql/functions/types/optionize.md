# optionize

convert unions with none to option types

## Synopsis

```
optionize(t: type) -> type
```

## Description

The `optionize` function returns the type `t` with every
[union type](../../types/union.md) that has a `none` member replaced
by an option type, i.e., `none|T` becomes
`option(T)` and `none|T1|T2|...` becomes `option(T1|T2|...)`.
Since an option type already represents an absent value, any
option members of such a union are absorbed into the result.
Record fields of option type are optional fields.

The type of a record field with a `null` member is treated the same way,
i.e., a field of type `null|T`, `none|T`, or `none|null|T` becomes a
field of type `option(T)`.  Since a [cast](cast.md) of `null` to an option
type that doesn't admit `null` is the option's `none`, a value whose field
is `null` is cast to a value without the field, just as when the field is
absent.  (A field whose only type is `null` is left as is.)

`optionize` is useful for turning a [fused type](../../aggregates/fuse.md),
where a field absent from some values has type `none|T`, into a type
with optional fields, which can then be the target of a [cast](cast.md),
where an absent field is cast to `none`.

Named types are left as is.

## Examples

---

_Replace unions with none_

```mdtest-spq
# spq
values optionize(this)
# input
<int64|none>
<none|int64|string>
<{a:int64|none,b:[{c:none|string}]}>
<int64|string>
# expected output
<option(int64)>
<option(int64|string)>
<{a?:int64,b:[{c?:string}]}>
<int64|string>
```

---

_Optionize a fused type_

```mdtest-spq
# spq
aggregate t:=fuse(this)
| values optionize(t)
# input
{x:1}
{x:2,y:"foo"}
# expected output
<{x:int64,y?:string}>
```

---

_Make the fields that are null or absent in some values optional_

```mdtest-spq
# spq
aggregate t:=fuse(this)
| values optionize(t)
# input
{x:1,y:"foo"}
{x:2,y:null}
{x:3}
# expected output
<{x:int64,y?:string}>
```
