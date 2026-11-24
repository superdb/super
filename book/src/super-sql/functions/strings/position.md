# position

find position of a substring

## Synopsis

```
position(s: string, sub: string) -> int64
position(sub: string IN s:string) -> int64
```

## Description

The `position` function returns the 1-based index where string `sub` first
occurs in string `s`. If `sub` is not a sub-string of `s` then 0 is returned.
The index is always 1-based, whatever the
[index_base](../../declarations/pragmas.md) pragma is set to.

The second form in the synopsis is SQL syntax, and it works in pipe queries
too.

## Examples

---

```mdtest-spq
# spq
values position(s, sub)
# input
{s:"foobar",sub:"bar"}
# expected output
4
```

---

_0 means the substring wasn't found_

```mdtest-spq
# spq
values {input:this, pos:position(this, "bar")}
# input
"foobar"
"foo"
# expected output
{input:"foobar",pos:4}
{input:"foo",pos:0}
```

---

_The SQL syntax_

```mdtest-spq
# spq
SELECT position('bar' IN s) AS pos
# input
{s:"foobar"}
# expected output
{pos:4}
```
