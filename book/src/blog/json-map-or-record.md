# JSON Can't Tell a Map from a Record

_November 2026_

Here is a JSON object:
```
{"width":2,"height":1}
```
Is it a _record_, with a fixed set of named fields, like a struct in Go or
Rust or a row in a SQL table?  Or is it a _map_, with arbitrary string keys
that can be anything at all?

JSON can't say.  The syntax is the same either way.  That one missing bit
of information is why querying JSON is so error-prone, whether you do it
in SQL, Rust, or Go, and why SuperDB's data model treats records and maps
as different types.

## A messy data set

Here are some shapes:
```mdtest-input shapes.json
{"id":1,"type":"Rectangle","dimensions":{"width":2,"height":1}}
{"id":2,"type":"Circle","dimensions":{"radius":3}}
{"id":3,"type":"Square","dimensions":{"side":5}}
{"id":4,"type":"Circle","dimensions":{"radius":7}}
{"id":5,"type":"Vector","dimensions":[1,2,3,2,1]}
```
Most `dimensions` are objects with different fields, and one is an array.

## JSON as a type

When a database stores JSON in a column, the column's type is
effectively this recursive union, which we can write in SuperSQL as:
```mdtest-input json.spq
type JSON =
  float64 | int64 | bool | string | null
  | [JSON]
  | map{string:JSON}
```
Every JSON object becomes a map, because a map is the only thing an
arbitrary object is guaranteed to be.  Casting our shapes to this type
gives us exactly what a JSON column holds:
```mdtest-command
super -I json.spq -s -c 'from shapes.json | values this::JSON | head 1'
```
```mdtest-output
type JSON=int64|float64|bool|string|null|[JSON]|map{string:JSON}
map{"id":1,"type":"Rectangle","dimensions":map{"width":2,"height":1}::map{string:JSON}}::map{string:JSON}::JSON
```

Now try the obvious query:
```mdtest-command fails
super -I json.spq -s -c 'from shapes.json | values this::JSON | values dimensions'
```
```mdtest-output
no such field "dimensions": . applied to int64|float64|bool|string|[JSON] in JSON at line 1, column 47:
from shapes.json | values this::JSON | values dimensions
                                              ~~~~~~~~~~
```
The type checker is right.  A `JSON` value might be a number, a string, or
an array, and none of those has a field.  To get at `dimensions` safely,
we have to take the value apart, checking what it is at each step:
```mdtest-command
super -I json.spq -s -c 'from shapes.json | values this::JSON | values this.(map{string:JSON})?.dimensions.(map{string:JSON}) ?? 1'
```
```mdtest-output
type JSON=int64|float64|bool|string|null|[JSON]|map{string:JSON}
map{"width":2,"height":1}::map{string:JSON}
map{"radius":3}::map{string:JSON}
map{"side":5}::map{string:JSON}
map{"radius":7}::map{string:JSON}
1
```
That works, but look what happens with a typo, `dimension` for
`dimensions`:
```mdtest-command
super -I json.spq -s -c 'from shapes.json | values this::JSON | values this.(map{string:JSON})?.dimension.(map{string:JSON}) ?? 1'
```
```mdtest-output
1
1
1
1
1
```
No error, just the wrong answer.  A map's keys live in the data, not in
its type, so there is nothing for a compiler to check them against.

## SQL has the same problem

Suppose the shapes are stored in a DuckDB table with a JSON column `j`.
Here is the same logic in SQL:
```sql
CASE
  WHEN json_type(j) = 'OBJECT'
   AND json_type(j, '$.dimensions') = 'OBJECT'
  THEN j->'$.dimensions'
  ELSE '1'::JSON
END
```
Each `json_type` test does the job of one of the type assertions above.
Leave them out and write the natural `j->'$.dimensions'`, and DuckDB
hands back the vector's array as if it were an object, and `NULL` for a
value that isn't an object or a key that's misspelled.  Nothing fails.

SQL databases have added a lot of syntax to make JSON easier to work with:
`->` and `->>` operators, JSONPath expressions, `JSON_VALUE` and
`JSON_QUERY`, dot notation, and "strict" modes that raise errors instead of
returning `NULL`.  These make queries shorter, but they don't change the
underlying problem.  The column's type is `JSON`, and the planner can't see
past that.  The best a strict mode can do is fail at run time, on the rows
that happen to trip it.

