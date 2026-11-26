# The Algebraic Data Model

> "In the same way ... one plus one is two ..., the relational model is the
> true way, the correct way ... to represent data"
>
> &mdash; Andy Pavlo, [CMU database lecture](https://www.youtube.com/watch?t=4466&v=APqWIjtzNGE)

In [What Goes Around Comes Around... And Around...](https://db.cs.cmu.edu/papers/2024/whatgoesaround-sigmodrec2024.pdf),
Pavlo and Stonebraker argue that any attempt to replace the relational model
(RM) is doomed to fail, because whatever good ideas such efforts produce
will simply be absorbed into SQL and the RM.

We disagree.

They are right that the RM absorbs ideas.  The trouble is what the
absorption produces.

The RM was brilliant at the time of its invention, but everything in it is
built upon the mathematical concept of a _relation_, which models a _table_.
Tables are an easily understood and broadly applicable design pattern,
but real-world data just doesn't always fit in.

JSON is the notable case.  Modern SQL has nested column types like structs
and lists, but a relation is still a single product type that every row must
have, and JSON data in general doesn't have one.  So, just as Pavlo and Stonebraker
describe, the RM absorbed this challenge by embedding JSON as its own
standalone type inside the RM's type system, with a parallel set of semantics
and manipulation logic.

The result is two different design patterns bolted together and forced to
interoperate in unnatural ways.  Borrowing a
[term from design culture](https://architecturefringe.com/news/2026/frankentypes-from-the-archive),
we call the juxtaposition of these mismatched type systems a _frankentype_.

## The Frankentype

As opposed to a freakish building, here's an example of a frankentype _data type_.
Suppose we have some data describing shapes (`shapes.json`), which we will use throughout
this section:
```mdtest-input shapes.json
{"id":1,"type":"Rectangle","dimensions":{"width":2,"height":1}}
{"id":2,"type":"Circle","dimensions":{"radius":3}}
{"id":3,"type":"Square","dimensions":{"side":5}}
{"id":4,"type":"Circle","dimensions":{"radius":7}}
{"id":5,"type":"Vector","dimensions":[1,2,3,2,1]}
```
Each shape has an `id`, a `type`, and `dimensions` that depend on the type.
A relational schema has no way to say "depends on the type", so to get this
data into a table, it has to be made to fit in one of three ways:
* a _wide table_ with a nullable column for every field of every kind of shape,
  e.g., `width`, `height`, `radius`, `side`, and `vector`, where most values in
  each row are `NULL`,
* a _normalized_ table for each kind of shape, which must be
  recombined with `UNION ALL` or outer joins whenever the shapes are queried
  together, again padding the results with `NULL`, or
* a _JSON column_ for the dimensions.

The first two take a schema design and an ETL job for five lines of data.
The third is the path of least resistance, and it is where the frankentype
comes from:
```sql
CREATE TABLE shapes (id BIGINT NOT NULL, type VARCHAR NOT NULL, dimensions JSON NOT NULL)
```
Written as a type, a row of this table is
```
{id:int64,type:string,dimensions:JSON}
```
The outer part belongs to the SQL type system.  The `JSON` inside it is
a different type system altogether, with its own operators, its own path
language, and its own rules for what happens when things go wrong.

We can better understand the frankentype problem by looking at each of its
halves as a _type_.

### Relational Schema as a Type

In the RM, a relation is a set of tuples over a fixed list of columns.
Each tuple has a value for the first column _and_ a value for the second
_and_ so on, so a tuple is a value of a _product type_ and a table's schema
is exactly one such type.  For example,
```sql
CREATE TABLE paint (id BIGINT NOT NULL, color VARCHAR NOT NULL)
```
declares the product type often called a `struct` or `record` in
programming languages.
In SuperSQL, we can write this type as follows:
```
{id:int64,color:string}
```
In the RM, every row from the `paint` table must be a value of
this type and this conformance is fundamental to every design decision
that underlies any relational database.

While Codd originally conceived of these rows as tuples that
conformed to mathematical relations, his paper was knocking on
the door of a type-oriented interpretation, including a footnote that
essentially describes a relation as a product type:

---
<!-- markdownlint-disable-next-line MD033 -->
<center><img src="codd-footnote.png" alt="Codd's Footnote" title="Codd's Footnote" width="500"></center>

---

But sum types were notably absent.

### JSON as a Type

The document data model starts from the other end.  A JSON value is a number _or_
a string _or_ a boolean _or_ null _or_ an array of JSON values _or_ an object
whose keys map to JSON values.  This is a sum type and, since JSON values
nest, a recursive one.

Many languages support recursive sum types and we can write down a precise
type definition for JSON using the SuperSQL syntax as follows:
```
type JSON = float64 | bool | string | null | [JSON] | map{string:JSON}
```
This one very specific type is the type of every JSON value ever written.

Making matters worse, many applications serialize their algebraically
typed data into JSON, losing the original type detail of the host language's
representation.  Specifically, a `record` and a `map` type are both
serialized to the same JSON object, modeled as `map{string:JSON}`.
Once serialized, the original intent of the type detail
is lost and schema inference algorithms must guess whether the data structure
is best modeled as a `record` or a `map`.

### The Mismatch

Viewed as types, it is easy to see why the two halves of a frankentype
don't fit together:
* The type checker is blind inside the JSON.  The keys of a map are values
  rather than the field names of a product type, so there is nothing to check
  a reference like `dimensions.radius` against.
* SQL's expressions were not designed for sum types.  There is no native way
  to ask which kind of value a JSON value is and proceed accordingly, so
  specialized functions and casts like `TRY_CAST` are needed to bridge from
  one type system to the other.
* Going the other way and treating the documents as the tables doesn't work
  either.  With no types, SQL can't resolve an unqualified column name to a
  table alias when the query is compiled, because nothing says which table
  has that field.
* When things go wrong, the answer is `NULL`.  A key that is absent, a value
  of the wrong kind, a misspelled path, and a value that really is null all
  come back as `NULL`, leading to subtle and often hard-to-find bugs.

The relational design for JSON confines it to a column type and relies on
these bridges.  This is good engineering under a hard constraint, and
systems keep getting better at it.  But the constraint is the problem.
As long as there are two type systems, the type checker can't follow a query
from one into the other.

Could there be a better way?

## Enter Algebraic Data

The relational schema is a product type and JSON is a sum type, and both
are algebraic types.  So why not start from the premise that an algebraic
type system is the fundamental building block?

We have taken precisely this approach by devising a new data model
called the _algebraic data model_ (ADM).  Instead of jamming the JSON type
inside of a relational product type, the ADM has one type system in which both are
special cases.

[SQL++](https://par.nsf.gov/servlets/purl/10548703) made the same diagnosis:
JSON as a column type is a bolt-on.  But its cure was to relax SQL toward
untyped documents, and three things follow.  A query over data with no
declared schema can't be statically type checked.  Column names can't be
resolved to table aliases, as described [above](#the-mismatch).  And an
application's algebraic types are still lost when they are serialized to
JSON on the way in.  Relaxing the type system removes the mismatch by
removing the types.  The ADM removes it by making the type system big enough
to hold both.

While putting sum types in databases is not a new idea, doing so while maintaining
interoperability and warehouse-style performance required several things to
work together:
* nominal types with recursion to provide alignment with host programming languages,
* type-safe untagged unions, where a value is used directly with no tag to
  name in a query, so that JSON drops in seamlessly,
* fusion types to provide warehouse performance for JSON where algebraic types
  have been lost through serialization,
* first-class types to enable fusion types and flexible exploration and
  processing of heterogeneous data, and
* first-class errors to provide robust error handling, especially when
  encountering unanticipated types, e.g., due to upstream schema changes in APIs.

And unlike the RM, data sets in the ADM may be heterogeneously typed and need not
conform to a table.  At the same time, all data is explicitly typed
and queries are statically type-checked no matter how much type variation exists.

The argument here is that the two data models in wide use today
each took one half of this picture.  A relational schema is a product type,
with no sums.  JSON is a specific sum type, with no products.  A type system
that has both and composes them freely contains each as a special case.
And because it is the same type system that languages like Rust and TypeScript
already use, data can keep its types all the way from an application to
the database and back.

| | Type of a collection | What the type says |
|---|---|---|
| Relational table | one product type, e.g., `{id:int64,color:string}` | everything, provided every row is alike |
| JSON collection | one sum type, `JSON` | nothing, but anything fits |
| ADM | any algebraic type | as much as is true of the data |

In the ADM, products and sums belong to one type system and compose freely.
A record can have a field that is a union, a union can have members that are
records, an array can hold a union of records, and types can be named and
can refer to themselves.  So
* a relational table is a collection whose type is a record of columns,
* a JSON collection is a collection whose type is `JSON`, and
* everything in between has a type, too.

This is what it means for the ADM to subsume the two models.  Neither is
emulated.  A relational schema and the JSON type are both just types that
can be written down, and the relational operators work on all of them.

The ADM is a general idea, like the relational model or the document model.
SuperDB's implementation of it is the [super data model](formats/model.md),
and the examples in this section use its types and the syntax of SuperSQL.
The super data model rounds out records and unions with arrays, sets, and
maps (a map being an
[exponential type](https://en.wikipedia.org/wiki/Exponential_object) in the
algebraic nomenclature), along with first-class types and first-class errors.

## Shapes in SuperDB

With algebraic types, the diverse shapes from our example above
need no workaround, and no types need to be declared to get started.
When SuperDB reads the JSON, each
shape becomes a record with its own precise type, and saving the shapes as
[BSUP](formats/bsup.md) keeps these types with the data:
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
Under the covers, SuperDB [fuses](super-sql/type-fusion.md) these types into
one type that describes the whole collection, which the type checker uses to
check queries:
```mdtest-command
super -S -c 'from shapes.bsup | aggregate fuse(this)'
```
```mdtest-output
<{
  id: int64,
  type: string,
  dimensions: {
    width: int64|missing,
    height: int64|missing,
    radius: int64|missing,
    side: int64|missing
  }|[int64]
}>
```
Here `dimensions` is a sum type: either a record or an array of integers.
And a field that only some shapes have is marked `missing` rather than
filled with `NULL`, so "doesn't apply" and "unknown" are never confused.

## ADM Operators

Just as the relational model includes a set of operators for manipulating
relations, the ADM includes a set of operators for manipulating algebraic data.

The relational operators provide a great foundation for them:
selection, projection, joins, aggregation, normalized tables, and primary keys
have all stood the test of time.  The problem is that they are bound to tables,
which are rooted in mathematical relations, i.e., sets of tuples that all
have the same type.  Every operator takes relations in and puts a relation
out, so a join, for example, has to form its result by spreading the columns
of its inputs into one wide row.

The ADM operators are the same operators without that constraint.  They take
in and put out sequences of heterogeneously typed values, of which a table is
a special case.  When the relational operators are applied to algebraic types
in this way, they also become simpler and more natural to use.

### Join Is Record Composition

In SuperSQL, a [join](super-sql/operators/join.md) doesn't flatten its inputs
into one wide row.  It composes them: each result is a record with a `left`
field and a `right` field holding the two values that matched.
Given some paint colors for the shapes:
```mdtest-input paint.json
{"id":1,"color":"red"}
{"id":2,"color":"blue"}
{"id":4,"color":"green"}
```
the join is
```mdtest-command
super -s -c 'from shapes.bsup | join (from paint.json) on left.id=right.id'
```
```mdtest-output
{left:{id:1,type:"Rectangle",dimensions:{width:2,height:1}},right:{id:1,color:"red"}}
{left:{id:2,type:"Circle",dimensions:{radius:3}},right:{id:2,color:"blue"}}
{left:{id:4,type:"Circle",dimensions:{radius:7}},right:{id:4,color:"green"}}
```
and the matched values can be combined in any way you like, e.g.,
by spreading a shape into a new record along with its color:
```mdtest-command
super -s -c 'from shapes.bsup | join (from paint.json) on left.id=right.id | values {...left, color:right.color}'
```
```mdtest-output
{id:1,type:"Rectangle",dimensions:{width:2,height:1},color:"red"}
{id:2,type:"Circle",dimensions:{radius:3},color:"blue"}
{id:4,type:"Circle",dimensions:{radius:7},color:"green"}
```

An outer join shows the difference most clearly.  In SQL, a `LEFT JOIN`
pads an unmatched row with `NULL` columns, which look just like a color that
is actually `NULL`.  In SuperSQL, an unmatched shape simply has no `right`
field, and the query says what to do about it:
```mdtest-command
super -s -c "from shapes.bsup | left join (from paint.json) on left.id=right.id | values {...left, color:right.color.ok() ?? 'unpainted'} | sort id"
```
```mdtest-output
{id:1,type:"Rectangle",dimensions:{width:2,height:1},color:"red"}
{id:2,type:"Circle",dimensions:{radius:3},color:"blue"}
{id:3,type:"Square",dimensions:{side:5},color:"unpainted"}
{id:4,type:"Circle",dimensions:{radius:7},color:"green"}
{id:5,type:"Vector",dimensions:[1,2,3,2,1],color:"unpainted"}
```

Keys and normalization work just as they do in the RM.  A
[table](super-sql/statements/create-table.md) can have typed columns and
`NOT NULL` constraints, and tables are joined on their keys.  The ADM
simply doesn't require every value in a table to have the same type.

### Unnest Instead of Lateral Joins

To work with the elements of the vector in SQL, say with the wide-table
version of the shapes, you would unnest the array with a lateral join, which
joins each row with a table computed from that row:
```sql
SELECT s.id, v.d
FROM shapes s CROSS JOIN LATERAL unnest(s.vector) AS v(d)
```
In SuperSQL, [unnest](super-sql/operators/unnest.md) is an operator of its own.
A [type assertion](super-sql/expressions/type-assertions.md) picks out the
dimensions that are arrays, the other shapes produce nothing, and
`with` carries along the shape's `id`:
```mdtest-command
super -s -c '
  from shapes.bsup
  | unnest d:=dimensions.([int64]) with id
'
```
```mdtest-output
{d:1,id:5}
{d:2,id:5}
{d:3,id:5}
{d:2,id:5}
{d:1,id:5}
```
And with `into`, a query runs on the elements of each array as a unit,
just like a lateral subquery but without the join:
```mdtest-command
super -s -c 'from shapes.bsup | unnest d:=dimensions.([int64]) with id into (aggregate total:=sum(d), peak:=max(d) by id)'
```
```mdtest-output
{id:5,total:9,peak:3}
```

### No NULL Surprises

In SQL, comparing `NULL` to anything yields `NULL`, which `WHERE` treats as
false.  So with the wide table above, the rows returned by
`WHERE dimensions.radius > 5` and by `WHERE NOT (dimensions.radius > 5)` don't add up to the
whole table: every shape that isn't a circle silently disappears from both.
And a misspelled JSON path just returns `NULL`, so the query still runs
and gives the wrong answer.

In SuperSQL, the type checker makes you work out what to do about a field
that might be missing before the query runs.  Asking for the radius of every
shape is an error, because the checker knows which shapes don't have one:
```mdtest-command fails
super -s -c 'from shapes.bsup | where dimensions.radius > 5 | values id'
```
```mdtest-output
no such field "radius": . applied to [int64] in {width:int64|missing,height:int64|missing,radius:int64|missing,side:int64|missing}|[int64] at line 1, column 37:
from shapes.bsup | where dimensions.radius > 5 | values id
                                    ~~~~~~
field may be missing (consider ok() or ?.) at line 1, column 26:
from shapes.bsup | where dimensions.radius > 5 | values id
                         ~~~~~~~~~~~~~~~~~
```
Saying that the radius may be absent with [ok](super-sql/functions/generics/ok.md)
isn't enough either, as the checker then wants to know what an absent
radius should mean:
```mdtest-command fails
super -s -c 'from shapes.bsup | where dimensions.radius.ok() > 5 | values id'
```
```mdtest-output
int64|none: none not handled (consider ?? or ?.) at line 1, column 26:
from shapes.bsup | where dimensions.radius.ok() > 5 | values id
                         ~~~~~~~~~~~~~~~~~~~~~~
```
Once the query says it, e.g., that a shape without a radius counts as
radius 0, the filter and its negation together account for every shape:
```mdtest-command
super -s -c 'from shapes.bsup | where (dimensions.radius.ok() ?? 0) > 5 | values id'
super -s -c 'from shapes.bsup | where not ((dimensions.radius.ok() ?? 0) > 5) | values id'
```
```mdtest-output
4
1
2
3
5
```
A misspelled field is caught, too:
```mdtest-command fails
super -s -c 'from shapes.bsup | values dimensions.radious.ok()'
```
```mdtest-output
no such field "radious": . applied to {width:int64|missing,height:int64|missing,radius:int64|missing,side:int64|missing}|[int64] at line 1, column 38:
from shapes.bsup | values dimensions.radious.ok()
                                     ~~~~~~~
```
There are no `NULL` surprises because there is no `NULL` standing in for
missing data: a missing field is part of the type, and the query has to say
what to do about it.

## Types, Not Tables

The ADM redefines the foundation of data around algebraic _types_ instead of
_tables_.  A relational table is a special case, so SuperDB supports
[schema-constrained tables](super-sql/statements/create-table.md) and
SQL queries on them, but a table can also be a
[super table](super-sql/statements/create-super-table.md) of any type, e.g.,
a sum type of the kinds of shapes, or have no type at all.

And because the super formats serialize values with their types, a value's
type is the same whether it's in a query, in a file, or in a database table.
That holds for the types of a program's own data, too.

## The Algebraic Boomerang

The types of the ADM weren't invented for databases.  They are the types
application programmers already use.  Rust has structs and enums, TypeScript
has object types and discriminated unions, and Swift, Kotlin, OCaml, Haskell,
and recent versions of Java and Python have their own forms of the same thing.
The shapes, for example, would be modeled in Rust as
```rust
struct Shape {
    id: i64,
    dimensions: Dimensions,
}

enum Dimensions {
    Rectangle { width: i64, height: i64 },
    Circle { radius: i64 },
    Square { side: i64 },
    Vector(Vec<i64>),
}
```
`Shape` is a product type: a shape has an `id` _and_ `dimensions`.
`Dimensions` is a sum type: it is a rectangle _or_ a circle _or_ a square
_or_ a vector.

Today these types don't survive a _boomerang_ to a database and back:

1. The application serializes its values to JSON.  A struct's fields become
   the keys of a map.  A sum type's tag has nowhere to go, so it is written
   into the data as a string using one of
   [several conventions](https://serde.rs/enum-representations.html), and
   the reader has to know which.  The shapes data above uses one of them,
   with the tag in a `type` field.
2. The database stores the JSON in a JSON column, where its type is `JSON`,
   or shreds it into tables using one of the
   [workarounds above](#the-frankentype).
3. A query returns rows or more JSON, and the application rebuilds its types
   from them, with a deserializer in Rust or a validation library like
   [Zod](https://zod.dev/) in TypeScript, checking at run time that the data
   still has the shape the program expects.

The types exist at both ends and nowhere in between, and every hop is a
place where the two ends can drift apart.

With the ADM, there is nothing to erase because each of these types has a
direct counterpart, shown here for the super data model:

| Rust | TypeScript | Super data model |
|---|---|---|
| `struct` | object type | [record](super-sql/types/record.md) |
| `enum` with data | discriminated union | [union](super-sql/types/union.md) of [named types](super-sql/types/named.md) |
| `enum` without data | union of string literals | [enum](super-sql/types/enum.md) |
| `Option<T>` | optional property | [option](formats/model.md#29-option) |
| `Vec<T>` | `T[]` | [array](super-sql/types/array.md) |
| `HashSet<T>` | `Set<T>` | [set](super-sql/types/set.md) |
| `HashMap<K, V>` | `Map<K, V>` | [map](super-sql/types/map.md) |

So the shapes can be given the type
```
type Rectangle = {width:int64,height:int64}
type Circle = {radius:int64}
type Square = {side:int64}
type Vector = [int64]
type Dimensions = Rectangle|Circle|Square|Vector
type Shape = {id:int64,dimensions:Dimensions}
```
which is the Rust type from above, almost token for token.  Because each
member of `Dimensions` is a [named type](super-sql/types/named.md), the name
says which kind of shape a value is, and the `"type"` string that the JSON
had to carry is no longer needed.

There is no mapping layer to configure and no second copy of the schema to
keep in sync.  One type, declared once in the program, is the type on the
wire, in the file, in the table, and in the query.

With the ADM, data boomerangs from the program to the database and back
with no loss of fidelity.

[Algebraic Types, End to End](blog/end-to-end.md) walks through this round
trip in Rust and in TypeScript.

## Failed Attempts to Replace the RM

The ADM is far from the first attempt to move beyond the relational model,
and the battlefield is littered with the corpses of the others.
Pavlo and Stonebraker's
[paper](https://db.cs.cmu.edu/papers/2024/whatgoesaround-sigmodrec2024.pdf)
is a tour of that battlefield, and two of its casualties are worth a
closer look.

**Object databases** of the late 1980s and 1990s made the programming
language's objects persistent.  That closed the gap between a program's
types and its database's types, but only for one language at a time:
a database of C++ or Smalltalk objects, with their
pointers, identity, methods, and inheritance, is of little use to a program
written in anything else.  Queries meant navigating from object to object
rather than declaring a result, no standard took hold, and SQL was left
behind along with every tool built on it.

**XML databases** around 2000 offered yet another model, a tree of elements and
attributes with its own schema language and its own query language.  It
matched neither side: it isn't how programs structure their data and it
isn't relational.  SQL absorbed XML as a column type, JSON then displaced
XML, and SQL absorbed that, too.

What survived was not a replacement at all.  **Object-relational mappers**
keep the RM in the database and translate between objects and rows in a
library.  They live with that gap, known as the
[object-relational impedance mismatch](https://en.wikipedia.org/wiki/Object%E2%80%93relational_impedance_mismatch),
rather than close it.  ORMs are everywhere, and so are
complaints about them, because the translation never quite disappears.
The trouble starts exactly where sum types do.  To store a class hierarchy,
an ORM offers
[single-table inheritance](https://martinfowler.com/eaaCatalog/singleTableInheritance.html),
[class-table inheritance](https://martinfowler.com/eaaCatalog/classTableInheritance.html),
or a [serialized LOB](https://martinfowler.com/eaaCatalog/serializedLOB.html),
which are the wide table, the normalized tables, and the JSON column from
[above](#the-frankentype).  The ORM automates the workarounds,
but the sum type is still missing.

We don't dispute this history.  But look at what the three have in common.
Each casualty asked the world to move, to commit to one language's object
model or to give up SQL, and the survivor asks it to accept a mapping layer
forever.  The ADM asks for none of that:
* There is nothing to map, because the database's types and the application's
  types are the same types.
* The types describe values, not objects.  Records, unions, and arrays have
  no identity, methods, pointers, or inheritance, so they mean the same thing
  in every language and in every format.
* Nothing is left behind.  A relational table is a special case, so SQL and
  schema-constrained tables keep working, and JSON is a special case, so
  existing data loads as is.  Algebraic types can be adopted one table,
  or one column, at a time.
* The languages have already arrived.  In the 1990s, mainstream languages had
  classes and little else, so a database had to choose between tables and
  objects.  Today sum types are standard equipment, and the ADM gives
  programmers the types they already write.

So the ADM is not a challenger to the RM in the paper's sense.  It is a
generalization that contains it, and we think the time has come for the
relational model to become a special case of an easier and broader approach
for modern data.  When the RM absorbed JSON, it got a frankentype.  When the
ADM absorbs the RM, it gets tables as a special case.

We know the odds, and we are only cautiously optimistic that SuperDB and the
ADM won't be the next corpse on that battlefield.  But everything buried
there tried to leave the relational model behind.  The ADM brings it along.
