# shapes

[🎲](../intro.md#data-order)&ensp; aggregate sample values by type

## Synopsis

```
shapes [ <expr> ]
```

## Description

The `shapes` operator aggregates the values computed by `<expr>`
by type and produces an arbitrary sample value for each unique type
in the input.  It ignores null values and errors.

`shapes` is a shorthand for
```
where <expr> is not null
| aggregate sample:=any(<expr>) by typeof(<expr>)
| values this?.sample
```

If `<expr>` is not present, then `this` is presumed.

## Examples

---

```mdtest-spq
# spq
shapes | sort
# input
1
2
3
"foo"
"bar"
null
error(1)
# expected output
1
"foo"
```
---

```mdtest-spq
# spq
shapes a | sort
# input
{a:1}
{b:2}
{a:"foo"}
# expected output
1
"foo"
```
