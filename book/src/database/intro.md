# Database

A SuperDB database stores super-structured data persistently in _tables_.
You manage and query a database with the [`super db`](../command/db.md)
command, with SuperSQL [statements](../super-sql/statements/intro.md)
like `CREATE TABLE` and `INSERT`, or over HTTP with the database
[API](api.md).

>[!NOTE]
> While `super` and its accompanying formats are production quality,
> SuperDB's database is earlier in development.  It works well at small
> scale and at larger scales when automation manages the layout of the
> data through the [API](api.md), but its storage format and API may
> still change between releases.

A database lives entirely at its _storage path_, which is a directory
on a local file system or a location in cloud storage (currently
[Amazon S3](../dev/integrations/s3.md)).  All of its data and metadata
are stored there as [BSUP](../formats/bsup.md) objects: each table in the
[SuperTable](format.md#supertable) format and the database's list of tables in
the [SuperCatalog](format.md#supercatalog) format.  There is no separate
metastore or catalog service to set up, as the catalog is just objects at the
storage path, and a copy of the storage path is a complete copy of the database.

## Getting Started

Create a database with [`super db init`](../command/db.md#super-db-init) and
[attach](../command/db.md#super-db-attach) to it so later `super db`
commands use it:
```mdtest-command
super db init -q mydb
super db attach -q mydb
super db -c "create table people (name string not null, city string)" > /dev/null
echo '{name:"alice",city:"Berkeley"} {name:"bob",city:"Oakland"}' |
  super db load -q -use people -
super db -s -c "insert into people values ('carol', 'Berkeley')" > /dev/null
super db -s -c "select city, count(*) as n from people group by city order by city"
```
```mdtest-output
{city:"Berkeley",n:2}
{city:"Oakland",n:1}
```

Here, [`CREATE TABLE`](../super-sql/statements/create-table.md) created a table,
[`super db load`](../command/db.md#super-db-load) loaded data into it
from standard input, [`INSERT`](../super-sql/statements/insert.md) added
another row, and a SQL query read it back.  Queries on a database
read tables with [FROM](../super-sql/sql/from.md) or the
[from](../super-sql/operators/from.md) operator.

## Tables

A database is made up of tables.  A table has a name, which can be any
UTF-8 string, and a unique ID assigned when it is created.
A table can be referred to by either and can be
[renamed](../command/db.md#super-db-rename) while its ID stays the same.

Unlike a relational table in traditional SQL, a table in SuperDB need not have a schema.
There are three kinds of tables, which differ in what values they accept:

* A table created by [`CREATE TABLE`](../super-sql/statements/create-table.md)
  with columns has a record type and behaves like a SQL table:
  values are cast to the columns' types and missing values in nullable columns are null.
* A table created by [`CREATE SUPER TABLE`](../super-sql/statements/create-super-table.md)
  or [`super db create -type`](../command/db.md#super-db-create) has a
  type of any kind, e.g., a union of record types, and values are cast to it.
* A table created by `CREATE TABLE` without columns or by `super db create`
  without `-type` has no type and holds values of any type as they are.

In a table with a type, a value that cannot be cast to the type
fails the whole load and nothing is committed.

### Sort Key

A table may be organized by a _sort key_, which is set when the table is
created with the `ORDER BY` clause of `CREATE TABLE` or the `-orderby` option
of `super db create`, and cannot be changed.
The sort key is typically a field of the table's records, e.g., a timestamp
`ts` for time series data, and may be ascending or descending.

A query with a filter on the sort key scans only the data objects that
could hold matching values.  For example, on a table with sort key `ts`,
the query `ts >= 2026-01-01T00:00:00Z` skips objects whose values of
`ts` are all earlier.  Scans that follow the table's sort order are
generally more efficient than scans in the opposite order.

Values that lack the sort key are treated as having a null key and
can't benefit from range scans.  A table without a sort key is unsorted.

## Branches and Commits

A database follows a model loosely based on [Git](https://git-scm.com/):
* a _table_ is like a repository,
* a _branch_ of a table is like a Git branch,
* [`super db use`](../command/db.md#super-db-use) is like `git checkout`, and
* [`super db load`](../command/db.md#super-db-load) or `INSERT` is like
  `git add`, `git commit`, and `git push` in one step.

Every table has a branch called `main`, which is used whenever a branch
isn't given, so you can ignore branches entirely until you need them.
New branches are created with [`super db branch`](../command/db.md#super-db-branch)
and merged with [`super db merge`](../command/db.md#super-db-merge).

### Commit Objects

Data is added to and deleted from a branch in atomic units called
_commits_.  Each commit is identified by a unique ID and points to its
parent, so the commits of a table form a tree, and a branch is a named
pointer to a commit.  A commit is transactionally consistent: each commit
provides a consistent view of an arbitrary amount of data at a point in time.

Many commands refer to a commit or a branch with a
[_committish_](../command/db.md#committish) of the form `<table>@<branch>`
or `<table>@<commit-id>`.

The commit history of a branch can be displayed with
[`super db log`](../command/db.md#super-db-log) or queried with the
[meta-query](../command/db.md#meta-queries) `from <table>:log`.

## Time Travel

Because commits are immutable, a query sees its data as a fixed snapshot
as of the commit it reads.  If a writer commits data while a reader is
scanning, the reader does not see the new data.

A query can read any past commit by giving its ID after the table name
in [from](../super-sql/operators/from.md).  Continuing the example above,
the first commit has only the two loaded rows:
```mdtest-command
first=$(super db -f line -c 'from people:log | tail 1 | values id')
super db -s -c "from people@$first | sort name"
```
```mdtest-output
{name:"alice",city:"Berkeley"}
{name:"bob",city:"Oakland"}
```
As long as its data has not been [vacuumed](../command/db.md#super-db-vacuum),
any past snapshot can be queried.

## Storage

The storage layer is designed for cloud object stores and separates
compute from storage.  A database comprises one SuperCatalog, which records
its tables and their configuration, and one SuperTable for each table, which
holds the table's data objects, commits, and branches.
All of these objects are stored as BSUP and, apart from the heads of the
journals that track tables and branches, are immutable, so caching them is easy
and backup, migration, and replication are simple copies.
The [format](format.md) section describes the layout in detail.

Data accumulates in a table as a set of data objects.  Over time, many
small or overlapping objects can make scans less efficient, so
[`super db manage`](../command/db.md#super-db-manage) (or
`super db serve -manage`) compacts them, and
[`super db vacuum`](../command/db.md#super-db-vacuum) removes objects that
are no longer referenced.

## Accessing a Database

`super db` finds the database to use in one of two ways:
1. the `-attach` option, which applies to that command only, or otherwise
2. the database most recently attached with
   [`super db attach`](../command/db.md#super-db-attach).

There is no default database.  The [init](../command/db.md#super-db-init) and
[serve](../command/db.md#super-db-serve) commands instead take the
database's storage path as an argument.

Depending on the database it is attached to, `super db` acts in one of
three ways:

* _Direct access_ - When the database is a storage path (a file system path
  or an `s3` URL), `super db` commands operate directly on the storage.
* _Client_ - When the database is an HTTP or HTTPS URL, `super db` commands
  are sent to a database service at that URL.
* _Server_ - [`super db serve`](../command/db.md#super-db-serve) runs a
  service that handles [API](api.md) requests for the database at a
  storage path.

Any number of processes can access the same storage at once, e.g.,
a `super db serve` along with direct-access `super db` commands, and the
database remains consistent.

>[!NOTE]
> Transactional consistency is not yet fully implemented for S3, so only
> single-node access to S3 is supported for now.  Multi-process,
> single-node access to a local file system is well tested.
