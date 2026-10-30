# paths

return the paths of the leaves of a value

## Synopsis

```
paths(val: any) -> [[any]]
```

## Description

The `paths` function returns a path collection, i.e., an array of paths,
comprising the path of each leaf value in `val`.  The paths are the same
as the paths produced by [flatten](flatten.md), i.e., `paths(val)` is
```
[unnest flatten(val) | values path]
```
where a path is an array whose elements are a string for a record field,
an integer for the position of an element in an array or set, and any other
value for a map key.  A value that isn't a record, array, set, or map is
a leaf at the root so its path collection is `[[]]`.

If `val` is a type value of a record type, then `paths` returns
the paths of the fields of the type, descending into fields whose type is
a record type.

An error is returned if `val` is a type value that isn't a record type.

## Examples

---

_Extract the paths of a nested record_

```mdtest-spq
# spq
values paths(this)
# input
{a:1,b:2,c:{d:3,e:[4,5]}}
# expected output
[["a"],["b"],["c","d"],["c","e",0],["c","e",1]]
```

---

_Easily convert to dotted names if you prefer_

```mdtest-spq
# spq
unnest paths(this) | values join(this,".")
# input
{a:1,b:2,c:{d:3,e:4}}
# expected output
"a"
"b"
"c.d"
"c.e"
```

---

_The paths of a record type_

```mdtest-spq
# spq
values paths(this)
# input
<{a:int64,b:{c:string,d:[ip]}}>
# expected output
[["a"],["b","c"],["b","d"]]
```
