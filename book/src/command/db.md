# super db

`super db` is a sub-command of [super](super.md) to manage and query SuperDB databases.

>[!NOTE]
> The database portion of SuperDB is early in development.  While previous versions
> have been deployed in production at non-trivial scale, the current version
> is somewhat out of date with recent changes to the runtime.  This will be remedied
> in forthcoming releases.

The `super db` command is invoked either by itself to run a query:
```
super db -c <query> | -I <query-file> [ options ]
```
or with a [sub-command](#sub-commands):
```
super db <sub-command> [options]...
```

By default, commands that display database metadata
(e.g., [log](#super-db-log) or [ls](#super-db-ls))
use a text format.  However, the `-f` option can be used
to specify any supported [output format](formats.md).

## Concepts

A SuperDB database resides in a directory in a storage system located by its path,
called the _storage path_, which may be either:
* a local file system defined by the path to the directory containing the database, or
* cloud storage as defined by a URL indicating the root location of the database.

>[!NOTE]
> Currently, only [S3](../dev/integrations/s3.md) is supported for cloud storage.
> Support is very early and little work has been done on optimizing S3 performance.

The contents of the database are entirely defined by the data located
at its storage path and requires no auxiliary databases or other third-party services
to interpret the database.

### Storage Layer

Data is arranged in a database as a set of _pools_, which are comprised of one
or more _branches_, which consist of a sequence of _commits_.

The database storage model is intended to perform well with cloud object stores.
All of the meta-data describing the pools, branches, commit history,
and so forth is stored as objects inside of the database.  There is no need
to set up and manage an auxiliary metadata store or catalog.

Commits are immutable and named with globally unique IDs,
and many commands may reference various database entities by their ID, e.g.,
* _Pool ID_ - the ID of a pool
* _Commit object ID_ - the ID of a commit object
* _Data object ID_ - the ID of a committed data object

Data is added and deleted from a database only with new commits that
are transactionally consistent at the level of a commit.  Thus, each
commit point provides a completely
consistent view of an arbitrary amount of committed data
at a specific point in time.

### Database Connection

`super db` may run queries directly on the storage layer or via a service endpoint.

A service endpoint is instantiated with the [serve](#super-db-serve) subcommand.

You _connect_ to a database by setting the
`SUPER_DB` environment variable to point at a particular database,
which may be:
* the storage path of a database (i.e., S3 URL or file system path), or
* an HTTPS URL that points to a SuperDB service endpoint.

In the case of a service endpoint, authentication may be performed with the
[super db auth](#super-db-auth) command.

>[!NOTE]
> A future release of SuperDB will introduce `db connect` and `db disconnect` commands,
> which will include optional authentication and replace `db auth`.

### Committish

Many `super db` commands operate with respect to a commit.
While commit objects are always referenceable by their commit ID, it is also convenient
to refer to the commit object at the tip of a branch.

The entity that represents either a commit ID or a branch is called a _committish_.
A committish is always relative to the pool and has the form:
* `<pool>@<id>` or
* `<pool>@<branch>`

where `<pool>` is a pool name or pool ID, `<id>` is a commit ID,
and `<branch>` is a branch name.

In particular, the working branch set by the [use](#super-db-use) sub-command
is a committish.

A committish may be abbreviated in several ways where the missing detail is
obtained from the working-branch committish, e.g.,
* `<pool>` - When just a pool name is given, then the committish is assumed to be
`<pool>@main`.
* `@<id>` or `<id>`- When an ID is given (optionally with the `@` prefix), then the committish is assumed to be `<pool>@<id>` where `<pool>` is obtained from the working-branch committish.
* `@<branch>` - When a branch name is given with the `@` prefix, then the committish is assumed to be `<pool>@<id>` where `<pool>` is obtained from the working-branch committish.

### Sort Key

Data in a pool may be organized with a sort key to improve performance for certain use
cases.  The sort key may be ascending or descending.

The sort key is specified with the [create](#super-db-create) sub-command.

For example, a time series database with time represented by a timestamp
`ts` could use `ts` as the sort key.

## Running a Query

When `super db` is invoked without a `db` sub-command and
a query is specified, `super db` executes the
SuperDB query engine attached to the database storage layer.

In addition to inputs from database pools, URLs may be referenced
within the query text as described in [input data section](input.md).

For built-in command help and a listing of all available options,
simply run `super db` without any arguments.

When running a query attached to a database, the options include:

* [Global](options.md#global)
* [Database](options.md#database)
* [Query](options.md#query)
* [Input](options.md#input)
* [Output](options.md#output)

A database query typically begins with a [from](../super-sql/operators/from.md) operator
indicating the pool and branch to use as input.
If a pool name is provided to `from` without a branch name, then branch
"main" is assumed.

Output is sent to a [file or stdout](output.md).

This example reads every record from the full key range of the `logs` pool
and sends the results to stdout.

```
super db -c 'from logs'
```

We can narrow the span of the query by specifying a filter on the database
[sort key](#sort-key):
```
super db -c 'from logs | ts >= 2018-03-24T17:36:30.090766Z and ts <= 2018-03-24T17:36:30.090758Z'
```

When querying data to the [BSUP](../formats/bsup.md) output format,
output from a pool can be easily piped to other commands like `super`, e.g.,
```
super db -f bsup -c 'from logs' | super -s -c 'count() by field' -
```
Of course, it's even more efficient to run the query inside of the pool traversal
like this:
```
super db -s -c 'from logs | count() by field'
```
By default, the `query` command scans pool data in sort-key order though
the query optimizer may, in general, reorder the scan to optimize searches,
aggregations, and joins.

#### Meta-queries

Commit history, metadata about data objects, database and pool configuration,
etc. can all be queried and
returned as super-structured data, which in turn, can be queried.

These structures are introspected using meta-queries that simply
specify a metadata source using an extended syntax in the `from` operator.
There are three types of meta-queries:
* `from :<meta>` - database level
* `from pool:<meta>` - pool level
* `from pool[@<branch>]<:meta>` - branch level

`<meta>` is the name of the metadata being queried. The available metadata
sources vary based on level.

For example, a list of pools with configuration data can be obtained
in the SUP format as follows:
```
super db -S -c "from :pools"
```
This meta-query produces a list of branches in a pool called `logs`:
```
super db -S -c "from logs:branches"
```
You can filter the results just like any query,
e.g., to look for particular branch:
```
super db -S -c "from logs:branches | branch.name=='main'"
```

This meta-query produces a list of the data objects in the `live` branch
of pool `logs`:
```
super db -S -c "from logs@live:objects"
```

You can also pretty-print in human-readable form most of the metadata records
using the "db" format, e.g.,
```
super db -f db -c "from logs@live:objects"
```

The `main` branch is queried by default if an explicit branch is not specified,
e.g.,

```
super db -f db -c "from logs:objects"
```

## Sub-commands

* [auth](#super-db-auth) authentication and authorization commands
* [branch](#super-db-branch) create a new branch in a pool
* [compact](#super-db-compact) compact data objects on a pool branch
* [create](#super-db-create) create a new pool in a database
* [delete](#super-db-delete) delete data from a pool
* [drop](#super-db-drop) remove a pool from a database
* [init](#super-db-init) create and initialize a new database
* [load](#super-db-load) load data into database
* [log](#super-db-log) display the commit log
* [ls](#super-db-ls) list the pools in a database
* [manage](#super-db-manage) run regular maintenance on a database
* [merge](#super-db-merge) merged data from one branch to another
* [rename](#super-db-rename) rename a database pool
* [revert](#super-db-revert) reverse an old commit
* [serve](#super-db-serve)  run a SuperDB service endpoint
* [use](#super-db-use) set working branch for `db` commands
* [vacate](#super-db-vacate) truncate a pool's commit history by removing old commits
* [vacuum](#super-db-vacuum) vacuum deleted storage in database

### super db auth

```
super db auth login|logout|method|verify
```
* [Global](options.md#global)
* [Database](options.md#database)

> **TODO: rename this command. it's really about connecting to a database.
> authenticating is something you do to connect.**
* login - log in to a database service and save credentials
* logout - remove saved credentials for a database service
* method - display authentication method supported by database service
* verify - verify authentication credentials

### super db branch
```
super db branch [options] [name]
```
* `-use <committish>` commit to use, i.e., pool, pool@branch, or pool@commit
* [Global](options.md#global)
* [Database](options.md#database)
* [Output](options.md#output)

The `branch` command creates a branch with the name `name` that points
to the tip of the working branch or, if the `name` argument is not provided,
lists the existing branches of the selected pool.

For example, this branch command
```
super db branch -use logs@main staging
```
creates a new branch called "staging" in pool "logs", which points to
the same commit object as the "main" branch.  Once created, commits
to the "staging" branch will be added to the commit history without
affecting the "main" branch and each branch can be queried independently
at any time.

Supposing the `main` branch of `logs` was already the working branch,
then you could create the new branch called "staging" by simply saying
```
super db branch staging
```
Likewise, you can delete a branch with `-d`:
```
super db branch -d staging
```
No data is
deleted by this operation and the deleted branch can be easily recreated by
running the branch command again with the commit ID desired.

You can list the branches as follows:
```
super db branch
```

If no branch is currently checked out, then "-use pool@base" can be
supplied to specify the desired pool for the new branch.

### super db compact

```
super db compact id id [ id... ]
```
* [Global](options.md#global)
* [Database](options.md#database)
* [Output](options.md#output)
* [Commit](options.md#commit)

The `compact` command takes a list of one or more
data object IDs, writes the values
in those objects to a sequence of new, non-overlapping objects, and
creates a commit on HEAD replacing the old objects with the new ones.

### super db create

```
super db create [-orderby key[,key...][:asc|:desc]] <name>
```
* `-orderby key` pool key with optional :asc or :desc suffix to organize data in pool (cannot be changed) (default "ts:desc")
* `-S size` target size of pool data objects, as '10MB' or '4GiB', etc. (default "500MiB")
* `-use` set created pool as the current pool (default "false")
* [Global](options.md#global)
* [Database](options.md#database)
* [Output](options.md#output)

The `create` command creates a new data pool with the given name,
which may be any valid UTF-8 string.

The `-orderby` option indicates the [sort key](#sort-key) that is used to sort
the data in the pool, which may be in ascending or descending order.

If a sort key is not specified, then it defaults to
the [special value `this`](../super-sql/intro.md#pipe-scoping).

A newly created pool is initialized with a branch called `main`.

> [!NOTE]
> Pools can be used without thinking about branches.  When referencing a pool without
> a branch, the tooling presumes the "main" branch as the default, and everything
> can be done on main without having to think about branching.

### super db delete

```
super db delete [options] <id> [<id>...]
super db delete [options] -where <filter>
```
* `-use committish` commit to use, i.e., pool, pool@branch, or pool@commit
* `-where predicate` delete by any SuperSQL predicate
* [Global](options.md#global)
* [Database](options.md#database)
* [Output](options.md#output)
* [Commit](options.md#commit)

The `delete` command removes one or more data objects indicated by their ID from a pool.
This command
simply removes the data from the branch without actually deleting the
underlying data objects thereby allowing time travel to work in the face
of deletes.  Permanent deletion of underlying data objects is handled by the
separate [vacuum](#super-db-vacuum) command.

If the `-where` flag is specified, delete will remove all values for which the
provided filter expression is true.  The value provided to `-where` must be a
single filter expression, e.g.:

```
super db delete -where 'ts > 2022-10-05T17:20:00Z and ts < 2022-10-05T17:21:00Z'
```

### super db drop

```
super db drop [options] <name>|<id>
```
* `-f` do not prompt for confirmation
* [Global](options.md#global)
* [Database](options.md#database)
* [Output](options.md#output)

The `drop` command deletes a pool and all of its constituent data.

**DANGER ZONE.**  You must confirm that you want to delete
the pool to proceed.  The `-f` option can be used to force the deletion
without confirmation.

### super db init

```
super db init [path]
```
* [Global](options.md#global)
* [Database](options.md#database)

A new database is created and initialized with the `init` command.
The `path` argument is a storage path
and is optional.  If not present, the path
is [determined automatically](#database-connection).

If the database already exists, `init` reports an error and does nothing.

Otherwise, the `init` command writes the initial cloud objects to the
storage path to create a new, empty database at the specified path.

### super db load

```
super db load [options] input [input ...]
```
* `-use <committish>` commit to use, i.e., pool, pool@branch, or pool@commit
* [Global](options.md#global)
* [Database](options.md#database)
* [Input](options.md#input)
* [Output](options.md#output)

The `load` command commits new data to a branch of a pool.

Run `super db load -h` for a list of command-line options.

Note that there is no need to define a schema or insert data into
a "table" as all super-structured data is _self describing_ and can be queried in a
schema-agnostic fashion.  Data of any _shape_ can be stored in any pool
and arbitrary data _shapes_ can coexist side by side.

As with [super](super.md),
the [input arguments](super.md#options) can be in
any [supported format](formats.md) and
the input format is auto-detected if `-i` is not provided.  Likewise,
the inputs may be URLs, in which case, the `load` command streams
the data from a Web server or [S3](../dev/integrations/s3.md)
and into the database.

When data is loaded, it is broken up into objects of a target size determined
by the pool's `threshold` parameter (which defaults to 500MiB but can be configured
when the pool is created).  Each object is sorted by the [sort key](#sort-key) but
a sequence of objects is not guaranteed to be globally sorted.  When lots
of small or unsorted commits occur, data can be fragmented.  The performance
impact of fragmentation can be eliminated by regularly [compacting](#super-db-manage)
pools.

For example, this command
```
super db load sample1.json sample2.bsup sample3.sup
```
loads files of varying formats in a single commit to the working branch.

An alternative branch may be specified with a branch reference with the
`-use` option, i.e., `<pool>@<branch>`.  Supposing a branch
called `live` existed, data can be committed into this branch as follows:
```
super db load -use logs@live sample.bsup
```
Or, as mentioned above, you can set the default branch for the load command
via the [use](#super-db-use) sub-command:
```
super db use logs@live
super db load sample.bsup
```
During a `load` operation, a commit is broken out into units called _data objects_
where a target object size is configured into the pool,
typically 100MB-1GB.  The records within each object are sorted by the sort key.
A data object is presumed by the implementation
to fit into the memory of an intake worker node
so that such a sort can be trivially accomplished.

Data added to a pool can arrive in any order with respect to its sort key.
While each object is sorted before it is written,
the collection of objects is generally not sorted.

Each load operation creates a single commit,
which includes:
* an author and message string,
* a timestamp computed by the server, and
* an optional metadata field of any type expressed as a Super (SUP) value.
This data has the type signature:
```
{
    author: string,
    date: time,
    message: string,
    meta: <any>
}
```
where `<any>` is the type of any optionally attached metadata .
For example, this command sets the `author` and `message` fields:
```
super db load -user user@example.com -message "new version of prod dataset" ...
```
If these fields are not specified, then the system will fill them in
with the user obtained from the session and a message that is descriptive
of the action.

The `date` field here is used by the database for time travel
through the branch and pool history, allowing you to see the state of
branches at any time in their commit history.

Arbitrary metadata expressed as any [SUP value](../formats/sup.md)
may be attached to a commit via the `-meta` flag.  This allows an application
or user to transactionally commit metadata alongside committed data for any
purpose.  This approach allows external applications to implement arbitrary
data provenance and audit capabilities by embedding custom metadata in the
commit history.

Since commit objects are stored as super-structured data, the metadata can easily be
queried by running the `log -f bsup` to retrieve the log in BSUP format,
for example, and using [super](super.md) to pull the metadata out
as in:
```
super db log -f bsup | super -c 'has(meta) | values {id,meta}' -
```

### super db log

```
super db log [options] [committish]
```
* [Global](options.md#global)
* [Database](options.md#database)
* [Output](options.md#output)

The `log` command, like `git log`, displays a history of the commits
starting from any commit, expressed as a [committish](#committish).  If no argument is
given, the tip of the working branch is used.

Run `super db log -h` for a list of command-line options.

To understand the log contents, the `load` operation is actually
decomposed into two steps under the covers:
an "add" step stores one or more
new immutable data objects in the pool and a "commit" step
materializes the objects into a branch with an ACID transaction.
This updates the branch pointer to point at a new commit object
referencing the data objects where the new commit object's parent
points at the branch's previous commit object, thus forming a path
through the object tree.

The `log` command prints the commit ID of each commit object in that path
from the current pointer back through history to the first commit object.

A commit object includes
an optional author and message, along with a required timestamp,
that is stored in the commit journal for reference.  These values may
be specified as options to the [load](#super-db-load) sub-command,
and are also available in the SuperDB API for automation.

>[!NOTE]
> The branchlog meta-query source is not yet implemented.

### super db ls

```
super db ls [options] [pool]
```
* [Global](options.md#global)
* [Database](options.md#database)
* [Output](options.md#output)

The `ls` command lists pools in a database or branches in a pool.

By default, all pools in the database are listed along with each pool's unique ID
and [sort key](#sort-key)

If a pool name or pool ID is given, then the pool's branches are listed along
with the ID of their commit object, which points at the tip of each branch.

### super db manage

```
super db manage [options]
```
* `-config path` path of manage YAML config file
* `-interval duration` interval between updates (applicable only with -monitor)
* `-log.devmode` development mode
* `-log.filemode`
* `-log.level level` logging level (default "info")
* `-log.path path` path to send logs (values: stderr, stdout, path in file system) (default "stderr")
* `-monitor` continuously monitor the database for updates
* `-pool pool` pool to manage (all if unset, can be specified multiple times)
* `-vectors` create vectors for objects
* [Global](options.md#global)
* [Database](options.md#database)
* [Output](options.md#output)

The `manage` command performs maintenance tasks on a database.

Currently the only supported task is _compaction_, which reduces fragmentation
by reading data objects in a pool and writing their contents back to large,
non-overlapping objects.

If the `-monitor` option is specified and the database is
[configured](#database-connection)
via service connection, `super db manage` will run continuously and perform updates
as needed.  By default a check is performed once per minute to determine if
updates are necessary.  The `-interval` option may be used to specify an
alternate check frequency as a [duration](../super-sql/types/time.md).

If `-monitor` is not specified, a single maintenance pass is performed on the
database.

By default, maintenance tasks are performed on all pools in the database.  The
`-pool` option may be specified one or more times to limit maintenance tasks
to a subset of pools listed by name.

The output from `manage` provides a per-pool summary of the maintenance
performed, including a count of `objects_compacted`.

As an alternative to running `manage` as a separate command, the `-manage`
option is also available on the [serve](#super-db-serve) sub-command to have maintenance
tasks run at the specified interval by the service process.

### super db merge

```
super db merge -use logs@updates <branch>
```
* `-f` force merge of main into a target (default "false")
* `-use <committish>` commit to use, i.e., pool, pool@branch, or pool@commit
* [Global](options.md#global)
* [Database](options.md#database)
* [Output](options.md#output)
* [Commit](options.md#commit)

Data is merged from one branch into another with the `merge` command, e.g.,
```
super db merge -use logs@updates main
```
where the `updates` branch is being merged into the `main` branch
within the `logs` pool.

A merge operation finds a common ancestor in the commit history then
computes the set of changes needed for the target branch to reflect the
data additions and deletions in the source branch.
While the merge operation is performed, data can still be written concurrently
to both branches and queries performed and everything remains transactionally
consistent.  Newly written data remains in the
branch while all of the data present at merge initiation is merged into the
parent.

This Git-like behavior for a database provides a clean solution to
the live ingest problem.
For example, data can be continuously ingested into a branch of `main` called `live`
and orchestration logic can periodically merge updates from branch `live` to
branch `main`, possibly [compacting](#super-db-manage) data after the merge
according to configured policies and logic.

### super db rename

```
super db rename <existing> <new-name>
```
* [Global](options.md#global)
* [Database](options.md#database)
* [Output](options.md#output)

The `rename` command assigns a new name `<new-name>` to an existing
pool `<existing>`, which may be referenced by its ID or its previous name.

### super db revert

```
super db revert committish
```
* `-use <committish>` commit to use, i.e., pool, pool@branch, or pool@commit
* [Global](options.md#global)
* [Database](options.md#database)
* [Query](options.md#query)
* [Output](options.md#output)
* [Commit](options.md#commit)

The `revert` command reverses the actions in a commit by applying the
inverse steps in a new commit to the tip of the indicated branch.  Any
data loaded in a reverted commit remains in the database but no longer
appears in the branch. The new commit may recursively be reverted by an
additional revert operation.

### super db serve

```
super db serve [options]
```

* `-auth.audience` [Auth0](https://auth0.com/) audience for API clients (will be publicly accessible)
* `-auth.clientid` [Auth0](https://auth0.com/) client ID for API clients (will be publicly accessible)
* `-auth.domain` [Auth0](https://auth0.com/) domain (as a URL) for API clients (will be publicly accessible)
* `-auth.enabled` enable authentication checks
* `-auth.jwkspath` path to JSON Web Key Set file
* `-cors.origin` CORS allowed origin (may be repeated)
* `-defaultfmt` default response format (default "sup")
* `-l [addr]:port` to listen on (default ":9867")
* `-log.devmode` development mode (if enabled dpanic level logs will cause a panic)
* `-log.filemod` logger file write mode (values: append, truncate, rotate)
* `-log.level` logging level
* `-log.path` path to send logs (values: stderr, stdout, path in file system)
* `-manage duration` when positive, run database maintenance tasks at this interval
* `-rootcontentfile` file to serve for GET /
* [Global](options.md#global)
* [Database](options.md#database)

The `super db serve` command runs continuously and services
SuperDB API requests on the interface and port
specified by the `-l` option, executes the requests, and returns results.

The `-log.level` option controls log verbosity.  Available levels, ordered
from most to least verbose, are `debug`, `info` (the default), `warn`,
`error`, `dpanic`, `panic`, and `fatal`.  If the volume of logging output at
the default `info` level seems too excessive for production use, `warn` level
is recommended.

The `-manage` option enables the running of the same maintenance tasks
normally performed via the [manage](#super-db-manage) sub-command.

### super db use

```
super db use [ <committish> ]
```
* [Global](options.md#global)
* [Database](options.md#database)

The `use` command sets the working branch to the indicated committish.
When run with no argument, it displays the working branch and
[database connection](#database-connection).

Setting these values allows commands like load, rebase, merge, etc. to function without
having to specify the working branch.  The branch specifier may also be
a commit ID, in which case you enter a headless state and commands
like load that require a branch will report an error.

The use command is like "git checkout" but there is no local copy of
the database.  Rather, the local HEAD state influences commands as
they access the database.

The pool must be the name or ID of an existing pool.  The branch must be
the name of an existing branch or a commit ID.

Any command that relies upon HEAD can also be run with the `-use` option
to refer to a different HEAD without executing an explicit `use` command.
While the use of HEAD is convenient for interactive CLI sessions,
automation and orchestration tools are better off hard-wiring the
HEAD references in each database command via `-use`.

The `use` command merely checks that the branch exists and updates the
file ~/.super_head.  This file simply contains a pointer to the HEAD branch
and thus provides the default for the `-use` option.  This way, multiple working
directories can contain different HEAD pointers (along with your local files)
and you can easily switch between windows without having to continually
re-specify a new HEAD.  Unlike Git, all the committed pool data remains
in the database and is not copied to this local directory.

For example,
```
super db use logs
```
provides a "pool-only" committish that sets the working branch to `logs@main`.

If an `@branch` or commit ID are given without a pool prefix, then the pool of
the committish previously in use is presumed.  For example, if you are on
`logs@main` then run this command:
```
super db use @test
```
then the working branch is set to `logs@test`.

To specify a branch in another pool, simply prepend
the pool name to the desired branch:
```
super db use otherpool@otherbranch
```
This command stores the working branch in `$HOME/.super_head`.

### super db vacate

```
super db vacate [options] [timestamp]
```
**Options**
* `-use <committish>` commit to use, i.e., pool, pool@branch, or pool@commit
* [Global](options.md#global)
* [Database](options.md#database)
* [Output](options.md#output)
* [Commit](options.md#commit)

The `vacate` command truncates a pool's commit history by removing old commits.

If `-use` is specified, all commits timestamped older than that of the
given committish will be deleted. If the optional `timestamp` argument is
supplied, all commits older than that timestamp will be deleted. If
neither `-use` nor `timestamp` is given, only the most recent commit in the
history will be kept and all others deleted.

**DANGER ZONE.** Once the pool's commit history has been truncated and old
commits are deleted, they cannot be recovered.  You must confirm that you want
to remove the commits to proceed.  The `-f` option can be used to force
removal without confirmation.  The `-dryrun` option may also be used to see a
summary of how many commits would be removed by a `vacate` but without
removing them.

### super db vacuum

```
super db vacuum [ options ]
```
* `-dryrun` run vacuum without deleting anything
* `-f` do not prompt for confirmation
* `-use <committish>` commit to use, i.e., pool, pool@branch, or pool@commit
* [Global](options.md#global)
* [Database](options.md#database)
* [Output](options.md#output)

The `vacuum` command permanently removes underlying data objects that have
previously been subject to a [delete](#super-db-delete) sub-command.

**DANGER ZONE.** You must confirm that you want to remove
the objects to proceed.  The `-f` option can be used to force removal
without confirmation.  The `-dryrun` option may also be used to see a summary
of how many objects would be removed by a `vacuum` but without removing them.
