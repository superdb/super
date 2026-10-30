# Python

The [`superdb` Python package](https://github.com/superdb/superdb-python)
provides support for Python-based interaction with a
persistent [database](../../command/db.md).  The Python package supports loading data into a
database as well as querying and retrieving results.  The Python client
interacts with the database via the REST API provided by
[super db serve](../../command/db.md#super-db-serve).

>[!NOTE]
> This package is useful for experimentation with SuperDB from Python but is
> currently limited in its functionality and performance.
>
> The use of [Apache Arrow IPC](https://arrow.apache.org/docs/format/Columnar.html#ipc-streaming-format) for
> query response transport covers common data types cleanly —
> integers, floats, timestamps, strings, booleans, nested records, arrays, and
> maps all arrive as natural Python types — but Arrow is *schema-rigid*: every
> record in a result must share a single type, and a handful of rich types in the
> [super data model](../../formats/model.md) arrive as plain Python strings
> rather than richer objects. The full picture is in
> [type mapping and limitations](#type-mapping-and-limitations)
> below.
>
> The full benefits of super-structured data (including improved performance)
> are expected when there is a native Python library implementation of
> [CSUP](../../formats/csup.md).  Stay tuned.

## Installation

```
pip3 install "git+https://github.com/superdb/superdb-python"
```

## Example

To run this example, first start a SuperDB service from your shell:
```sh
super db init scratch
super db serve scratch
```

Then, in another shell, use Python to create a table, load some data,
and run a query:
```sh
python3 <<EOF
import superdb

# Connect to the default local service endpoint at http://localhost:9867.
# To use a different endpoint, supply its URL via the SUPER_DB environment
# variable or as an argument here.
client = superdb.Client()

client.create_table('TestTable')

# Load some SUP records from a string.  A file-like object also works.
# Most data formats are detected automatically.
client.load('TestTable', '{s:"hello"} {s:"world"}')

# Begin executing a SuperDB query for all values in TestTable.
# This returns an iterator, not a container.
values = client.query('from TestTable')

# Stream values from the server.
for val in values:
    print(val)

# Clean up after ourselves.
client.delete_table('TestTable')
EOF
```

You should see this output:
```
{'s': 'world'}
{'s': 'hello'}
```

## Overview

The `Client` class connects to a running SuperDB service and exposes these
operations:

- **`query(query, safe=True)`** — run a SuperSQL query and get results back as
  an iterator of Python dicts, one per record. With `safe=True` (the default),
  a pre-flight query runs first to check for mixed result types and, if detected,
  raises `MixedTypesError` rather than silently returning incomplete data. Pass
  `safe=False` to skip the pre-flight check and avoid the extra round trip, but
  only if you know your query response will have a single type.
- **`query_raw(query)`** — like `query()`, but returns the raw HTTP response
  (Apache Arrow IPC stream format) for callers that want to handle decoding
  themselves.
- **`create_table(name, ...)`** — create a new table.
- **`load(table_name_or_id, data, ...)`** — load data into a table branch.
- **`delete_table(table_name_or_id)`** — delete a table by name or ID.

## Type mapping and limitations

`query()` uses the Apache Arrow IPC streaming format to transport query
responses from the SuperDB services back to the client, then converts each
Arrow record batch to Python objects via PyArrow's `to_pylist()`. Most
types in the [super data model](../../formats/model.md) survive the round
trip cleanly, but several do not.

### Types that convert cleanly

| SuperDB type | Python type |
|---|---|
| `int8` … `int64`, `uint8` … `uint64` | `int` |
| `float16`, `float32`, `float64` | `float` |
| `bool` | `bool` |
| `string` | `str` |
| `bytes` | `bytes` |
| `time` | `datetime.datetime` |
| `duration` | `datetime.timedelta` |
| `null` | `None` |
| record | `dict` |
| array | `list` |
| map | `dict` |
| union | the Python type of whichever union branch holds the value |

### Types that lose fidelity

**`ip` and `net` → `str`**
IP addresses (e.g. `192.168.1.1`) and network prefixes (e.g. `10.0.0.0/8`)
are converted to plain strings by the Arrow encoder. They will not be
`ipaddress.IPv4Address` / `ipaddress.IPv6Address` / `ipaddress.IPv4Network` /
`ipaddress.IPv6Network`.

**`enum` → `str`**
Enum values are converted to their symbol name. The enum type itself is not
preserved.

**`error` → `str`**
SuperDB error values are formatted as strings.

**`type` → `str`**
SuperDB first-class type values are formatted as their string representation.

**`set` → `list`**
Arrow has no set type, so sets are encoded as lists. SuperDB enforces set
invariants (no duplicate elements) before encoding, so the returned list will
not contain duplicates. However, Python will not enforce this once the result
is stored in a list, and the list has an arbitrary order.

### Structural limitations

- **Multiple top-level record types in a single result.** When a query
  result contains records of more than one type, the Arrow encoder streams
  records matching the first type it encounters and then stops when it hits
  an incompatible record. `query()` guards against this by default: it runs a
  pre-flight check and raises `MixedTypesError` (with a `type_count` attribute)
  if more than one distinct type is detected. If encountered, one possible
  way to work around this is append the [fuse](../../super-sql/operators/fuse.md)
  operator to your query to merge all types into a single type:
  ```
  from mytable | ... | fuse
  ```
  If you know your data is homogeneous and want to avoid the extra round trip,
  pass `safe=False` — but results will be silently truncated if mixed types are
  encountered.
- **Non-record top-level values.** The client's use of Arrow requires every
  top-level value in a result to be a record. If a table contains non-record
  values (e.g., strings loaded with line format), `query()` raises
  `NonRecordError` (with a `kinds` attribute listing the non-record
  [kinds](../../super-sql/functions/types/kind.md) found). Pass `safe=False`
  to skip the check — the result will be silently empty.
- **Empty records** (records with no fields) are not supported by Arrow. A
  table containing only empty records will return no results rather than an
  error.
- **Unions with more than 255 fields** are not supported.

## MIME types

When `load()` is called without a `mime_type`, the server auto-detects the format
by inspecting the beginning of the uploaded stream. This works for most formats:

| Format | MIME type |
|---|---|
| SUP | `application/x-sup` |
| BSUP | `application/x-bsup` |
| NDJSON | `application/x-ndjson` |
| JSON | `application/json` |
| CSV | `text/csv` |
| TSV | `text/tab-separated-values` |
| Zeek | `application/x-zeek` |
| Arrow IPC stream | `application/vnd.apache.arrow.stream` |

Three formats cannot be auto-detected over the API and **require an explicit
`mime_type`**:

| Format | MIME type | Reason |
|---|---|---|
| CSUP | `application/x-csup` | Detection requires seekable input |
| Parquet | `application/x-parquet` | Detection requires seekable input |
| Line | `application/x-line` | Auto-detection not supported |

CSUP and Parquet store identifying metadata outside the stream header (Parquet's
footer is at the end of the file), so the server cannot detect them from an HTTP
request body. Line format is plain text with no identifying structure and is
intentionally excluded from auto-detection.

```python
with open('data.parquet', 'rb') as f:
    client.load('MyTable', f, mime_type='application/x-parquet')
```
