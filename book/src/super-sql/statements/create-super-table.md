# CREATE SUPER TABLE

```
CREATE SUPER TABLE [ IF NOT EXISTS ] <name> <type>
  [ ORDER BY <key> [ ASC | DESC ] [ , ... ] ]
```

## Description

`CREATE SUPER TABLE` creates a table called `<name>` whose values all have
the super-structured type `<type>`, which may be any
[SuperSQL type](../types/intro.md), not just a relational schema (i.e.,
a record of comprising columns).
A union type, for example, lets a single table hold values of different
shapes while still giving every value a precise type.
`<type>` may refer to [named types](../types/named.md) declared at the
top of the script, including types computed from data.

As with [CREATE TABLE](create-table.md), `<name>` is an identifier or a
quoted string, `IF NOT EXISTS` makes the statement do nothing if the
table already exists, and `ORDER BY` sets the table's
[sort key](../../database/intro.md#sort-key).

Every value added to the table, whether by [INSERT](insert.md),
[`super db load`](../../command/db.md#super-db-load),
or the [load](../operators/load.md) operator, is
[cast](../expressions/cast.md) to `<type>` with SuperSQL semantics.
Unlike a table created with `CREATE TABLE`, a missing field is not
filled in with null: the value must cast to `<type>`.  A field not
in `<type>` is an error.

A value that cannot be cast to the table's type fails the whole `INSERT`
or load and nothing is committed.

`CREATE SUPER TABLE` is equivalent to
[`super db create -type`](../../command/db.md#super-db-create).

## Examples

The first example creates a database in the current directory with
[`super db init`](../../command/db.md#super-db-init) and
[attaches](../../command/db.md#super-db-attach) to it, and the
examples that follow use it.

---

_A table whose values are either a record or a string_

```mdtest-command
super db init -q db
super db attach -q db
super db -c "
  create super table mixed {a:int64}|string;
  insert into mixed values {a:'1'}, 'hi'
" > /dev/null
super db -s -c 'from mixed | sort this'
```
```mdtest-output
"hi"::(string|{a:int64})
{a:1}::(string|{a:int64})
```

---

_A value that doesn't fit the type is rejected_

```mdtest-command fails
super db -c "insert into mixed values {a:1,b:2}" > /dev/null
```
```mdtest-output
value {a:1,b:2} cannot be cast to table type string|{a:int64}
```

---

_A table whose type is a sum type computed from the data in another table_

Given some messy event data:
```mdtest-input events.json
{"type":"PushEvent","ref":"main","commits":[{"sha":"a1"}]}
{"type":"WatchEvent","action":"started"}
{"type":"PushEvent","ref":"dev"}
{"type":"IssuesEvent","action":"opened","issue":{"number":1}}
```
we can load it into a table without a type, then compute a
[sum type](../functions/types/sum_type.md) with one named type for each
kind of event by [fusing](../type-fusion.md) the types of each kind,
and create a table of that type:
```mdtest-command
super db -c "create table raw" > /dev/null
super db load -q -use raw events.json
super db -c "
  type Event = (
    from raw
    | fuse(this) by type
    | collect_map(map{type:optionize(fuse)})
    | sum_type(this)
  );
  create super table events Event;
  insert into events from raw
" > /dev/null
super db -s -c 'from events | sort type, this'
```
```mdtest-output
type IssuesEvent={type:string,action:string,issue:{number:int64}}
type PushEvent={type:string,ref:string,commits?:[{sha:string}]}
type WatchEvent={type:string,action:string}
type Event=IssuesEvent|PushEvent|WatchEvent
{type:"IssuesEvent",action:"opened",issue:{number:1}}::IssuesEvent::Event
{type:"PushEvent",ref:"dev"}::PushEvent::Event
{type:"PushEvent",ref:"main",commits?:[{sha:"a1"}]}::PushEvent::Event
{type:"WatchEvent",action:"started"}::WatchEvent::Event
```
Each value is now tagged with the kind of event it is, and a value that
fits none of them can't be added:
```mdtest-command fails
super db -c "insert into events values {type:'ForkEvent'}" > /dev/null
```
```mdtest-output
value {type:"ForkEvent"} cannot be cast to table type Event
```
