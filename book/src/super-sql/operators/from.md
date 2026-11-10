# from

[✅](../intro.md#data-order)[🎲](../intro.md#data-order)&ensp; source data from databases, files, or URLs

## Synopsis

```
from <entity> [ ( <options> ) ]
from <named-query>
```
where `<entity>` has the form of
* a [text entity](../queries.md#text-entity) representing a file, URL, or table,
* an [f-string](../expressions/f-strings.md) representing a file, URL, or table,
* a [glob](../queries.md#glob) matching files in the local file system or tables in a database, or
* a [regular expression](../queries.md#regular-expression) matching tables in a database;

`<options>` is an optional concatenation of named [options](#options); and,

`<named-query>` is an identifier referencing a
[declared query](../declarations/queries.md).

## Description

The `from` operator identifies one or more sources of data as input to
a query and transmits the data required by the query to its output.

Unlike the [FROM](../sql/from.md) clause in a [SQL query](../sql/intro.md),
the pipe `from` merely sources data to its downstream operators
and does not include relational joins or table subqueries, .

As a pipe operator,
`from` preserves the order of the data within a file,
URL, or a sorted table but when multiple sources are identified
(e.g., as a file-system glob or regular expression matching tables),
the data may be read in parallel and interleaved in an undefined order.

Optional arguments to `from` may be appended as a parenthesized concatenation of named [arguments](#options).

### Entity Syntax

How the entity is interpreted depends on whether the query is run
attached to or detached from a [database](../../command/db.md).

When detached from a database, the entity must be a
[text entity](../queries.md#text-entity),
[f-string](../expressions/f-strings.md), or
[glob](../queries.md#glob).
A glob matches [files](#files) in the file system
while a text entity or f-string
is an [URL](#urls) if it parses as an URL; otherwise, it is presumed to be a file path.

When attached to a database, the entity must be a
[text entity](../queries.md#text-entity),
[f-string](../expressions/f-strings.md),
[glob](../queries.md#glob), or a slash-delimitated
[regular expression](../queries.md#regular-expression).
A regular expression matches [tables](#tables) in the attached database.
A text entity or f-string is an [URL](#urls) if it parses as an URL and otherwise,
is presumed to be a table.

Local files are not accessible when attached to a database.

> [!NOTE]
> While table names and file names have overlapping syntax,
> their use is disambiguated by the presence or absence of an attached
> database.

When the entity is a text entity recognized as a file,
the `from` operator reads data from its upstream pipe operator.
For each input value, the file is scanned once and the data is fed to
the output of `from`.  If `from` is the first operator, it receives
exactly one `null` value as input so it scans the file exactly once.

When the entity is a glob recognized as a file-system glob,
the `from` operator reads data from its upstream pipe operator.
For each input value, all matching files are scanned once and the data
is fed to the output of `from`.  If `from` is the first operator, it receives
exactly one `null` value as input so it scans all matching files exactly once.

When the entity is an [f-string](../expressions/f-strings.md),
the `from` operator reads data from its upstream pipe operator
and for each input value, the f-string expression is evaluated and
used as the `<entity>` string argument.  Each such entity is scanned
one at a time and the data is fed to the output of `from`.
When an entity does not exist, a structured error is produced and
the query continues execution.

### Options

Options to `from` may be appended as a parenthesized list of name/value pairs
having the form:
```
( <name> <value> [ <name> <value> ... ] )
```
Each entity type supports a specific set of named options as described below.
When the entity comprises multiple sources (e.g., with a glob), then the
options apply to every entity matched.

### Format Detection

When reading data from files or URLs, the serialization format of the
input data is determined by the presence of a
[well-known extension](../../command/formats.md)
(e.g., `.json`, `.sup`, etc.) on the file path or URL,
or if the extension is not present or unknown, the format is
[inferred](../../command/input.md#format-detection)
by inspecting the input data.

To manually specify the format of a source and override these heuristics,
a format argument may be appended as an argument and has the form
```
format <fmt>
```
where `<fmt>` is the name of a supported
[serialization format](../../command/formats.md) and is
parsed as a [text entity](../queries.md#text-entity).

### Files

When the `<entity>` argument is recognized as a file, the file
data required for the query
is read from the local file system, parsed as its specified or
detected serialization format,  and emitted to its output.

File-system paths are interpreted relative to the directory in which
the [super](../../command/super.md) command is running.

The only allowed option for file entities is the
[format](#format-detection) option described above.

Here are some examples of file syntax:
```
from file.json
from 'file-with-dash.sup'
from /path/to/file.csv
from file*.parq (format parquet)
```

### Tables

When the `<entity>` argument is recognized as a [database](../../command/db.md) table,
the data required for the query is read from the database and
emitted to its output.

The only allowed option for a table is the commit argument having the form
```
commit <commitsh>
```
where `<committish>` specifies a specific
commit in the table's log thereby allowing time travel.

The the commit argument may be abbreviated by appending to the table name
an `@` character followed by the committish, e.g.,
```
from Table (commit 36AwHUt9s8usF7pi9x3l6LOl8IB)
```
maybe be instead written as
```
from Table@36AwHUt9s8usF7pi9x3l6LOl8IB
```
When a single table is specified without a `commit` option, or
when using a regular expression, the tip of the `main` branch
of each table is accessed.

It is an error to specify a format option when the entity is
is a table.

>[!NOTE]
> Metadata from database tables also may be sourced using `from`.
> This will be documented in a future release of SuperDB.

### URLs

Data sources identified by URLs can be accessed either when attached
or detached from a database.

As a [text entity](../queries.md#text-entity), typical URLs need not be quoted though URLs with special characters must be quoted.

When the `<entity>` argument begins with `http:` or `https:`
and has the form of a valid URL, then the source is fetched remotely
using either HTTP or HTTPS.

When the URL begins with `s3:` then data is fetched via
the Amazon S3 object service using the settings defined
by a [local configuration](../../dev/integrations/s3.md).

Named options for URL entities include `format`, `method`, `headers`,
and `body` as in
```
from <url> [ ( format <fmt> method <method> headers <headers> body <body> ) ]
```
where
* `<method>` is one of `GET`, `PUT`, `POST`, or `DELETE` given as a
[text entity](../queries.md#text-entity) or an
[f-string](../expressions/f-strings.md),
* `<headers>` is a [record expression](../types/record.md) that defines the names and values to be included as HTTP header options, and
* `<body>` is an expression of type `string` or `bytes`
to be included as the body of the HTTP request (e.g., use
[json_stringify](../functions/strings/json_stringify.md) to send a record as JSON).

Each field of the `headers` record must either be a string or (to specify a
header option appearing multiple times with different values)
an array or set of strings.

These options cannot be used with S3 URLs.

When `from` has no parent, the method, headers, and body must be
compile-time constants.  When `from` has a parent, the URL, which may be
a constant or a computed [f-string](../expressions/f-strings.md) entity,
is fetched for each input value and the options are evaluated
for each input value, so they may refer to the input value as in
```
values {token:"abc",query:{q:"x"}}
| from "https://example.com/api" (method POST headers {Authorization:"Bearer " || token} body json_stringify(query))
```
In this case, a computed URL must begin with `http:` or `https:` when
any of these options are present or an error results.

A null or none value for an option, a header field, or an element
of a header field's array is ignored.

A response with an HTTP status other than 2xx is an error.

### Combining Data

To combine data from multiple sources using pipe operators, `from` may be
used in combination with other operators like [`fork`](fork.md) and [`join`](join.md).

For example, multiple tables can be accessed in parallel
and combined in undefined order:
```
fork
  ( from TableOne | op1 | op2 | ... )
  ( from TableTwo | op1 | op2 | ... )
| ...
```
or joined according to a join condition:
```
fork
  ( from TableOne | op1 | op2 | ... )
  ( from TableTwo | op1 | op2 | ... )
| join as {left,right} on left.key=right.key
| ...
```
Alternatively, the right-hand leg of the join may be written as a subquery
of join:
```
from TableOne | op1 | op2 | ...
| join ( from TableTwo | op1 | op2 | ... )
    as {left,right} on left.key=right.key
| ...
```

## File Examples

---

_Source structured data from a JSON file_

```mdtest-command
echo '{"greeting":"hello world!"}' > hello.json
super -s -c 'from hello.json | values greeting'
```
```mdtest-output
"hello world!"
```

---

_Source super-structured data from a local file_
```mdtest-command
echo '1 2 {x:1} {s:1::(int64|string)} {s:"hello"::(int64|string)}' > vals.sup
super -s -c 'from vals.sup'
```
```mdtest-output
1
2
{x:1}
{s:1::(int64|string)}
{s:"hello"::(int64|string)}
```

---

_Source data from a file repeatedly_
```mdtest-command
echo '1 2' > vals.sup
super -s -c 'values 0,0,0 | from vals.sup'
```
```mdtest-output
1
2
1
2
1
2
```

---

## HTTP Example

---

_Source data from a URL_
```
super -s -c 'from https://api.github.com/repos/superdb/super | values name'
```
```
"super"
```

---

## F-String Examples

_Read from dynamically defined files and add a column_

```mdtest-command
echo '{a:1}{a:2}' > a.sup
echo '{b:3}{b:4}' > b.sup
echo '"a.sup" "b.sup"' | super -s -c "from f'{this}' | c:=coalesce(a,b)+1" -
```
```mdtest-output
{a:1,c:2}
{a:2,c:3}
{b:3,c:4}
{b:4,c:5}
```

---

## Database Examples

The remaining examples below assume the existence of the SuperDB database
created and populated by the following commands:

```mdtest-command
super db -q init example
super db -q attach example
super db -q create -orderby flip:desc coinflips
echo '{flip:1,result:"heads"} {flip:2,result:"tails"}' |
  super db load -q -use coinflips -
super db branch -q -use coinflips trial
echo '{flip:3,result:"heads"}' | super db load -q -use coinflips@trial -
super db -q create numbers
echo '{number:1,word:"one"} {number:2,word:"two"} {number:3,word:"three"}' |
  super db load -q -use numbers -
super db -f line -c '
  from :branches
  | values table.name || "@" || branch.name
  | sort'
```

The database then contains the two tables and three branches:

```mdtest-output
coinflips@main
coinflips@trial
numbers@main
```

The following file `hello.sup` is also used.

```mdtest-input hello.sup
{greeting:"hello world!"}
```

_Source data from the `main` branch of a table_
```mdtest-command
super db -attach example -s -c 'from coinflips'
```
```mdtest-output
{flip:2,result:"tails"}
{flip:1,result:"heads"}
```

---

_Source data from a specific branch of a table_
```mdtest-command
super db -attach example -s -c 'from coinflips@trial'
```
```mdtest-output
{flip:3,result:"heads"}
{flip:2,result:"tails"}
{flip:1,result:"heads"}
```

---

_Count the number of values in the `main` branch of all tables_
```mdtest-command
super db -attach example -s -c 'from * | count()'
```
```mdtest-output
5
```

---

_Join the data from multiple tables_

```mdtest-command
super db -attach example -s -c '
  from coinflips
  | join ( from numbers ) on left.flip=right.number
  | values {...left, word:right.word}
  | sort'
```
```mdtest-output
{flip:1,result:"heads",word:"one"}
{flip:2,result:"tails",word:"two"}
```

---

_Use `pass` to combine our join output with data from yet another source_
```mdtest-command
super db -attach example -s -c '
  from coinflips
  | join ( from numbers ) on left.flip=right.number
  | values {...left, word:right.word}
  | fork
    ( pass )
    ( from coinflips@trial
      | c:=count()
      | values f"There were {c} flips" )
  | sort this'
```
```mdtest-output
"There were 3 flips"
{flip:1,result:"heads",word:"one"}
{flip:2,result:"tails",word:"two"}
```

---

_Use an f-string to source data from table names computed at run time_
```mdtest-command
super db -attach example -s -c '
  from :tables
  | from f"{name}"
  | sort this'
```
```mdtest-output
{flip:1,result:"heads"}
{flip:2,result:"tails"}
{number:1,word:"one"}
{number:2,word:"two"}
{number:3,word:"three"}
```

---
