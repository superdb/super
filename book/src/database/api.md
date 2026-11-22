# SuperDB API

A database service started with [`super db serve`](../command/db.md#super-db-serve)
exposes the database over HTTP with the API described here.
The [`super db`](../command/db.md) command uses this API when it is
[attached](intro.md#accessing-a-database) to a service URL.

>[!NOTE]
> The API may change between releases while the database matures.

The examples below use [curl](https://curl.se/) with a service
listening on its default port, 9867, e.g., as started by
```
super db init mydb
super db serve mydb
```

## Endpoints

### Tables

#### Create Table

Create a new table.

```
POST /table
```

**Params**

| Name | Type | In | Description |
| ---- | ---- | -- | ----------- |
| name | string | body | **Required.** Name of the table. Must be unique. |
| sort_keys | [{order:string,path:[string]}] | body | Sort key of the table, where `order` is `asc` or `desc` and `path` is a field path, e.g., `["ts"]` or `["product","serial_number"]`.  Default: unsorted. |
| type | string | body | A [type value](../super-sql/types/type.md) in [SUP](../formats/sup.md) format, e.g., `"<{a:int64}>"`, to which all values loaded into the table are cast as with [CREATE SUPER TABLE](../super-sql/statements/create-super-table.md). Default: no type. |
| object_cap | number | body | Target number of values in each data object. Default: 1048576. |
| frame_cap | number | body | Target number of values in each BSUP frame of a data object. Default: 20000. |
| Content-Type | string | header | [MIME type](#mime-types) of the request payload. |
| Accept | string | header | Preferred [MIME type](#mime-types) of the response. |

**Example Request**

```
curl -X POST \
     -H 'Accept: application/json' \
     -H 'Content-Type: application/json' \
     -d '{"name": "inventory", "sort_keys": [{"order": "asc", "path": ["product"]}]}' \
     http://localhost:9867/table
```

**Example Response**

```
{
  table: {
    ts: "2026-11-21T14:28:00.818576616Z",
    name: "inventory",
    id: "01a1fe81-c8b2-793a-aab3-383076bc5d23",
    sortkeys: [
      {
        order: "asc",
        path: [
          "product"
        ]
      }
    ],
    objectcap: 1048576,
    framecap: 20000,
    type: "0x",
    sql: false
  },
  branch: {
    ts: "2026-11-21T14:28:00.818741489Z",
    name: "main",
    commit: "00000000-0000-0000-0000-000000000000"
  }
}
```

In the response, `type` is the binary encoding of the table's type, which is
empty for a table without a type, and `sql` is true for a table created
by [CREATE TABLE](../super-sql/statements/create-table.md) with columns.

Tables may also be created with a [CREATE TABLE](../super-sql/statements/create-table.md)
or [CREATE SUPER TABLE](../super-sql/statements/create-super-table.md)
statement sent to the [query](#query) endpoint.

---

#### Rename Table

Change a table's name.

```
PUT /table/{table}
```

**Params**

| Name | Type | In | Description |
| ---- | ---- | -- | ----------- |
| table | string | path | **Required.** ID or name of the table. |
| name | string | body | **Required.** The new name of the table. Must be unique. |
| Content-Type | string | header | [MIME type](#mime-types) of the request payload. |

**Example Request**

```
curl -X PUT \
     -H 'Content-Type: application/json' \
     -d '{"name": "catalog"}' \
     http://localhost:9867/table/parts
```

On success, HTTP 204 is returned with no response payload.

---

#### Delete Table

Permanently delete a table and all of its data.

```
DELETE /table/{table}
```

**Params**

| Name | Type | In | Description |
| ---- | ---- | -- | ----------- |
| table | string | path | **Required.** ID or name of the table. |

**Example Request**

```
curl -X DELETE \
     http://localhost:9867/table/catalog
```

On success, HTTP 204 is returned with no response payload.

---

#### Table Stats

Get the total size in bytes of the data objects in a table's `main` branch
and, for a table sorted by a timestamp, the span of time the data covers.

```
GET /table/{table}/stats
```

**Params**

| Name | Type | In | Description |
| ---- | ---- | -- | ----------- |
| table | string | path | **Required.** ID or name of the table. |
| Accept | string | header | Preferred [MIME type](#mime-types) of the response. |

**Example Request**

```
curl -X GET \
     -H 'Accept: application/json' \
     http://localhost:9867/table/inventory/stats
```

**Example Response**

```
{"size":213,"span":null}
```

---

#### Vacuum Table

Free storage space by permanently removing data objects that are no longer
referenced by a branch, e.g., because they were deleted or compacted.

```
POST /table/{table}/revision/{revision}/vacuum
```

**Params**

| Name | Type | In | Description |
| ---- | ---- | -- | ----------- |
| table | string | path | **Required.** ID or name of the table. |
| revision | string | path | **Required.** The starting point for locating objects that can be vacuumed. Can be the name of a branch (whose tip is used) or a commit ID. |
| dryrun | string | query | Set to "T" to return the list of objects that could be vacuumed without removing them. Defaults to "F". |
| Accept | string | header | Preferred [MIME type](#mime-types) of the response. |

**Example Request**

```
curl -X POST \
     -H 'Accept: application/json' \
     http://localhost:9867/table/inventory/revision/main/vacuum
```

**Example Response**

```
{"object_ids":["01a1fe81-c8cb-769c-af2f-918d01841e68"]}
```

---

#### Vacate Table

Truncate a table's commit history by removing the commits older than a
given time.  See [`super db vacate`](../command/db.md#super-db-vacate).

```
POST /table/{table}/vacate
```

**Params**

| Name | Type | In | Description |
| ---- | ---- | -- | ----------- |
| table | string | path | **Required.** ID or name of the table. |
| ts | string | query | **Required.** An RFC 3339 timestamp.  Commits older than this time are removed. |
| dryrun | string | query | Set to "T" to return the list of commits that would be removed without removing them. Defaults to "F". |
| Accept | string | header | Preferred [MIME type](#mime-types) of the response. |

**Example Request**

```
curl -X POST \
     -H 'Accept: application/json' \
     'http://localhost:9867/table/inventory/vacate?ts=2026-11-21T14:28:20Z&dryrun=T'
```

**Example Response**

```
{"commit_ids":["01a1fe81-f08a-7e86-99a1-8428cd2d4d78","01a1fe81-f048-7a41-95e5-0809f98ef7bc","01a1fe81-f03d-7143-9881-c32cb60a0865","01a1fe81-c8cb-7a7a-882d-c0685ab961a7"]}
```

---

### Branches

#### Create Branch

Create a new branch of a table that points to a commit.

```
POST /table/{table}
```

**Params**

| Name | Type | In | Description |
| ---- | ---- | -- | ----------- |
| table | string | path | **Required.** ID or name of the table. |
| name | string | body | **Required.** Name of the new branch. |
| commit | string | body | **Required.** ID of the commit the branch points to, e.g., the commit at the tip of another branch as returned by [Get Branch](#get-branch). |
| Content-Type | string | header | [MIME type](#mime-types) of the request payload. |
| Accept | string | header | Preferred [MIME type](#mime-types) of the response. |

**Example Request**

```
curl -X POST \
     -H 'Accept: application/json' \
     -H 'Content-Type: application/json' \
     -d '{"name": "staging", "commit": "01a1fe81-c8cb-7a7a-882d-c0685ab961a7"}' \
     http://localhost:9867/table/inventory
```

**Example Response**

```
{"ts":"2026-11-21T14:28:10.924125921Z","name":"staging","commit":"01a1fe81-c8cb-7a7a-882d-c0685ab961a7"}
```

---

#### Load Data

Add data to a branch in a new commit and return the commit's ID.
If the table has a type, every value is cast to it, and if any value
can't be cast, the load fails and nothing is committed.

```
POST /table/{table}/branch/{branch}
```

**Params**

| Name | Type | In | Description |
| ---- | ---- | -- | ----------- |
| table | string | path | **Required.** ID or name of the table. |
| branch | string | path | **Required.** Name of branch to which data will be loaded. |
|   | various | body | **Required.** Contents of the posted data. |
| csv.delim | string | query | Exactly one character specifying the field delimiter for CSV data. Defaults to ",". |
| SuperDB-Commit | string | header | A JSON object with optional `author`, `body`, and `meta` strings describing the commit, where `meta` is a value in [SUP](../formats/sup.md) format. |
| Content-Type | string | header | [MIME type](#mime-types) of the posted content. If undefined, the service will attempt to introspect the data and determine the format automatically. |
| Accept | string | header | Preferred [MIME type](#mime-types) of the response. |

**Example Request**

```
curl -X POST \
     -H 'Accept: application/json' \
     -H 'Content-Type: application/json' \
     -H 'SuperDB-Commit: {"author": "alice", "body": "initial inventory"}' \
     -d '{"product": "widget", "warehouse": "chicago"}
         {"product": "gadget", "warehouse": "miami"}
         {"product": "widget", "warehouse": "miami"}' \
     http://localhost:9867/table/inventory/branch/main
```

**Example Response**

```
{"commit":"01a1fe81-c8cb-7a7a-882d-c0685ab961a7","warnings":[]}
```

---

#### Get Branch

Get the ID of the commit at the tip of a branch.

```
GET /table/{table}/branch/{branch}
```

**Params**

| Name | Type | In | Description |
| ---- | ---- | -- | ----------- |
| table | string | path | **Required.** ID or name of the table. |
| branch | string | path | **Required.** Name of branch. |
| Accept | string | header | Preferred [MIME type](#mime-types) of the response. |

**Example Request**

```
curl -X GET \
     -H 'Accept: application/json' \
     http://localhost:9867/table/inventory/branch/main
```

**Example Response**

```
{"commit":"01a1fe81-c8cb-7a7a-882d-c0685ab961a7","warnings":[]}
```

---

#### Delete Branch

Delete a branch.

```
DELETE /table/{table}/branch/{branch}
```

**Params**

| Name | Type | In | Description |
| ---- | ---- | -- | ----------- |
| table | string | path | **Required.** ID or name of the table. |
| branch | string | path | **Required.** Name of branch. |

**Example Request**

```
curl -X DELETE \
     http://localhost:9867/table/inventory/branch/staging
```

On success, HTTP 204 is returned with no response payload.

---

#### Delete Data

Create a commit that deletes data from a branch.  The data
to delete is given as either a list of data object IDs or
a filter expression (see [limitations](../command/db.md#super-db-delete)).

This removes the data from the branch without removing the
underlying data objects, so that [time travel](intro.md#time-travel)
still works after a delete.  Data objects are permanently removed by
a separate [vacuum](#vacuum-table) operation.

```
POST /table/{table}/branch/{branch}/delete
```

**Params**

| Name | Type | In | Description |
| ---- | ---- | -- | ----------- |
| table | string | path | **Required.** ID or name of the table. |
| branch | string | path | **Required.** Name of branch. |
| object_ids | [string] | body | IDs of the data objects to delete. |
| where | string | body | Filter expression selecting the values to delete (see [limitations](../command/db.md#super-db-delete)). |
| SuperDB-Commit | string | header | A JSON object describing the commit as for [Load Data](#load-data). |
| Content-Type | string | header | [MIME type](#mime-types) of the request payload. |
| Accept | string | header | Preferred [MIME type](#mime-types) of the response. |

**Example Request**

```
curl -X POST \
     -H 'Accept: application/json' \
     -H 'Content-Type: application/json' \
     -d '{"where": "product == \"gadget\""}' \
     http://localhost:9867/table/inventory/branch/main/delete
```

**Example Response**

```
{"commit":"01a1fe81-f048-7a41-95e5-0809f98ef7bc","warnings":[]}
```

**Example Request**

```
curl -X POST \
     -H 'Accept: application/json' \
     -H 'Content-Type: application/json' \
     -d '{"object_ids": ["01a1fe81-f033-7127-b956-9fff957f4bbe"]}' \
     http://localhost:9867/table/inventory/branch/main/delete
```

**Example Response**

```
{"commit":"01a1fe81-f08a-7e86-99a1-8428cd2d4d78","warnings":[]}
```

---

#### Compact Data

Create a commit that replaces data objects in a branch with new objects
holding the same data, merged in sort-key order.  See
[`super db compact`](../command/db.md#super-db-compact).

```
POST /table/{table}/branch/{branch}/compact
```

**Params**

| Name | Type | In | Description |
| ---- | ---- | -- | ----------- |
| table | string | path | **Required.** ID or name of the table. |
| branch | string | path | **Required.** Name of branch. |
| object_ids | [uuid] | body | **Required.** IDs of the data objects to compact. |
| SuperDB-Commit | string | header | A JSON object describing the commit as for [Load Data](#load-data). |
| Content-Type | string | header | [MIME type](#mime-types) of the request payload. |
| Accept | string | header | Preferred [MIME type](#mime-types) of the response. |

Since `object_ids` are UUID values, this request is most easily sent
in [SUP](../formats/sup.md) format, where UUIDs are written unquoted.

**Example Request**

```
curl -X POST \
     -H 'Accept: application/json' \
     -H 'Content-Type: application/x-sup' \
     -d '{object_ids:[01a1fe81-f033-7127-b956-9fff957f4bbe,01a1fe81-f048-7624-8355-7347349b095e]}' \
     http://localhost:9867/table/inventory/branch/main/compact
```

**Example Response**

```
{"commit":"01a1fe82-1260-71b6-bcfa-93d596a6cff9","warnings":[]}
```

---

#### Merge Branches

Create a commit that adds the changes in a child branch to another branch.

```
POST /table/{table}/branch/{branch}/merge/{child}
```

**Params**

| Name | Type | In | Description |
| ---- | ---- | -- | ----------- |
| table | string | path | **Required.** ID or name of the table. |
| branch | string | path | **Required.** Name of the branch to merge into. |
| child | string | path | **Required.** Name of the branch to merge from. |
| SuperDB-Commit | string | header | A JSON object describing the commit as for [Load Data](#load-data). |
| Accept | string | header | Preferred [MIME type](#mime-types) of the response. |

**Example Request**

```
curl -X POST \
     -H 'Accept: application/json' \
     http://localhost:9867/table/inventory/branch/main/merge/staging
```

**Example Response**

```
{"commit":"01a1fe81-f03d-7143-9881-c32cb60a0865","warnings":[]}
```

---

#### Revert

Create a commit that reverses the changes made by an earlier commit.

```
POST /table/{table}/branch/{branch}/revert/{commit}
```

**Params**

| Name | Type | In | Description |
| ---- | ---- | -- | ----------- |
| table | string | path | **Required.** ID or name of the table. |
| branch | string | path | **Required.** Name of the branch on which to revert the commit. |
| commit | string | path | **Required.** ID of the commit to revert. |
| SuperDB-Commit | string | header | A JSON object describing the commit as for [Load Data](#load-data). |
| Accept | string | header | Preferred [MIME type](#mime-types) of the response. |

**Example Request**

```
curl -X POST \
     -H 'Accept: application/json' \
     http://localhost:9867/table/inventory/branch/main/revert/01a1fe81-f08a-7e86-99a1-8428cd2d4d78
```

**Example Response**

```
{"commit":"01a1fe81-f0a5-7da1-8616-e5057713b5e9","warnings":[]}
```

---

### Queries

#### Query

Run a query or a [script](../super-sql/statements/intro.md) of statements
on the database.

```
POST /query
```

**Params**

| Name | Type | In | Description |
| ---- | ---- | -- | ----------- |
| query | string | body | **Required.** The query or script to run. |
| args | [string] | body | Strings bound to the `args` [constant](../super-sql/declarations/constants.md) in the query's main scope. |
| ctrl | string | query | Set to "T" to include control messages in BSUP responses. Defaults to "F". |
| Content-Type | string | header | [MIME type](#mime-types) of the request payload. |
| Accept | string | header | Preferred [MIME type](#mime-types) of the response. |

**Example Request**

```
curl -X POST \
     -H 'Accept: application/x-sup' \
     -H 'Content-Type: application/json' \
     -d '{"query": "from inventory | count() by warehouse | sort warehouse"}' \
     http://localhost:9867/query
```

**Example Response**

```
{warehouse:"chicago",count:2}
{warehouse:"miami",count:1}
```

**Example Request**

```
curl -X POST \
     -H 'Accept: application/json' \
     -H 'Content-Type: application/json' \
     -d '{"query": "from inventory | where warehouse in args | count()", "args": ["miami"]}' \
     http://localhost:9867/query
```

**Example Response**

```
[1]
```

**Example Request**

```
curl -X POST \
     -H 'Accept: application/x-sup' \
     -H 'Content-Type: application/json' \
     -d '{"query": "create table t (x int64); insert into t values (1); select * from t"}' \
     http://localhost:9867/query
```

**Example Response**

```
{kind:"create_table",stmt:"create table t (x int64)",start:2026-11-21T14:28:19.745488455Z,end:2026-11-21T14:28:19.746648429Z,info:{table:"t",id:01a1fe82-12a1-7ef0-8fc7-f1d4652c7ae2,created:true},error:null}
{kind:"insert",stmt:"insert into t values (1)",start:2026-11-21T14:28:19.746684832Z,end:2026-11-21T14:28:19.747823178Z,info:{table:"t",values:1::uint64,commit:01a1fe82-12a3-76da-8a3a-fe5c09377b49},error:null}
{x:1}
{kind:"query",stmt:"select * from t",start:2026-11-21T14:28:19.747850829Z,end:2026-11-21T14:28:19.748818657Z,info:{values:1::uint64},error:null}
```

With `ctrl=T` and a BSUP response, the results are accompanied by control
messages marking the start and end of each output channel and reporting
query statistics, e.g., in SUP:
```
{channel:"main"}::QueryChannelSet
{warehouse:"chicago",count:2}
{warehouse:"miami",count:1}
{channel:"main"}::QueryChannelEnd
{start_time:2026-11-21T14:15:22.978387144Z,update_time:2026-11-21T14:15:22.978817913Z,Progress:{bytes_scanned:209,values_scanned:3,...}}::QueryStats
```

An error found before a query starts to run, e.g., a syntax error, is
returned as a non-2xx response.  Since the response status is sent when
results begin to flow, an error that occurs while a query is running is
instead available from the [Query Status](#query-status) endpoint.

#### Query Status

Retrieve the error, if any, from a query that has finished running.
This endpoint responds only after the query has exited and is only available
for a limited time afterwards.

```
GET /query/status/{request_id}
```

**Params**

| Name | Type | In | Description |
| ---- | ---- | -- | ----------- |
| request_id | string | path | **Required.** The value of the response header `X-Request-Id` of the query. |
| Accept | string | header | Preferred [MIME type](#mime-types) of the response. |

**Example Request**

```
curl -X GET \
     -H 'Accept: application/json' \
     http://localhost:9867/query/status/01a1fe82-29a7-7d3b-a1a2-0f5e8a1c6d20
```

**Example Response**

```
{"error":"INSERT column \"x\": error({message:\"cannot cast to int64\",on:\"x\"})"}
```

#### Describe Query

Describe a query without running it: the tables it reads and, for each
output channel, its aggregation keys and sort order.

```
POST /query/describe
```

**Params**

| Name | Type | In | Description |
| ---- | ---- | -- | ----------- |
| query | string | body | **Required.** The query to describe. |
| Content-Type | string | header | [MIME type](#mime-types) of the request payload. |
| Accept | string | header | Preferred [MIME type](#mime-types) of the response. |

**Example Request**

```
curl -X POST \
     -H 'Accept: application/json' \
     -H 'Content-Type: application/json' \
     -d '{"query": "from inventory | count() by warehouse"}' \
     http://localhost:9867/query/describe
```

**Example Response**

```
{"sources":[{"kind":"Table","name":"inventory","id":"01a1fe81-c8b2-793a-aab3-383076bc5d23"}],"channels":[{"name":"main","aggregation_keys":[["warehouse"]],"sort":[]}]}
```

#### Compile Query

Parse a query without running it and return its abstract syntax tree (AST),
as displayed by [`super compile`](../command/compile.md).  The query is
only parsed, so a reference to a table that doesn't exist is not an error.

```
POST /compile
```

**Params**

| Name | Type | In | Description |
| ---- | ---- | -- | ----------- |
| query | string | body | **Required.** The query to parse. |
| Content-Type | string | header | [MIME type](#mime-types) of the request payload. |
| Accept | string | header | Preferred [MIME type](#mime-types) of the response. |

**Example Request**

```
curl -X POST \
     -H 'Accept: application/json' \
     -H 'Content-Type: application/json' \
     -d '{"query": "from inventory | head 1"}' \
     http://localhost:9867/compile
```

**Example Response**

```
[{"kind":"FromOp","item":{"source":{"kind":"Text","value":"inventory","loc":{"first":5,"last":13}},"args":[],"loc":{"first":5,"last":13}},"loc":{"first":0,"last":13}},{"kind":"HeadOp","count":{"kind":"Primitive","type":"int64","text":"1","loc":{"first":22,"last":22}},"loc":{"first":17,"last":22}}]
```

A query with a syntax error returns HTTP 400 with the error and its
position in `compilation_errors`, e.g.,
```
{"type":"Error","kind":"invalid operation","error":"parse error at line 1, column 23:\nfrom inventory | head(\n                  === ^ ===","compilation_errors":[{"Msg":"parse error","Pos":22,"End":-1}]}
```

---

### Events

Subscribe to a feed of database events in the format of
[server-sent events](https://html.spec.whatwg.org/multipage/server-sent-events.html).
The [MIME type](#mime-types) in the request's Accept header determines the format
of the `data` field values in the event stream.

The events are `table-new`, `table-update`, `table-delete`,
`branch-commit`, `branch-update`, and `branch-delete`.

```
GET /events
```

**Params**

None

**Example Request**

```
curl -X GET \
     -H 'Accept: application/json' \
     http://localhost:9867/events
```

**Example Response**

```
event: table-new
data: {"table_id":"01a1fe82-45e7-70f5-9a02-f4f793350aee"}

event: branch-commit
data: {"commit_id":"01a1fe82-45f1-7ab7-87c7-0e980e3c1623","table_id":"01a1fe82-45e7-70f5-9a02-f4f793350aee","branch":"main","parent":""}

event: table-update
data: {"table_id":"01a1fe82-45e7-70f5-9a02-f4f793350aee"}

event: table-delete
data: {"table_id":"01a1fe82-45e7-70f5-9a02-f4f793350aee"}
```

---

### Service

| Endpoint | Description |
| -------- | ----------- |
| `GET /version` | The version of the service, e.g., `{"version":"v0.5.0"}` |
| `GET /status` | The text `ok` if the service is running |
| `GET /auth/method` | The authentication method the service requires, if any |
| `GET /auth/identity` | The tenant and user IDs of the authenticated caller |
| `POST /mcp` | The [Model Context Protocol](../command/db.md#super-db-mcp) endpoint, when the service is started with `-mcp` |

---

## Media Types

For both request and response payloads, the service supports a variety of
formats.

### Request Payloads

When sending request payloads, include the MIME type of the format in the
request's Content-Type header. If the Content-Type header is not specified, the
service will expect SUP as the payload format.  Since JSON is a subset of
SUP, request bodies may be written in JSON.

An exception to this is when [loading data](#load-data) and Content-Type is not
specified. In this case the service will attempt to introspect the data and may
determine the type automatically. The
[input formats](../command/formats.md) table describes which
formats may be successfully auto-detected.

### Response Payloads

To receive successful (2xx) responses in a preferred format, include the MIME
type of the format in the request's Accept HTTP header. If the Accept header is
not specified, the service will return SUP as the default response format. A
different default response format can be specified by invoking the
`-defaultfmt` option when running [`super db serve`](../command/db.md#super-db-serve).

For non-2xx responses, the content type of the response will be
`application/json` or `text/plain`.

### MIME Types

The following table shows the supported MIME types and where they can be used.

| Format           | Request   | Response | MIME Type                             |
| ---------------- | --------- | -------- | ------------------------------------- |
| Arrow IPC Stream | yes       | yes      | `application/vnd.apache.arrow.stream` |
| BSUP             | yes       | yes      | `application/x-bsup`                  |
| CSV              | yes       | yes      | `text/csv`                            |
| JSON             | yes       | yes      | `application/json`                    |
| Line             | yes       | yes      | `application/x-line`                  |
| NDJSON           | no        | yes      | `application/x-ndjson`                |
| Parquet          | yes       | yes      | `application/x-parquet`               |
| SUP              | yes       | yes      | `application/x-sup`                   |
| TSV              | yes       | yes      | `text/tab-separated-values`           |
| Zeek             | yes       | yes      | `application/x-zeek`                  |
