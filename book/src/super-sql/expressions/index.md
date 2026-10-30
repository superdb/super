# Index

The index operation is denoted with square brackets and can be applied to
any indexable data type and has the form:
```
<entity> [ <index> ]
```
where `<entity>` is an [expression](intro.md) resulting in an indexable entity
and `<index>` is an [expression](intro.md) resulting in a value that is
used to index the indexable entity.

Indexable entities include
[records](../types/record.md),
[arrays](../types/array.md),
[sets](../types/set.md),
[maps](../types/map.md),
[type values](../types/type.md),
[strings](../types/string.md), and
[bytes](../types/bytes.md).

If `<entity>` is a record, then the `<index>` operand
must be coercible to a string and the result is the record's field
of that name.

If `<entity>` is an array, then the `<index>` operand
must be coercible to an integer and the result is the
value in the array of that index.

If `<entity>` is a set, then the `<index>` operand
must be coercible to an integer and the result is the
value in the set of that index ordered by total order of values.

If `<entity>` is a map, then the `<index>` operand
is presumed to be a key and the corresponding value for that key is
the result of the operation.  If no such key exists in the map, then
the result is `none`.

If `<entity>` is a type value, then a string `<index>` is a field name and
the result is the same as the [dot operator](dot.md) with that field name,
while an integer `<index>` is the position of a field of a record type or
selects the element type of an array or set type.  For example,
`<{x:int64,y:string}>["y"]`, `<{x:int64,y:string}>[1]`, and `<[string]>[0]`
are all `<string>`.

If `<entity>` is a string, then the `<index>` operand
must be coercible to an integer and the result is an integer representing
the unicode code point at that offset in the string.

If `<entity>` is type `bytes`, then the `<index>` operand
must be coercible to an integer and the result is an unsigned 8-bit integer
representing the byte value at that offset in the bytes sequence.

>[!NOTE]
> Indexing of strings and bytes is not yet implemented.

## Index Base

Indexing in SuperSQL is 0-based meaning the first element is at index `0` and
the last element is at index `n-1` for an entity of size `n`.

If 1-based indexing is desired, a scoped language [pragma](../declarations/pragmas.md) may be used
to specify either 1-based indexing or mixed indexing.  In mixed indexing,
0-based indexing is used for expressions appearing in [pipe operators](../operators/intro.md) and
1-based indexing is used for expressions appearing in [SQL operators](../sql/intro.md).

>[!NOTE]
> Mixed indexing is not yet implemented.

## Examples

---

_Simple index_

```mdtest-spq
# spq
values a[2]
# input
{a:[1,2,3,4]}
{a:set[1,2,3,4]}
{a:"1234"}
{a:0x01020304}
# expected output
3
3
error({message:"value cannot be indexed",on:"1234"})
error({message:"value cannot be indexed",on:0x01020304})
```

---

_One-based indexing_

```mdtest-spq
# spq
pragma index_base = 1
values a[2]
# input
{a:[1,2,3,4]}
{a:set[1,2,3,4]}
{a:"1234"}
{a:0x01020304}
# expected output
2
2
error({message:"value cannot be indexed",on:"1234"})
error({message:"value cannot be indexed",on:0x01020304})
```

---

_Index from end of entity_

```mdtest-spq
# spq
values a[-1]
# input
{a:[1,2,3,4]}
{a:set[1,2,3,4]}
{a:"1234"}
{a:0x01020304}
# expected output
4
4
error({message:"value cannot be indexed",on:"1234"})
error({message:"value cannot be indexed",on:0x01020304})
```

---

_Index a type value_

```mdtest-spq
# spq
values this["y"], this[0]
# input
<{x:int64,y:string}>
# expected output
<string>
<int64>
```
