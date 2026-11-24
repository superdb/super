# concat

concatenate strings

## Synopsis

```
concat(s: string [, ... s: string]) -> string
```

## Description

The `concat` function concatenates its string arguments, in the order given,
into a single string. Arguments that are `null` are skipped, and if every
argument is `null`, the result is an empty string.

It is an error to concatenate non-string values.
Values may be converted to string using a [cast](../../expressions/cast.md)
to type string.

The [concatenation operator](../../expressions/concat.md) `||` also
concatenates strings.

## Examples

---

_Concatenate fields and a literal_

```mdtest-spq
# spq
values concat(first, " ", last)
# input
{first:"Ada",last:"Lovelace"}
# expected output
"Ada Lovelace"
```

---

_Null arguments are skipped_

```mdtest-spq
# spq
values {input:this, s:concat(a, "-", b)}
# input
{a:"x",b:"y"}
{a:"x",b:null}
# expected output
{input:{a:"x",b:"y"},s:"x-y"}
{input:{a:"x",b:null},s:"x-"}
```

---

_Cast non-string values to concatenate them_

```mdtest-spq
# spq
values {input:this, s:concat("port ", port), cast:concat("port ", port::string)}
# input
{port:8080}
# expected output
{input:{port:8080},s:error({message:"concat: string arg required",on:8080}),cast:"port 8080"}
```
