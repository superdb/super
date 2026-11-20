# The Algebraic Data Model

XXX


This contrasts with the RM's approach to managing JSON where the JSON type system
is juxtaposed inside the relational type system.  Since JSON is a recursive sum type
and the relational model has no notion of algebraic types, an entire set of ad hoc
techniques has been integrated into SQL to deal with this, e.g., a
[JSON path language](https://www.postgresql.org/docs/current/datatype-json.html#DATATYPE-JSONPATH),
specialized constructors and extraction functions like
[`JSON_OBJECT` or `JSON_VALUE`](https://www.postgresql.org/docs/current/datatype-json.html)
and the domain-specific sub-language embedded as relational table function
[JSON_TABLE](https://www.postgresql.org/docs/current/functions-json.html#FUNCTIONS-SQLJSON-TABLE).

Variant no better.

type expressions... 
SuperSQL
franken type...

type fusion -> schema inference
fusion values -> fused but invertible data (efficient columnar)



## Why Not Relational?

The fashionable argument against a new system like SuperDB is that SQL and the relational
model (RM) are perfectly good solutions that have stood the test of time
so there's no need to replace them.
In fact,
[a recent paper](https://db.cs.cmu.edu/papers/2024/whatgoesaround-sigmodrec2024.pdf)
from legendary database experts
argues that any attempt to supplant SQL or the RM is doomed to fail
because any good ideas that arise from such efforts will simply be incorporated
into SQL and the RM.

Yet, the incorporation of the JSON data model into the
relational model never fails to disappoint.  One must typically choose
between creating columns of a JSON or [variant type](https://github.com/apache/spark/blob/master/common/variant/README.md) that layers in a parallel set of
operators and behaviors that diverge from core SQL semantics, or
rely upon schema inference to convert variant data into relational tables,
which unfortunately does not always work.

To understand the difficulty of schema inference,
consider this simple line of JSON data is in a file called `example.json`:
```json
{"a":[1,"foo"]}
```

>[!NOTE]
> The literal `[1,"foo"]` is a contrived example but it adequately
> represents the challenge of mixed-type JSON values, e.g.,
> an API returning an array of JSON objects with varying shape.

Surprisingly, this simple JSON input causes unpredictable schema inference
with some popular SQL systems.

For example, DuckDB performs only a partial schema inference &mdash; determining a
relational column called `a` is an array &mdash; but leaves the contents
of the array as the non-inferred type JSON:
```sh
$ duckdb -c "SELECT * FROM 'example.json'"
┌────────────┐
│     a      │
│   json[]   │
├────────────┤
│ [1, "foo"] │
└────────────┘
```
DataFusion simply fails with an error:
```sh
$ datafusion-cli -c "SELECT * FROM 'example.json'"
DataFusion CLI v52.1.0
Error: Arrow error: Json error: whilst decoding field 'a': expected string got 1
```
It turns out there's no easy way to represent this straightforward
literal array value `[1,'foo']` in these SQLs, e.g., simply including this
value in a SQL expression results in errors:
```sh
$ duckdb -c "SELECT [1,'foo']"
Conversion Error:
Could not convert string 'foo' to INT32

LINE 1: SELECT [1,'foo']
                  ^^^^^
$ datafusion-cli -c "SELECT [1,'foo']"
DataFusion CLI v52.1.0
Error: Optimizer rule 'simplify_expressions' failed
caused by
Arrow error: Cast error: Cannot cast string 'foo' to value of Int64 type
```
The more recent innovation of an open variant type
is more general than JSON but suffers from similar problems.
In both these cases, the JSON type and the variant
type are not individual types but rather entire type systems that differ
from the base relational type system and so are shoehorned into the relational model
as a parallel type system masquerading as a specialized type to make it all work.

## Enter Algebraic Types

What's missing here is an easy and native way to represent mixed-type entities.
In modern programming languages, such entities are enabled with a
[sum type or tagged union](https://en.wikipedia.org/wiki/Tagged_union).

While the original conception of the relational data model anticipated
"product types" &mdash; in fact, describing a relation's schema in terms of
a product type &mdash; it unfortunately did not anticipate sum types.

>[!NOTE]
> Codd's original paper on the relational model has a footnote that
> essentially describes as a product type:
>
> <!-- markdownlint-disable-next-line MD033 -->
> <center><img src="codd-footnote.png" alt="Codd's Footnote" title="Codd's Footnote" width="500"></center>
>
> But sum types were notably absent.

Armed with both sum and product types, a
comprehensive algebraic type system can represent any
[JSON value as a concrete type](https://www.researchgate.net/publication/221325979_Union_Types_for_Semistructured_Data).
And since relations are simply product types
as originally envisioned by Codd, any relational table can be represented
also as a super-structured product type.  Thus, JSON and relational tables
are cleanly unified with an algebraic type system.

The relational database industry is just beginning to recognize this
and ClickHouse, in particular, recently introduced sum types into their
type system so that mixed-type arrays now work, e.g., the query
from above seems to work
```sh
$ clickhouse -q "SELECT a FROM 'example.json'"
[1,'foo']
```
Interestingly, if we use ClickHouse's type-of function `toTypeName()`
we can see the resulting type relies upon another new type
called `Dynamic`:
```sh
$ clickhouse -q "SELECT toTypeName(a) FROM 'example.json'"
Array(Dynamic)
```
This result rhymes with the DuckDB type `[]JSON` but
ClickHouse provides a secondary type function `dynamicType()`
to get at the underlying type,
but we can't use that function on the top-level value
```sh
$ clickhouse -q "SELECT dynamicType(a) FROM 'example.json'"
Code: 43. DB::Exception: A value of illegal type was provided as 1st argument 'dynamic' to function 'dynamicType'. Expected: Dynamic, got: Array(Dynamic): In scope SELECT dynamicType(a) FROM `example.json`. (ILLEGAL_TYPE_OF_ARGUMENT)
```
Instead we must ask for the type of the array element
```sh
$ clickhouse -q "SELECT dynamicType(a[1]), dynamicType(a[2]) FROM 'example.json'"
Int64   String
```
While ClickHouse successfully inferred the types of the mixed-type array,
this feels clunky.  Moreover, with a slightly more complicated example,
ClickHouse fails to convert the input to its `Dynamic` representation:
```sh
$ cat example2.json
{"a":[1,"foo"]}
{"a":1}
$ clickhouse -q "SELECT a FROM 'example2.json'"
Code: 636. DB::Exception: The table structure cannot be extracted from a JSON format file. Error:
Code: 53. DB::Exception: Automatically defined type Int64 for column 'a' in row 1 differs from type defined by previous rows: Array(String). You can specify the type for this column using setting schema_inference_hints. (TYPE_MISMATCH) (version 26.1.1.912 (official build)).
...
```
No doubt, this will be fixed in the future.  But there's a
fundamental dissonance here in an
approach that adapts the statically-typed relational model to a new
model based on dynamic data without clearly defining what the new
foundation might be.

The result is user-facing ergonomics that are unappealing.

While this is truly impressive engineering, maybe there's a better way?

### Types Not Tables

Instead of adapting dynamic data to the relational model, SuperDB's approach is to
redefine the foundation around algebraic _types_ instead of _tables_, where a relational
table is simply a special case of an algebraic type.

This is the essence of super-structured data.

With this foundational shift, SuperDB "just works" when it comes to processing the
JSON example from above:
```sh
$ super -c "SELECT * FROM 'example.json'"
{a:[1,"foo"]}
$ super -c "SELECT [1,'foo'] AS a"
{a:[1,"foo"]}
```
In fact, we can see algebraic types at work here if we interrogate the type of
such an expression:
```sh
$ super -c "SELECT typeof(a) as type FROM (SELECT [1,'foo'] AS a)"
{type:<[int64|string]>}
```
In this super-structured representation, the `type` field is a first-class
[type value](super-sql/types/type.md)
representing an array type of elements having a sum type of `int64` and `string`.

And the input that foiled ClickHouse is naturally and easily
handled by super-structured data:
```sh
$ super -c "SELECT a, typeof(a) as type FROM 'example2.json'"
{a:[1,"foo"],type:<[int64|string]>}
{a:1,type:<int64>}
```