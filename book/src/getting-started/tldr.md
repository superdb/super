# TL;DR

Skim these one liners to get the gist of what SuperDB can do...

>[!NOTE]
> JSON inputs can include any concatenation of valid JSON values, e.g.,
> [newline-delimited JSON](https://github.com/ndjson/ndjson-spec),
> though newline delimiters are not required.

## Query a CSV, JSON, or Parquet file using SuperSQL
```
super -c "SELECT * FROM file.[csv|csv.gz|json|json.gz|parquet]"
```
## Run a SuperSQL query sourced from an input file
```
super -I path/to/query.sql
```
## Pretty-print a sample value as super-structured data
```
super -S -c "limit 1" file.[csv|csv.gz|json|json.gz|parquet]
```
## Compute a histogram of the "data shapes" in a JSON file
```
super -c "count() by typeof(this)" file.json
```
## Display a sample value of each "shape" of JSON data
```
super -c "any(this) by typeof(this) | values any" file.json
```
## Search Parquet files easily and efficiently without schema handcuffs
```
super *.parquet > all.bsup
super -c "? search keywords | other pipe processing" all.bsup
```
## Read a CSV from stdin, process with a query, and write to stdout
```
cat input.csv | super -f csv -c <query> -
```
## Fuse JSON data into a unified schema and output as Parquet
```
super -o out.parquet -c fuse file.json
```
## Run as a calculator
```
super -c "1.+(1/2.)+(1/3.)+(1/4.)"
```
## Search all values in the logs table for keyword "alert" and level >= 2
```
super db -c "from logs | ? alert level >= 2"
```

## Traverse nested data with recursive functions and re-entrant subqueries

```
super -c '
fn walk(node, visit):
  case kind(node)
  when "array" then
    [unnest node | walk(this, visit)]
  when "record" then
    unflatten([unnest flatten(node) | {path,value:walk(value, visit)}])
  when "union" then
    walk(under(node), visit)
  else visit(node)
  end
fn addOne(node): case typeof(node) when <int64> then node+1 else node end
values 1, [1,2,3], [{x:[1,"foo"]},{y:2}]
| values walk(this, &addOne)
'
```

## Handle and wrap errors in a SuperSQL pipeline
```
... | super -c "
switch is_error(this) (
    case true ( values error({message:"error into stage N", on:this}) )
    default (
        <non-error processing here>
        ...
    )
)
"
| ...
```

## Embed a pipe query search in SQL FROM clause

```
super -c "
SELECT union(type) as kinds, network_of(srcip) as net
FROM ( from logs.json | ? example.com AND urgent )
WHERE message_length > 100
GROUP BY net
"
```

Or write this as a pure pipe query using
SuperSQL [shortcuts](../super-sql/operators/intro.md#shortcuts)

```
super -c "
from logs.json
| ? example.com AND urgent
| message_length > 100
| kinds:=union(type), net:=network_of(srcip) by net
"
```
