# Statements

A SuperSQL _script_ is a sequence of statements separated by semicolons.
A statement is either a [query](../queries.md) or one of these commands:

* [CREATE TABLE](create-table.md) creates a table whose rows have typed columns,
* [CREATE SUPER TABLE](create-super-table.md) creates a table whose values
  have any super-structured type,
* [DROP TABLE](drop-table.md) removes a table, and
* [INSERT](insert.md) adds the results of a query to a table.

The commands operate on the tables of a [database](../../database/intro.md)
and so run only with [`super db`](../../command/db.md), or through the
database's [API](../../database/api.md).  Queries may appear in a script
run by either `super` or `super db`.

[Declarations](../declarations/intro.md) at the top of a script
are in scope for every statement in the script, e.g., a named type declared
at the top may be used as a column type in `CREATE TABLE`.  A declaration
may be followed by a semicolon.

## Execution

The statements of a script run one after the other, each to completion
before the next begins, so a statement sees the effects of the statements
before it.

A script stops at the first statement that fails.  The effects of the
statements that ran before it remain, as each `INSERT` commits its data
when it completes.  A script is not a transaction.

## Status Records

When a script comprises more than one statement or includes a command,
each statement is followed by a _status record_ in the output, after
any values the statement itself produces.  A status record has the form
```
{
  kind: string,
  stmt: string,
  start: time,
  end: time,
  info: <info>,
  error: string
}
```
where
* `kind` is `create_table`, `drop_table`, `insert`, or `query`,
* `stmt` is the text of the statement,
* `start` and `end` are the times the statement started and finished,
* `info` is a record describing what the statement did, or null if it failed, and
* `error` is the error message if the statement failed and null otherwise.

The fields of `info` depend on the kind of statement:

| kind | info |
|------|------|
| `create_table` | `table` name, table `id`, and `created`, which is false if the table already existed and `IF NOT EXISTS` was given |
| `drop_table` | `table` name, table `id`, and `dropped`, which is false if the table did not exist and `IF EXISTS` was given |
| `insert` | `table` name, number of `values` inserted, and the ID of the `commit` |
| `query` | number of `values` produced |

The error of a failed statement is also reported on standard error and
`super db` exits with a non-zero status.

A script consisting of a single query produces just the query's results
without a status record.

## Examples

The examples in this section create a database in the current directory
with [`super db init`](../../command/db.md#super-db-init) and then
[attach](../../command/db.md#super-db-attach) to it so that subsequent
`super db` commands use it.

---

_A script that creates a table, inserts into it, and queries it_

The start and end times and the IDs vary from run to run so they are
dropped from the status records here.
```mdtest-command
super db init -q db
super db attach -q db
super db -s -c "
  create table people (name string not null, age int64);
  insert into people values ('alice', 33), ('bob', 41);
  select name from people where age > 35
" | super -s -c 'drop start,end,info.id,info.commit' -
```
```mdtest-output
{kind:"create_table",stmt:"create table people (name string not null, age int64)",info:{table:"people",created:true},error:null}
{kind:"insert",stmt:"insert into people values ('alice', 33), ('bob', 41)",info:{table:"people",values:2::uint64},error:null}
{name:"bob"}
{kind:"query",stmt:"select name from people where age > 35",info:{values:1::uint64},error:null}
```

---

_A script stops at the first statement that fails_

```mdtest-command fails
super db -c "
  create table t (x int64);
  insert into t values (1);
  insert into t values ('one');
  insert into t values (2)
" > /dev/null
```
```mdtest-output
INSERT column "x": error({message:"cannot cast to int64",on:"one"})
```
The first `INSERT` committed its value but the last never ran:
```mdtest-command
super db -s -c 'from t'
```
```mdtest-output
{x:1}
```
