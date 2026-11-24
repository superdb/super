# substring

slice strings with SQL substring function

## Synopsis

```
substring(s: string [ FROM start: number ] [ FOR len: number ]) -> string
```

## Description

The `substring` function returns a slice of a string using
the anachronistic SQL syntax which includes the `FROM` and `FOR` keywords
inside of the call arguments.  The function returns a string of length `len`
comprising the Unicode code points starting at offset `start`.
At least one of `FROM` and `FOR` is required. Without `FROM`, the slice
starts at the beginning of `s`, and without `FOR`, it runs to the end of `s`.
A negative `start` counts back from the end of `s`.

Indexing is 0-based by default but can be 1-based by the use of
a [pragma](../../declarations/pragmas.md) as with
[generalized indexing](../../expressions/index.md#index-base).

>[!TIP]
> This function is implemented for backward compatibility with SQL.
> [Slice expressions](../../expressions/slices.md) should be used instead
> and are best practice.

## Examples

---
_Simple substring call in a SQL operator_

```mdtest-spq
# spq
SELECT SUBSTRING(this FROM 3 FOR 7) AS s
# input
" = SuperDB = "
# expected output
{s:"SuperDB"}
```

---

_1-based indexing_

```mdtest-spq
# spq
pragma index_base = 1
SELECT SUBSTRING(this FROM 4 FOR 7) AS s
# input
" = SuperDB = "
# expected output
{s:"SuperDB"}
```

---

_The length parameter is optional_

```mdtest-spq
# spq
SELECT SUBSTRING(this FROM 3) AS s
# input
" = SuperDB = "
# expected output
{s:"SuperDB = "}
```

---

_A negative start counts back from the end_

```mdtest-spq
# spq
values substring(this FROM -2)
# input
"SuperDB"
# expected output
"DB"
```
