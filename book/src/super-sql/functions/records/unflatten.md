# unflatten

transform a path-value collection into a value

## Synopsis

```
unflatten(val: [{path:[any]|string,value:any}]) -> any
```

## Description

The `unflatten` function converts the path-value collection `val`, i.e.,
an array of records `{path,value}` as produced by [flatten](flatten.md), into
the value comprising each `value` at its `path`.
If `val` isn't an array or set, then `val` is returned unmodified.

The first element of the paths determines the structure at each level
of the value:
* a string element is a record field,
* an integer element is the index of an array element, and
* any other value is a map key.

A path whose elements conflict with an earlier path at the same level
(e.g., `["a",0]` and `["a","b"]`) is an error.  When a path is the same as or
a prefix of an earlier path, its value replaces the earlier value.

When an array is missing elements, e.g., because its paths have been filtered,
the elements of the array are optional and the missing elements are none.
An array index must be less than 1,048,576.

`unflatten` is the inverse of `flatten`, i.e., `unflatten(flatten(v))`
is `v`, except that
* a set becomes an array,
* a map with string keys becomes a record,
* a map with integer keys becomes an array, and
* an absent optional field is dropped.

A path may be abbreviated as a string of elements separated by periods,
where an element that is an unsigned decimal number is an array index,
e.g., `"x.1.y"` is the path `["x",1,"y"]`.  An abbreviation can't represent
a field name that contains a period.

## Examples

---

_Unflatten a path-value collection_

```mdtest-spq {data-layout="stacked"}
# spq
values unflatten(this)
# input
[{path:["a"],value:1},{path:["b",0],value:2},{path:["b",1,"c"],value:3}]
# expected output
{a:1,b:[2,{c:3}]}
```

---

_Abbreviate paths as strings_

```mdtest-spq {data-layout="stacked"}
# spq
values unflatten(this)
# input
[{path:"a",value:1},{path:"b.0",value:2},{path:"b.1.c",value:3}]
# expected output
{a:1,b:[2,{c:3}]}
```

---

_Flatten to unflatten_

```mdtest-spq
# spq
unnest flatten(this) into (
  path[0] != "rm"
  | collect(this)
)
| values unflatten(this)
# input
{a:1,rm:2}
# expected output
{a:1}
```

---

_Filtered array elements are none_

```mdtest-spq
# spq
unnest flatten(this) into (
  value != 2
  | collect(this)
)
| values unflatten(this)
# input
[1,2,3]
# expected output
[some(1),none::int64,some(3)]
```
