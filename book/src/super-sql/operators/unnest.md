# unnest

[✅](../intro.md#data-order)&ensp; expand nested array as values optionally into a subquery

## Synopsis

```
unnest [<name>:=]<expr> [ with <assignments> ] [ into ( <query> ) ]
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

The value of `<expr>` must be an array (or a set), whose elements form the
derived sequence.  A `null` or a plain `none` value (e.g., from
[ok](../functions/generics/ok.md) or `?.`) produces no values.

To carry other values along with each unnested element, give the unnested
value a name with `:=` or list the values in a `with` clause.  Then each
derived value is a record whose first field is an element and whose other
fields are the `with` values, which are copied to each derived value, where
each field's name is given with `:=` or, as for [put](put.md), derived from
its expression (e.g., `a.b` is named `b` and `this` is named `that`).  For
example,
```
unnest item:=a with outer:=this
```
produces, for each element `<elem>` of `a`,
```
{item:<elem>,outer:<this>}
```

To explode the leaf values of records, arrays, sets, and maps along with their
paths, use the [`flatten`](../functions/records/flatten.md) function, which
produces an array that can be unnested.

For example, if `this` is a record, it can be unnested with `unnest flatten(this)`.

## Errors

If the value to unnest is not an array or set, then an error results as follows:
```
error({message:"unnest: encountered non-array value",on:<value>})
```
where `<value>` is the offending value.

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
unnest a with s into ( sum(a) by s )
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
unnest f:=flatten(r) with s into ( values {s,key:f.path[0],val:f.value} )
# input
{s:"foo",r:{a:1,b:2}}
{s:"bar",r:{a:3,b:4}}
# expected output
{s:"foo",key:"a",val:1}
{s:"foo",key:"b",val:2}
{s:"bar",key:"a",val:3}
{s:"bar",key:"b",val:4}
```
