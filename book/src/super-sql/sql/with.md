# WITH

A [WITH](with.md) clause may precede any
[SQL operator](intro.md#sql-operator) and has the form
```
WITH <alias> AS [ [ NOT ] MATERIALIZED ] (
  <sql-op>
)
[ , <alias> AS [ [ NOT ] MATERIALIZED ] ( <sql-op> ) ... ]
```
where
* `<alias>` is a table alias with optional columns as defined
in a [FROM](from.md#table-aliases) clause, and
* `<sql-op>` is any [SQL operator](intro.md#sql-operator).

`WITH` defines one or more common-table expressions (CTE)
each of which binds a name to the query body defined in the CTE.

A CTE is similar to a [query declaration](../declarations/queries.md)
but the CTE body must be a [SQL operator](intro.md#sql-operator)
and the CTE name can be used only with a [FROM](from.md) clause
and is not accessible in an expression.

Use `MATERIALIZED` to materialize a named query by evaluating it once and
reusing the result on each invocation.  This is useful when a query is expensive
or has output that varies across evaluations.

The table aliases form a lexical scope
which is available in any `FROM` clause defined within the SQL operator
that follows the `WITH` clause and any `FROM` clauses recursively
defined within that operator.  Additionally, a CTE alias is available to
the other CTEs that follow in the same `WITH` clause.

>[!NOTE]
> SuperSQL will support recursive CTEs in a future version.

## Examples

---

_Hello world_
```mdtest-spq
# spq
WITH hello(message) AS (
    VALUES ('hello, world')
)
SELECT * FROM hello
# input

# expected output
{message:"hello, world"}
```

---

_A first CTE referenced in a second CTE_
```mdtest-spq
# spq
WITH T(x) AS (
    VALUES (1), (2), (3)
),
U(y) AS (
    SELECT x+1 FROM T
)
SELECT * FROM U
# input

# expected output
{y:2}
{y:3}
{y:4}
```

---

_A nested CTE reaching into its parent scope_
```mdtest-spq
# spq
WITH T(x) AS (
    VALUES (1), (2), (3)
)
SELECT (
    WITH U(y) AS (
        SELECT x+1 FROM T
    )
    SELECT max(y) FROM U
 ) as max
# input

# expected output
{max:4}
```

---

_A materialized CTE is evaluated once_

```mdtest-spq
# spq
WITH T(x) AS MATERIALIZED (
    VALUES (now())
)
SELECT * FROM T
UNION
SELECT * FROM T
UNION
SELECT * FROM T
| count(distinct this)
# input

# expected output
1
```
