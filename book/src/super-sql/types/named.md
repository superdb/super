# Named Types

A named type provides a means to bind a symbolic name to a type
and conforms to the
[named type](../../formats/model.md#3-named-type)
in the ADM.
The named type [syntax](../../formats/sup.md#258-named-type)
follows that of [SUP format](../../formats/sup.md), i.e.,
a named type has the form
```
type <name>=<type>
```
where `<name>` is an identifier or string and `<type>` is any type.

Named types may be defined in four ways:
* with a [type](../declarations/types.md) declaration,
* with a [cast](../expressions/cast.md), or
* imported from types defined in the input data.

For example, named types can be declared with a type statement, e.g.,
```
type port = int16
values 80::port
```
produces the value `80::port` as above.

Named types may also be defined by the input data itself, as super-structured data is
comprehensively self describing.
When named types are defined in the input data, there is no need to declare their
type in a query.
In this case, a SuperSQL expression may refer to the type by the name that simply
appears to the runtime as a side effect of operating upon the data.

When the same name is bound to different types, an error results.

## Examples

---

_Filter on a type name defined in the input data_

```mdtest-spq
# spq
type foo=int64
where typeof(this)==<foo>
# input
type foo=int64
1::foo
type bar=int64
2::bar
3::foo
# expected output
type foo=int64
1::foo
3::foo
```

---

_Emit a type name defined in the input data_

> [!NOTE]
> This query doesn't work properly yet as a recent change to SuperSQL requires
> compile-time types and the input is not yet being scanned in the playground
> examples to compute those types.

```mdtest-spq-skip
# spq
values <foo>
# input
type foo=int64
1::foo
# expected output
type foo=int64
<foo>
```
