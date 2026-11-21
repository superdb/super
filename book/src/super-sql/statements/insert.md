# INSERT

```
INSERT INTO <name> [ ( <column> [ , ... ] ) ] <query>
```

## Description

`INSERT` runs `<query>` and adds its results to the `main` branch of the
table `<name>` in a single commit.  `<name>` is an identifier or a quoted
string.  `<query>` may be any [query](../queries.md): a SQL
[VALUES](../sql/values.md) clause, a [SELECT](../sql/select.md), or a pipe
query.  As in SQL, a parenthesized query that is the entire body of the
`INSERT` is the query itself rather than a subquery expression.

The results are added to the table in one of two ways:

* When a column list is given or `<query>` is a SQL query
  (e.g., `VALUES` tuples or `SELECT`), the result columns are assigned
  _by position_ to the listed columns, or to all of the table's columns
  in order if there is no list.  Each value is cast to its column's
  type and columns without a value are null.
  It is an error if there are more result columns than target columns,
  if a listed column is not in the table, or if a `NOT NULL` column
  gets no value or a null value.
  This requires a table whose type is a record of columns as created by
  [CREATE TABLE](create-table.md).
* Otherwise, the results are added as values, just as
  [`super db load`](../../command/db.md#super-db-load) adds them:
  each value is cast to the table's type _by field name_ as described
  for [CREATE TABLE](create-table.md) and
  [CREATE SUPER TABLE](create-super-table.md),
  or added as is if the table has no type.

If any value can't be added, the `INSERT` fails and nothing is committed.

To add data to a branch other than `main`, use the
[load](../operators/load.md) operator or `super db load`.

## Examples

The examples below use a database with a `pets` table created like this:
```mdtest-command
super db init -q db
super db attach -q db
super db -c "create table pets (name string not null, species string, age int64)" > /dev/null
super db -f line -c 'from :tables | values name'
```
```mdtest-output
pets
```

---

_Insert `VALUES` tuples into all columns or into listed columns_

```mdtest-command
super db -c "
  insert into pets values ('rex', 'dog', 3), ('tom', 'cat', 7);
  insert into pets (name, age) values ('nemo', 1)
" > /dev/null
super db -s -c 'from pets | sort name'
```
```mdtest-output
{name:"nemo",species:null,age:1}
{name:"rex",species:"dog",age:3}
{name:"tom",species:"cat",age:7}
```

---

_Insert the results of a `SELECT`, assigned to columns by position_

```mdtest-command
super db -c "
  insert into pets select name || ' jr', species, 0 from pets where species = 'dog'
" > /dev/null
super db -s -c 'select * from pets where age = 0'
```
```mdtest-output
{name:"rex jr",species:"dog",age:0}
```

---

_Insert records from a pipe query, matched to columns by field name_

```mdtest-command
super db -c "insert into pets values {age:2,species:'bird',name:'polly'}" > /dev/null
super db -s -c "from pets | where species=='bird'"
```
```mdtest-output
{name:"polly",species:"bird",age:2}
```

---

_Too many values for the table's columns_

```mdtest-command fails
super db -c "insert into pets values ('max', 'dog', 4, 'extra')" > /dev/null
```
```mdtest-output
INSERT has more expressions (4) than target columns (3)
```

---

_A `NOT NULL` column must get a value_

```mdtest-command fails
super db -c "insert into pets (species) values ('fish')" > /dev/null
```
```mdtest-output
INSERT has no value for NOT NULL column "name"
```
