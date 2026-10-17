# Set Operators

Set operators combine two input tables produced by any
[SQL operator](intro.md#sql-operator)
using set union, set intersection, and set subtraction.

A set operation has the form
```
<sql-op> UNION [ALL | DISTINCT] <sql-op>
<sql-op> INTERSECT [ALL | DISTINCT] <sql-op>
<sql-op> EXCLUDE [ALL | DISTINCT] <sql-op>
```
where `<sql-op>` is any [SQL operator](intro.md#sql-operator).

The set operators all have equal precedence and associate left to right.
Parentheses may be used to override the default left-to-right
evaluation order.

The table produced by the first `<sql-op>` is called the _left table_ and
the table produced by the other `<sql-op>` is called the _right table_.

>[!NOTE]
> Only the `UNION` set operator is currently supported.
> The `INTERSECT` AND `EXCLUDE` operators will be available in
> a future version of SuperSQL.

## UNION

The `UNION` operation performs a relational set union between the left and
right tables.

The number of columns in the two tables must be the same but the column
names need not match.  The output table inherits the column names of
the left table and the columns from the right table are merged into the
output based on column position not by name.

If the `ALL` keyword is present, then all rows from both tables are
included in the output.

If the `DISTINCT` keyword is present, then only unique rows are included
in the output.

If neither the `ALL` nor `DISTINCT` keywords are present, then `DISTINCT`
is presumed.

## Non-relational Data

When processing mixed-type tables or non-table inputs, the effect
of union can be achieved by simply combining pipe queries using
[fork](../operators/fork.md).

When it is desirable to have a homogeneous output for such data,
data can be fused into one type with the [fuse](../operators/fuse.md) or
[blend](../operators/blend.md) operators,
which resembles the _union-by-name_ variation available in some SQL dialects.

## Examples

---

_Basic union where column name inherited from left table_

```mdtest-spq
# spq
SELECT 1 as x
UNION
SELECT 2 as y
ORDER BY x
# input

# expected output
{x:1}
{x:2}
```

---

_UNION results are distinct by default_

```mdtest-spq
# spq
SELECT 1 as x
UNION
SELECT 2 as y
UNION
SELECT 2 as z
ORDER BY x
# input

# expected output
{x:1}
{x:2}
```

---

_UNION ALL retains duplicate rows_

```mdtest-spq
# spq
SELECT 1 as x
UNION ALL
SELECT 2 as y
UNION ALL
SELECT 2 as z
ORDER BY x
# input

# expected output
{x:1}
{x:2}
{x:2}
```

---

_Misaligned tables cause a compilation error_

```mdtest-spq fails
# spq
WITH T(x,y) AS (
    VALUES (1,2), (3,4), (5,6)
),
U(z) AS (
    VALUES (2), (3)
)
SELECT * FROM T
UNION ALL
SELECT * from U
# input

# expected output
set operations can only be applied to sources with the same number of columns at line 7, column 1:
SELECT * FROM T
~~~~~~~~~~~~~~~
```

---

_Pad a table to align columns_

```mdtest-spq
# spq
WITH T(x,y) AS (
    VALUES (1,2), (3,4), (5,6)
),
U(z) AS (
    VALUES (2), (3)
)
SELECT * FROM T
UNION ALL
SELECT *, 0 from U
ORDER BY x,y
# input

# expected output
{x:1,y:2}
{x:2,y:0}
{x:3,y:0}
{x:3,y:4}
{x:5,y:6}
```
---

_Blend data as an alternative to a SQL UNION_

```mdtest-spq
# spq
fork
  (
    WITH T(x,y) AS (
      VALUES (1,2), (3,4), (5,6)
    )
    SELECT * FROM T
  )
  (
    WITH U(z) AS (
      VALUES (2), (3)
    )
    SELECT * FROM U
  )
| sort x,z
| blend
# input

# expected output
{x:1::(int64|missing),y:2::(int64|missing),z:none::(int64|missing)}
{x:3::(int64|missing),y:4::(int64|missing),z:none::(int64|missing)}
{x:5::(int64|missing),y:6::(int64|missing),z:none::(int64|missing)}
{x:none::(int64|missing),y:none::(int64|missing),z:2::(int64|missing)}
{x:none::(int64|missing),y:none::(int64|missing),z:3::(int64|missing)}
```
---
