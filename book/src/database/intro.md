# Database

> **TODO: update and simplify.  add note about readiness**

## Data Tables

## Committish

>[!NOTE]
> While `super` and its accompanying formats
> are production quality, SuperDB's persistent database is still fairly early in development
> and alpha quality.
> That said, SuperDB databases can be utilized quite effectively at small scale,
> or at larger scales when scripted automation
> is deployed to manage the database's data layout via the database
> [API](../database/api.md).
>
> Enhanced scalability with self-tuning configuration is under development.

## Design Philosophy

XXX this section pasted in...fix

The design philosophy for SuperDB is based on composable building blocks
built from self-describing data structures.  Everything in a SuperDB database
is built from super-structured data and each system component can be run and tested in isolation.

Since super-structured data is self-describing, this approach makes stream composition
very easy.  Data from a query can trivially be piped to a local
instance of `super` by feeding the resulting output stream to stdin of `super`, for example,
```
super db query "from table | ...remote query..." | super -c "...local query..." -
```
There is no need to configure the SuperDB entities with schema information
like [protobuf configs](https://developers.google.com/protocol-buffers/docs/proto3)
or connections to
[schema registries](https://docs.confluent.io/platform/current/schema-registry/index.html).

A SuperDB database is completely self-contained, requiring no auxiliary databases
(like the [Hive metastore](https://hive.apache.org/development/gettingstarted))
or other third-party services to interpret the database internals.
Once copied, a new service can be instantiated by pointing a `super db serve`
at the copy of the database.

Functionality like [data compaction](../command/db.md#super-db-manage) and retention are all API-driven.

Bite-sized components are unified by the super-structured data, usually in the BSUP format:
* All database meta-data is available via meta-queries.
* All database operations available through the service API are also available
directly via the `super db` command.
* Database management is agent-driven through the API.  For example, instead of complex policies
like data compaction being implemented in the core with some fixed set of
algorithms and policies, an agent can simply hit the API to obtain the meta-data
of the objects in the database, analyze the objects (e.g., looking for too much
key space overlap) and issue API commands to merge overlapping objects
and delete the old fragmented objects, all with the transactional consistency
of the commit log.
* Components are easily tested and debugged in isolation.

## The Database Model

A SuperDB database is a cloud-native arrangement of data, optimized for search,
analytics, ETL, data discovery, and data preparation
at scale based on data represented in accordance
with the [super-structured data model](../formats/model.md).

A database is organized into a collection of data tables forming a single
administrative domain.  The current implementation supports
ACID append and delete semantics at the commit level while
we have plans to support CRUD updates at the primary-key level
in the near future.

TODO: make tables independent entities then tie them together with a separate
layer of administrative glue (i.e., there should be no dependencies in a table
that are required to interpret and query it outside of the table entity)

TODO: back off on github metaphor?

The semantics of a SuperDB database loosely follows the nomenclature and
design patterns of [`git`](https://git-scm.com/).  In this approach,
* a _database_ is like a GitHub organization,
* a _table_ is like a `git` repository,
* a _branch_ of a _table_ is like a `git` branch,
* the _use_  command is like a `git checkout`, and
* the _load_ command is like a `git add/commit/push`.

A core theme of the SuperDB database design is _ergonomics_.  Given the Git metaphor,
our goal here is that the database tooling be as easy and familiar as Git is
to a technical user.

Since databases are built upon the super-structured data model,
getting different kinds of data into and out of a database is easy.
There is no need to define schemas or tables and then fit
semi-structured data into schemas before loading data into a database.
And because SuperDB supports a large family of formats and the load endpoint
automatically detects most formats, it's easy to just load data into a database
without thinking about how to convert it into the right format.

### CLI-First Approach

The SuperDB project has taken a _CLI-first approach_ to designing and implementing
the system.  Any time a new piece of functionality is added to the database,
it is first implemented as a `super db` command.  This is particularly convenient
for testing and continuous integration as well as providing intuitive,
bite-sized chunks for learning how the system works and how the different
components come together.

While the CLI-first approach provides these benefits,
all of the functionality is also exposed through an [API](../database/api.md) to
a database service.  Many use cases involve an application like
[SuperDB Desktop](https://zui.brimdata.io/) or a
programming environment like Python/Pandas interacting
with the service API in place of direct use with `super db`.

### Storage Layer

The storage model is designed to leverage modern cloud object stores
and separates compute from storage.

A database is entirely defined by a collection of cloud objects stored
at a configured object-key prefix.  This prefix is called the _storage path_.
All of the meta-data describing the tables, branches, commit history,
and so forth is stored as cloud objects inside of the database.  There is no need
to set up and manage an auxiliary metadata store.

Data is arranged in a database as a set of tables, which are comprised of one
or more branches, which consist of a sequence of data [commit objects](#commit-objects)
that point to cloud data objects.

Cloud objects and commits are immutable and named with UUIDs,
and many commands may reference various database entities by their ID, e.g.,
* _Table ID_ - the UUID of a table
* _Commit object ID_ - the UUID  of a commit object
* _Data object ID_ - the UUID  of a committed data object

Data is added and deleted from the database  only with new commits that
are implemented in a transactionally consistent fashion.  Thus, each
commit object (identified by its globally-unique ID) provides a completely
consistent view of an arbitrarily large amount of committed data
at a specific point in time.

While this commit model may sound heavyweight, excellent live ingest performance
can be achieved by micro-batching commits.

Because the database  represents all state transitions with immutable objects,
the caching of any cloud object (or byte ranges of cloud objects)
is easy and effective since a cached object is never invalid.
This design makes backup/restore, data migration, archive, and
replication easy to support and deploy.

The cloud objects that comprise a database, e.g., data objects,
commit history, transaction journals, partial aggregations, etc.,
are stored as super-structured data in [Super Binary (BSUP)](../formats/bsup.md).
This makes introspection of the database structure straightforward as many key
database data structures can be queried with metadata queries and presented
to a client for further processing by downstream tooling.

The implementation also includes a storage abstraction that maps the cloud object
model onto a file system so that databases can also be deployed on standard file systems.

### Command Personalities

The `super db` command provides a single command-line interface to SuperDB databases, but
different personalities are taken on by `super db` depending on the particular
sub-command executed and the database [connection](../command/db.md#database-connection).

To this end, `super db` can take on one of three personalities:

* _Direct Access_ - When the database is a storage path (`file` or `s3` URI),
then the `super db` commands (except for `serve`) all operate directly on the
database located at that path.
* _Client Personality_ - When the database is an HTTP or HTTPS URL, then the
database is presumed to be a service endpoint and the client
commands are directed to the service managing the database.
* _Server Personality_ - When the [`super db serve`](../command/db.md#super-db-serve) command is executed, then
the personality is always the server personality and the database must be
a storage path.  This command initiates a continuous server process
that serves client requests for the database at the configured storage path.

Note that a storage path on the file system is a standard
file system path, relative or absolute, e.g., `/databases/test`.
A fully qualified file URI of the form `file://` is also accepted.

Concurrent access to any database storage, of course, preserves
data consistency.  You can run multiple `super db serve` processes while also
running any `super db` command all pointing at the same storage endpoint
and the database's data footprint will always remain consistent as the endpoints
all adhere to the consistency semantics of the database.

>[!NOTE]
> Transactional data consistency is not fully implemented yet for
> the S3 endpoint so only single-node access to S3 is available right now,
> though support for multi-node access is forthcoming.
> For a shared file system, the close-to-open cache consistency
> semantics of [NFS](https://en.wikipedia.org/wiki/Network_File_System) should provide the
> necessary consistency guarantees needed by
> the database though this has not been tested.  Multi-process, single-node
> access to a local file system has been thoroughly tested and should be
> deemed reliable, i.e., you can run a direct-access instance of `super db` alongside
> a server instance of `super db` on the same file system and data consistency will
> be maintained.

### Locating the Database

`super db` commands locate the database in one of two ways:

1. The database given by the `-attach` option, which applies to that
   command only, or otherwise
2. The currently attached database in `~/.super_attach`, which is written by
   [super db attach](../command/db.md#super-db-attach).

If neither is present, the command reports that no database is attached.
There is no default location.

The [init](../command/db.md#super-db-init) and
[serve](../command/db.md#super-db-serve) commands do not locate a database
this way.  Each takes the path to the root of the database as an argument.

### Tables

A database is made up of _tables_, which are based on super-structured data
and simultaneously resemble relational tables as well
"collections" in NoSQL document stores but in SuperDB all data is strongly typed.  
Tables may have one or more branches and every table always
has a branch called `main`.

A table is created with the [`create` command](../command/db.md#super-db-create)
and a branch of a table is created with the [`branch` command](../command/db.md#super-db-branch).

A table name can be any valid UTF-8 string and is allocated a unique ID
when created.  The table can be referred to by its name or by its ID.
A table may be renamed but the unique ID is always fixed.

#### Commit Objects

Data is added into a table in atomic units called _commit objects_.

Each commit object is assigned a global ID.
Similar to Git, commit objects are arranged into a tree and
represent the entire commit history of the database.

>[!NOTE]
> Technically speaking, Git can merge from multiple parents and thus
> Git commits form a directed acyclic graph instead of a tree;
> SuperDB does not currently support multiple parents in the commit object history.

A branch is simply a named pointer to a commit object in the database
and like a table, a branch name can be any valid UTF-8 string.
Consistent updates to a branch are made by writing a new commit object that
points to the previous tip of the branch and updating the branch to point at
the new commit object.  This update may be made with a transaction constraint
(e.g., requiring that the previous branch tip is the same as the
commit object's parent); if the constraint is violated, then the transaction
is aborted.

The _working branch_ of a table may be selected on any command with the `-use` option
or may be persisted across commands with the [`use` command](../command/db.md#super-db-use) so that
`-use` does not have to be specified on each command-line.  For interactive
workflows, the `use` command is convenient but for automated workflows
in scripts, it is good practice to explicitly specify the branch in each
command invocation with the `-use` option.

#### Committish

Many `super db` commands operate with respect to a commit object.
While commit objects are always referenceable by their commit ID, it is also convenient
to refer to the commit object at the tip of a branch.

The entity that represents either a commit ID or a branch is called a _committish_.
A committish is always relative to the table and has the form:
* `<table>@<id>` or
* `<table>@<branch>`

where `<table>` is a table name or table ID, `<id>` is a commit object ID,
and `<branch>` is a branch name.

In particular, the working branch set by the [`use` command](../command/db.md#super-db-use) is a committish.

A committish may be abbreviated in several ways where the missing detail is
obtained from the working-branch committish, e.g.,
* `<table>` - When just a table name is given, then the committish is assumed to be
`<table>@main`.
* `@<id>` or `<id>`- When an ID is given (optionally with the `@` prefix), then the committish is assumed to be `<table>@<id>` where `<table>` is obtained from the working-branch committish.
* `@<table>` - When a branch name is given with the `@` prefix, then the committish is assumed to be `<table>@<id>` where `<table>` is obtained from the working-branch committish.

An argument to a command that takes a commit object is called a _committish_
since it can be expressed as a branch or as a commit ID.

#### Table Sort Key

Each table is organized according to its configured _sort key_,
As table data is often comprised of [records](../formats/model.md#21-record) (analogous to JSON objects),
the sort key is typically a field of the stored records.
When table data is not structured as records/objects (e.g., scalar or arrays or other
non-record types), then the sort key is sometimes be configured
as the special value [`this`](../super-sql/intro.md#pipe-scoping).

Data can be efficiently scanned if a query has a filter operating on the sort key.
For example, on a table with sort key `ts`, the query `ts == 100`
will be optimized to scan only the data objects where the value `100` could be
present.

A table also has a configured sort order, either ascending or descending
and data is organized in the table in accordance with this order.
Data scans may be either ascending or descending, and scans that
follow the configured order are generally more efficient than
scans that run in the opposing order.

Scans may also be range-limited but unordered.

Any data loaded into a table that lacks the sort key is presumed
to have a null value with regard to range scans.  If large amounts
of such "keyless data" are loaded into a table, the ability to
optimize scans over such data is impaired.

### Time Travel

Because commits are transactional and immutable, a query
sees its entire data scan as a fixed "snapshot" with respect to the
commit history.  In fact, the [`from` operator](../super-sql/operators/from.md)
allows a commit object to be specified with the `@` suffix to a
table reference, e.g.,
```
super db query 'from logs@1tRxi7zjT7oKxCBwwZ0rbaiLRxb | ...'
```
In this way, a query can time-travel through the commit history.  As long as the
underlying data has not been deleted, arbitrarily old snapshots of the
database can be easily queried.

If a writer commits data after or while a reader is scanning, then the reader
does not see the new data since it's scanning the snapshot that existed
before these new writes occurred.

Also, arbitrary metadata can be [committed to the log](../command/db.md#super-db-log),
e.g., to associate derived analytics to a specific
journal commit point potentially across different tables in
a transactionally consistent fashion.

While time travel through commit history provides one means to explore
past snapshots of the commit history, another means is to use a timestamp.
Because the entire history of branch updates is stored in a transaction journal
and each entry contains a timestamp, branch references can be easily
navigated by time.  For example, a list of branches of a table's past
can be created by scanning the internal "tables log" and stopping at the largest
timestamp less than or equal to the desired timestamp.  Then using that
historical snapshot of the tables, a branch can be located within the table
using that table's "branches log" in a similar fashion, then its corresponding
commit object can be used to construct the data of that branch at that
past point in time.

>[!NOTE]
> Time travel using timestamps is a forthcoming feature.
