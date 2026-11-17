# VALUES

A `VALUES` clause has the form
```
VALUES <tuple> [ , <tuple> ... ]
```
where each `<tuple>` has the form
```
( <expr> [ , <expr> ... ] )
```
and `<expr>` is an [expression](../expressions/index.md)
that must evaluate to a compile-time constant.

>[!NOTE]
> SuperSQL currently requires that VALUES expressions be compile-time constants.
> A future version of SuperSQL will support correlated subqueries and lateral
> joins at which time the expressions may refer to relational inputs.

Each tuple in the `VALUES` clause forms a row and the collection of
tuples form a table with an [output scope](intro.md#output-scope)
whose columns are named `column1`, `column2`, etc.

There is no tuple type in SuperSQL.  Instead, the tuple expressions are
translated to a record (i.e., relational row) with column names
`column1`, `column2`, etc.

As it produces an output scope, the result of `VALUES` does not have a
table name.  Typically, a `VALUES` clause is used as a table subquery
in a [FROM](from.md) clause and assigned table and column names with a
[table alias](from.md#table-aliases).

## Examples

---

_Simple `VALUES` operation_
```mdtest-spq
# spq
VALUES ('hello, world')
# input

# expected output
{column1:"hello, world"}
```

---

_As a table subquery_
```mdtest-spq
# spq
SELECT *
FROM (VALUES ('hello, world'),('to be or not to be')) T(message)
# input

# expected output
{message:"hello, world"}
{message:"to be or not to be"}
```

---

_Column variation filled in with null values_
```mdtest-spq
# spq
SELECT * FROM (VALUES (1,2),(3)) T(x,y)
# input

# expected output
{x:1,y:2}
{x:3,y:null}
```
