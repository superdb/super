# position

find position of a substring

## Synopsis

```
position(s: string, sub: string) -> int64
position(sub: string IN s:string) -> int64
```

## Description

The `position` function returns the 1-based index where string `sub` first
occurs in string `s`, counting Unicode code points as
[slices](../../expressions/slices.md) and [substring](substring.md) do.
If `sub` is not a sub-string of `s` then 0 is returned.
The index is always 1-based, whatever the
[index_base](../../declarations/pragmas.md) pragma is set to.
Subtract 1 to get a 0-based index, e.g., for a slice. Check for 0 first,
since a substring that isn't found then gives -1, and a slice that starts
at -1 begins at the last character.

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

---

_Code points are counted, so subtracting 1 (after checking for 0) gives a 0-based index for a slice_

```mdtest-spq
# spq
values {input:this, pos:position(this, "bar")}
| values {input, pos, rest:pos > 0 ? input[pos-1:] : null}
# input
"éébar"
"foo"
# expected output
{input:"éébar",pos:3,rest:"bar"}
{input:"foo",pos:0,rest:null}
```
