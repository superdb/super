# unnest

[✅](../intro.md#data-order)&ensp; expand nested array as values optionally into a subquery

## Synopsis

```
unnest <expr> [ into ( <query> ) ]
```

## Description

The `unnest` operator transforms the given expression
`<expr>` into a new ordered sequence of derived values.

When the optional argument `<query>` is present,
each unnested sequence of values is processed as a unit by that subquery,
which is shorthand for this pattern
```
unnest [unnest <expr> | <query>]
```
where the right-hand `unnest` is an
[array subquery](../expressions/subqueries.md#array-subqueries).

For example,
```
values [1,2],[3] | unnest this | sum(this)
```
produces
```
6
```
but
```
values [1,2],[3] | unnest this into (sum(this))
```
produces
```
3
3
```

If `<expr>` is an array, then the elements of that array form the derived sequence.

If `<expr>` is a record, it must have two fields of the form:
```
{<first>: <any>, <second>:<array>}
```
where `<first>` and `<second>` are arbitrary field names, `<any>` is any
SuperSQL value, and `<array>` is an array value.  In this compound form of `unnest`, the derived
sequence has the form:
```
{<first>: <any>, <second>:<elem0>}
{<first>: <any>, <second>:<elem1>}
...
```
where the first field is copied to each derived value and the second field is
the unnested elements of the array `elem0`, `elem1`, etc.

To explode the leaf values of records, arrays, sets, and maps along with their
paths, use the [`flatten`](../functions/records/flatten.md) function, which
produces an array that can be unnested.

For example, if `this` is a record, it can be unnested with `unnest flatten(this)`.

## Errors

If a value encountered by `unnest` does not have either of the forms defined
above, then an error results as follows:
```
error({message:"unnest: encountered non-array value",on:<value>})
```
where `<value>` is the offending value.

When a record value is encountered without the proper form, then the error is:
```
error({message:"unnest: encountered record without two fields",on:<value>})
```
or
```
error({message:"unnest: encountered record without an array/set type for second field",on:<value>})
```

## Examples

---

_unnest unrolls the elements of an array_
```mdtest-spq
# spq
unnest [1,2,"foo"]
# input

# expected output
1
2
"foo"
```

---

_The unnest clause is evaluated once per each input value_
```mdtest-spq
# spq
unnest [1,2]
# input
null
null
# expected output
1
2
1
2
```

---

_Unnest traversing an array inside a record_
```mdtest-spq
# spq
unnest a
# input
{a:[1,2,3]}
# expected output
1
2
3
```

---

_Filter the unnested values_
```mdtest-spq
# spq
unnest a | this % 2 == 0
# input
{a:[6,5,4]}
{a:[3,2,1]}
# expected output
6
4
2
```

---

_Aggregate the unnested values_
```mdtest-spq
# spq
unnest a | sum(this)
# input
{a:[1,2]}
{a:[3,4,5]}
# expected output
15
```

---

_Aggregate the unnested values in a subquery_
```mdtest-spq
# spq
unnest a into ( sum(this) )
# input
{a:[1,2]}
{a:[3,4,5]}
# expected output
3
12
```

---

_Access an outer value in a subquery_
```mdtest-spq
# spq
unnest {s,a} into ( sum(a) by s )
# input
{a:[1,2],s:"foo"}
{a:[3,4,5],s:"bar"}
# expected output
{s:"foo",sum:3}
{s:"bar",sum:12}
```

---

_Unnested the elements of a record by flattening it_
```mdtest-spq
# spq
unnest {s,f:flatten(r)} into ( values {s,key:f.path[0],val:f.value} )
# input
{s:"foo",r:{a:1,b:2}}
{s:"bar",r:{a:3,b:4}}
# expected output
{s:"foo",key:"a",val:1}
{s:"foo",key:"b",val:2}
{s:"bar",key:"a",val:3}
{s:"bar",key:"b",val:4}
```
