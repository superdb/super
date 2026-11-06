# SuperDB [![Tests][tests-img]][tests] [![GoPkg][gopkg-img]][gopkg]

[SuperDB](https://superdb.org/)
is a new analytics database that supports relational tables and JSON
on an equal footing.  It shines when it comes to data wrangling where
you need to explore or process large eclectic data sets.  It's also pretty
decent at analytics and
[search use cases](https://superdb.org/super-sql/operators/search.html).

Unlike other relational systems that do performance-fragile "schema inference" of JSON,
SuperDB won't fall over if you throw a bunch of eclectic JSON at it.
You can easily do
[schema inference if you want](https://superdb.org/super-sql/operators/fuse.html),
but data is ingested by default in its natural form no matter how much heterogeneity
it might have.  And unlike systems based on the document data model,
every value in SuperDB is strongly and dynamically typed thus providing the
best of both worlds: the flexibility of the document model and
the performance and type soundness of the relational model.

In SuperDB's SQL dialect, there are no "JSON columns" so there isn't a "relational
way to do things" and a different "JSON way to do things".  Instead of having
a relational type system for structured data and completely separate JSON type
system for semi-structured data,
all data handled by SuperDB (e.g., JSON, CSV, Parquet files, Arrow streams, relational tables, etc) is automatically massaged into
[super-structured data](https://superdb.org/intro.html#super-structured-data)
form.  This super-structured data is then processed by a runtime that simultaneously
supports the statically-typed relational model and the dynamically-typed
JSON data model in a unified compute engine.

## SuperSQL

SuperDB uses SQL as its query language, but it's a SQL that has been extended
with [pipe syntax](https://research.google/pubs/sql-has-problems-we-can-fix-them-pipe-syntax-in-sql/)
and [lots of fun shortcuts](https://superdb.org/super-sql/operators/intro.html#shortcuts).
This extended SQL is called [SuperSQL](https://superdb.org/super-sql/intro.html).

Here's a SuperSQL query that fetches some data from GitHub Archive,
computes the set of repos touched by each user, ranks them by number of repos,
picks the top five, and joins each user with their original `created_at` time
from the current GitHub API:

```sql
FROM 'https://data.gharchive.org/2015-01-01-15.json.gz'
| SELECT union(repo.name) AS repos, actor.login AS user
  GROUP BY user
  ORDER BY len(repos) DESC
  LIMIT 5
| FORK
  ( FROM f'https://api.github.com/users/{user}'
    SELECT login AS user, created_at::timestamp AS created_at )
  ( PASS )
| JOIN USING (user)
| VALUES {...left,repos:right.repos}
```

## Super (SUP) Format

Super-structured data is strongly typed even though any value can take on any type
and sequences of data need not all conform to a predefined schema.  To achieve this,
SuperDB extends the JSON format with explicit typing in a format called
[Super (SUP)](https://superdb.org/formats/sup.html) where all JSON values
are also SUP.  The text-based SUP format isn’t designed for performance; it’s meant to be human-readable at small scale, but the
binary version of SUP called [Super Binary (BSUP)](https://superdb.org/formats/bsup.html)
is highly efficient, supporting both a row and a column orientation.
The BSUP row form is a bit like [Avro](https://avro.apache.org/) while the column form
is a bit like [Parquet](https://parquet.apache.org/)
or [Arrow](https://arrow.apache.org/).

Even though SuperDB is based on these super-structured data formats, it can read and write
most common data formats.

## Project Status

Our long-term goal for SuperDB is to provide a flexible foundation for
databases that subsumes both the relational and document data models.
While SuperSQL is intended to be (mostly)
[Postgres-compatible](https://superdb.org/super-sql/sql/intro.html#sql-compatibility)
and interoperate with existing SQL tooling, we believe its type safety and
algebraic expressiveness will cover a broader range of use cases compared to
legacy SQL or document-based query languages.

In the meantime, SuperDB is stabilizing but still a bit of a moving
target.  We would love [your engagement](#join-the-community) to evolve and fine tune its
syntax and semantics.

## Try It

SuperDB is embodied in a dependency-free binary that is
[easy to install](https://superdb.org/getting-started/install.html).

Detailed documentation
for the SuperDB system and its piped SQL syntax is available on the
[SuperDB docs site](https://superdb.org).

If you're so inclined,
we'd love to hear your feedback via our
[community Slack](https://www.brimdata.io/join-slack/).

>**NOTE:** The SuperDB query engine can run locally without a storage engine by accessing
>files, HTTP endpoints, or S3 paths using the `super` command. While
>earlier in its development, SuperDB can also run on a
>[super-structured database](https://superdb.org/command/db)
>using the `super db` sub-commands.  The database format is a bit like
>[Iceberg](https://iceberg.apache.org/) but instead of rigid schemas, it supports
>flexible table shapes based on super-structured types.

### SuperDB Desktop - Coming Soon

[SuperDB Desktop](https://github.com/superdb/zui) is a
desktop app to explore, query, and shape data in a SuperDB database.
It combines a search experience with SQL that is well suited
for dealing with complex and large JSON data.

Unlike most JSON browsing tools, it won't slow to a crawl --- or worse crash ---
if you load it up with ginormous JSON values.

## Contributing

See the [contributing guide](CONTRIBUTING.md) on how you can help improve SuperDB!

## Join the Community

Join our [public Slack](https://www.brimdata.io/join-slack/) workspace for announcements, Q&A, and to trade tips!

[tests-img]: https://github.com/superdb/super/workflows/Tests/badge.svg
[tests]: https://github.com/superdb/super/actions?query=workflow%3ATests
[gopkg-img]: https://pkg.go.dev/badge/github.com/superdb/super
[gopkg]: https://pkg.go.dev/github.com/superdb/super
