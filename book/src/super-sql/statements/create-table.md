# CREATE TABLE

```
CREATE TABLE [ IF NOT EXISTS ] <name>
  [ ( <column> <type> [ NOT NULL | NULL ] [ , ... ] ) ]
  [ ORDER BY <key> [ ASC | DESC ] [ , ... ] ]
```

## Description

`CREATE TABLE` creates a table called `<name>` in the database
with a `main` branch and no data.  `<name>` is an identifier or a
quoted string.  It is an error if the table already exists unless
`IF NOT EXISTS` is given, in which case the statement does nothing.

When columns are given, the table has a _table type_, which is a
[record type](../types/record.md) with a field for each column, in order.
A `<type>` may be any [SuperSQL type](../types/intro.md), including
the SQL type names (e.g., `INTEGER` or `VARCHAR`), unions, and
[named types](../types/named.md) declared at the top of the script.
A length on a type, as in `VARCHAR(20)`, is accepted and ignored.

A column is nullable unless it is declared `NOT NULL`, i.e., the type
of a nullable column `<column> <type>` is `<type>|null`.
`NULL` explicitly declares a column nullable.

Every value added to a table with a table type, whether by
[INSERT](insert.md), [`super db load`](../../command/db.md#super-db-load),
or the [load](../operators/load.md) operator,
is [cast](../expressions/cast.md) to the table type as in SQL:
* a column missing from a value is null if the column is nullable,
  and is otherwise an error,
* a field of a value that is not a column is an error, and
* a column value that cannot be cast to the column's type is an error.

A value that cannot be cast to the table type fails the whole `INSERT` or
load and nothing is committed.

When no columns are given, the table has no table type and values of
any type are added to it as is, just like a table created by
[`super db create`](../../command/db.md#super-db-create).
To create a table whose values must conform to a type other than a
record of columns, use [CREATE SUPER TABLE](create-super-table.md).

The optional `ORDER BY` clause sets the table's
[sort key](../../database/intro.md#sort-key), which organizes the
table's data for efficient range scans.  Each `<key>` is a field path
and is ascending unless `DESC` is given.  `NULLS FIRST` and `NULLS LAST`
are not supported here.  A table without `ORDER BY` is unsorted.
The sort key cannot be changed after the table is created.

## Examples

The first example creates a database in the current directory with
[`super db init`](../../command/db.md#super-db-init) and
[attaches](../../command/db.md#super-db-attach) to it, and the
examples that follow use it.

---

_A table with typed columns, including a named type, sorted by `src` in descending order_

```mdtest-command
super db init -q db
super db attach -q db
super db -c "
  type port = uint16;
  create table conns (src ip not null, dport port, note varchar(20)) order by src desc;
  insert into conns values (10.0.0.1, 80, 'web'), (10.0.0.2, 443);
  insert into conns (note, src) values ('dns', 10.0.0.3)
" > /dev/null
super db -s -c 'from conns'
```
```mdtest-output
{src:10.0.0.3,dport:null,note:"dns"}
type port=uint16
{src:10.0.0.2,dport:443::port,note:null}
{src:10.0.0.1,dport:80::port,note:"web"}
```

---

_A NOT NULL column must have a value_

```mdtest-command fails
super db -c "insert into conns (dport) values (22)" > /dev/null
```
```mdtest-output
INSERT has no value for NOT NULL column "src"
```

---

_A field that isn't a column can't be added_

```mdtest-command fails
super db -c "insert into conns values {src:10.0.0.9,proto:'tcp'}" > /dev/null
```
```mdtest-output
value {src:10.0.0.9,proto:"tcp"} cannot be cast to table type {src:ip,dport:port|null,note:string|null}: no field proto in type
```

---

_A table without columns holds values of any type_

```mdtest-command
super db -c "
  create table misc;
  insert into misc values 1, 'two', {three:3}
" > /dev/null
super db -s -c 'from misc'
```
```mdtest-output
1
"two"
{three:3}
```

---

_`IF NOT EXISTS` leaves an existing table alone_

```mdtest-command
super db -s -c "create table if not exists misc (x int64)" |
  super -s -c 'values info.created' -
```
```mdtest-output
false
```
