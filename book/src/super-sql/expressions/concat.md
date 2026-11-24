# Concatenation

[Strings](../types/string.md) may be concatenated using the concatenation operator having the form
```
<expr> || <expr>
```
where `<expr>` is any [expression](intro.md) that results in a string value.

It is an error to concatenate non-string values.
Values may be converted to string using a [cast](cast.md) to type string.

The [concat](../functions/strings/concat.md) function also concatenates
strings and takes any number of arguments.

## Examples

---

_Concatenate two fields_

```mdtest-spq {data-layout="stacked"}
# spq
values a || b
# input
{a:"foo",b:"bar"}
{a:"hello, ",b:"world"}
{a:"foo",b:123}
# expected output
"foobar"
"hello, world"
error({message:"concat: string arg required",on:123})
```

---

_Cast non-string values to concatenate_

```mdtest-spq
# spq
values "foo" || 123::string
# input

# expected output
"foo123"
```
