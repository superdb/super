# flatten

transform a value into its paths and leaf values

## Synopsis

```
flatten(val: any) -> [{path:[any],value:any}]
```

## Description

The `flatten` function returns a _path-value collection_ for `val`, i.e.,
an array of records `{path,value}` where `path` is the path of each
leaf value nested in `val` and `value` is that leaf value.
If `val` isn't a record, array, set, or map, then `val` is returned unmodified.

A _path_ is an array of elements that navigate into a value, inspired by
the paths of [jq](https://jqlang.org/manual/#paths):
* a string element is a record field,
* an integer element is the position of an element in an array or set, and
* any other value is a map key.

A leaf value is a value that isn't a record, array, set, or map, or is an empty
record, array, set, or map.  An absent optional field has no leaf.

If there are multiple types for the paths or the leaf values in `val`, then the
inner type of the array is a union of the record types present.

[unflatten](unflatten.md) is the inverse of `flatten`.

## Examples

---

_Flatten a nested record_

```mdtest-spq {data-layout="stacked"}
# spq
values flatten(this)
# input
{a:1,b:{c:"foo"}}
# expected output
[{path:["a"],value:1},{path:["b","c"],value:"foo"}]
```

---

_Flatten arrays, sets, and maps_

```mdtest-spq {data-layout="stacked"}
# spq
values flatten(this)
# input
{a:[1,{b:2}],s:set["x"],m:map{10.0.0.1:true}}
# expected output
[{path:["a",0],value:1},{path:["a",1,"b"],value:2},{path:["s",0],value:"x"},{path:["m",10.0.0.1],value:true}]
```

---

_A primitive value is its own flattening_

```mdtest-spq
# spq
values flatten(this)
# input
1
"foo"
# expected output
1
"foo"
```

---

_List the paths of the leaves_

```mdtest-spq
# spq
unnest flatten(this) | values path
# input
{a:1,b:{c:[2,3]}}
# expected output
["a"]
["b","c",0]
["b","c",1]
```
