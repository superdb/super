# join

concatenate array of strings with a separator

## Synopsis

```
join(val: [string] [, sep: string]) -> string
```

## Description

The `join` function concatenates the elements of string array `val` to create a single
string. If the string `sep` is given, it is placed between each value in the
resulting string.

## Examples

---

_Join an array of strings with commas_

```mdtest-spq
# spq
values join(this, ",")
# input
["a","b","c"]
# expected output
"a,b,c"
```

---

_Join non-string arrays by first casting_

```mdtest-spq
# spq
values join(cast(this, <[string]>), "...")
# input
[1,2,3]
[10.0.0.1,10.0.0.2]
# expected output
"1...2...3"
"10.0.0.1...10.0.0.2"
```

---

_The separator is optional_

```mdtest-spq
# spq
values join(this)
# input
["a","b","c"]
# expected output
"abc"
```