The quiet `NULL` is the dangerous case.  A misspelled key in a join
condition doesn't break the query.  It just turns into `NULL`, the join
matches nothing (or everything, in an outer join), and the query returns
a plausible-looking wrong answer.

## So do Rust and Go

This isn't a SQL problem.  Statically typed languages hit the same wall
when JSON is decoded into a generic map.  In Go:
```go
func dimensions(v any) any {
	obj, ok := v.(map[string]any) // this.(map{string:JSON})
	if !ok {
		return 1
	}
	dims, ok := obj["dimensions"].(map[string]any) // .dimensions.(map{string:JSON})
	if !ok {
		return 1 // missing key, null, or wrong kind
	}
	return dims
}
```
And in Rust, using `serde_json`:
```rust
fn dimensions(v: &Value) -> Value {
    v.as_object()                              // this.(map{string:JSON})
        .and_then(|obj| obj.get("dimensions")) // ?.dimensions
        .and_then(Value::as_object)            // .(map{string:JSON})
        .map(|dims| Value::Object(dims.clone()))
        .unwrap_or(json!(1))                   // ?? 1
}
```
The code is the same shape every time: test the kind of the value, then
step into it, then test again.  And `obj["dimension"]` compiles just as
happily as `obj["dimensions"]`, for the same reason as before.

Both languages have a way out: decode into a struct, or a Rust enum, and
the compiler checks every field reference.  But that requires declaring a
schema in the program before you ever see the data.  That's exactly what
you can't do with data like this, which is why people reached for JSON in
the first place.

## The algebraic data model

SuperDB's [data model](../formats/model.md) keeps the information JSON
throws away.  Records and maps are different types.  Unions are
first-class types, so a value whose type varies from row to row has a
precise type rather than "JSON".  And the super-structured
[formats](../formats/intro.md) serialize values with their types intact,
so a type the data had when it was written is still there when it is read.

When SuperDB reads our shapes from JSON, it infers record types from the
data.  If we save them as [BSUP](../formats/bsup.md), those types travel
with the file:
```mdtest-command
super -o shapes.bsup shapes.json
super -s -c 'from shapes.bsup | values typeof(this)'
```
```mdtest-output
<{id:int64,type:string,dimensions:{width:int64,height:int64}}>
<{id:int64,type:string,dimensions:{radius:int64}}>
<{id:int64,type:string,dimensions:{side:int64}}>
<{id:int64,type:string,dimensions:{radius:int64}}>
<{id:int64,type:string,dimensions:[int64]}>
```
Nobody wrote a schema, yet the compiler knows the type of every value
before the query runs.  Now the typo is caught before any data is read:
```mdtest-command fails
super -s -c 'from shapes.bsup | values dimension'
```
```mdtest-output
no such field "dimension" at line 1, column 27:
from shapes.bsup | values dimension
                          ~~~~~~~~~
```
And a field that exists only sometimes gets a precise explanation of what
can go wrong:
```mdtest-command fails
super -s -c 'from shapes.bsup | values dimensions.radius'
```
```mdtest-output
no such field "radius": . applied to [int64] in {width:int64|missing,height:int64|missing,radius:int64|missing,side:int64|missing}|[int64] at line 1, column 38:
from shapes.bsup | values dimensions.radius
                                     ~~~~~~
field may be missing (consider ok()) at line 1, column 27:
from shapes.bsup | values dimensions.radius
                          ~~~~~~~~~~~~~~~~~
```
The compiler knows `dimensions` might be an array, and that `radius` might
be missing from the records.  Telling it we expect that gives the answer:
```mdtest-command
super -s -c 'from shapes.bsup | values dimensions.radius.ok()'
```
```mdtest-output
3
7
```
That's the static checking you'd get from a struct in Go or Rust, without
declaring a struct.  The type comes from the data, and the compiler
reads it.

JSON can't give you this, no matter how much syntax is layered on top,
because the information isn't there.  Once an object is stored as JSON,
the difference between a record and a map is gone.  Keeping data in a
format that preserves its types is what lets a compiler check the queries
written against it.
