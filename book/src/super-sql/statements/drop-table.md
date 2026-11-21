# DROP TABLE

```
DROP TABLE [ IF EXISTS ] <name>
```

## Description

`DROP TABLE` removes the table `<name>`, along with all of its branches,
commit history, and data, from the database.  This cannot be undone.  `<name>` is an identifier or a quoted string.
It is an error if the table does not exist unless `IF EXISTS` is given,
in which case the statement does nothing.

`DROP TABLE` is equivalent to
[`super db drop`](../../command/db.md#super-db-drop).

## Examples

---

_Drop a table, then drop it again with `IF EXISTS`_

```mdtest-command
super db init -q db
super db attach -q db
super db -c "create table t" > /dev/null
super db -s -c "drop table t; drop table if exists t" |
  super -s -c 'values {stmt,dropped:info.dropped}' -
```
```mdtest-output
{stmt:"drop table t",dropped:true}
{stmt:"drop table if exists t",dropped:false}
```

---

_Dropping a table that doesn't exist is an error_

```mdtest-command fails
super db -c "drop table t" > /dev/null
```
```mdtest-output
t: table not found
```
