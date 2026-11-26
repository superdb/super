# Arrays

Arrays conform to the
[array type](../../formats/model.md#22-array)
in the super data model and follow the
[syntax](../../formats/sup.md#252-array-type)
of arrays in the [SUP format](../../formats/sup.md), i.e.,
an array type has the form
```
[ <type> ]
```
where `<type>` is any type.

Any SUP text defining an [array value](../../formats/sup.md#242-array-value)
is a valid array literal in the SuperSQL language.

For example, this is a simple array value
```
[1,2,3]
```
whose type is
```
[int64]
```

An empty array value has the form `[]` and
an empty array type defaults to an array of type null, i.e., `[null]`,
unless otherwise cast, e.g., `[]::[int64]` represents an empty array
of integers.

Arrays can be created by reading external data (SUP files,
database data, Parquet values, JSON objects, etc) or by
constructing instances using
[_array expressions_](#array-expressions) or other
SuperSQL functions that produce arrays.

## Array Expressions

Array values are constructed from an _array expression_ that is comprised of
zero or more comma-separated elements contained in brackets:
```
[ <element>, <element>, ... ]
```
where an `<element>` has one of two forms:
```
<expr>
```
or
```
...<expr>
```
`<expr>` may be any valid [expression](../expressions/intro.md).

The first form is simply an element in the array, the result of `<expr>`.

The second form is the array spread operator `...`,
which expects an array or set value as
the result of `<expr>` and inserts all of the values from the result.  If a spread
expression results in neither an array nor set, then the value is elided.

When the expressions result in values of non-uniform type, then the types of the
array elements become a sum type of the types present,
tied together with the corresponding [union type](union.md).

## Examples
---
```mdtest-spq
# spq
values [1,2,3],["hello","world"]
# input

# expected output
[1,2,3]
["hello","world"]
```

---

_Arrays can be concatenated using the spread operator_
```mdtest-spq
# spq
values [...a,...b,5]
# input
{a:[1,2],b:[3,4]}
# expected output
[1,2,3,4,5]
```

---

_Arrays with mixed type are tied together with a union type_
```mdtest-spq
# spq
values typeof([1,"foo"])
# input

# expected output
<[int64|string]>
```

---

_The collect aggregate function builds an array
and uses a sum type for the mixed-type elements_
```mdtest-spq
# spq
collect(this) | values this, typeof(this)
# input
1
2
3
"hello"
"world"
# expected output
[1,2,3,"hello","world"]
<[int64|string]>
```
