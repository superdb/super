# Super-structured Data

 **TODO: update this doc.  drop TLDR.  Lead with "does the world need another data model?
 emphasize it kinds of looks like other things but is subtly different and that subtle
 difference is the key to a unified model.**

>[!NOTE]
> **TL;DR** The super-structured data model defines a new and easy way to manage, store,
> and process data utilizing an collections of strongly yet heterogeneously
typed values.
> The [data model specification](../formats/model.md) defines the type system
> and its semantics. which is embodied
> in a [family of interoperable serialization formats](#the-data-model-and-formats),
> providing a unified approach to row, columnar, and human-readable formats. Together these
> represent a superset of both the dataframe/table model of relational systems and the
> semi-structured model that is used ubiquitously in development as JSON and by NoSQL
> data stores.  The Super (SUP) format spec has
> [a few examples](../formats/sup.md#3-examples).

## Structured-ness

Modern data models are typically described in terms of their _structured-ness_:
* _tabular-structured_, often simply called _"structured"_,
where a specific schema is defined to describe a table and values are enumerated that conform to that schema;
* _semi-structured_, where arbitrarily complex, hierarchical data structures
define the data and values do not fit neatly into tables, e.g., JSON and XML; and
* _unstructured_, where arbitrary text is formatted in accordance with
external, often vague, rules for its interpretation.

### The Tabular-structured Pattern

CSV is arguably the simplest but most frustrating format that follows the tabular-structured
pattern.  It provides a bare bones schema consisting of the names of the columns as the
first line of a file followed by a list of comma-separated, textual values
whose types must be inferred from the text.  The lack of a universally adopted
specification for CSV is an all too common source of confusion and frustration.

The traditional relational database, on the other hand,
offers the classic, comprehensive example of the tabular-structured pattern.
The table columns have precise names and types.
Yet, like CSV, there is no universal standard format for relational tables.
The [SQLite file format](https://sqlite.org/fileformat.html)
is arguably the _de facto_ standard for relational data,
but this format describes a whole, specific database --- indexes and all ---
rather than a stand-alone table.

Instead, file formats like Avro, ORC, and Parquet arose to represent tabular data
with an explicit schema followed by a sequence of values that conform to the schema.
While Avro and Parquet schemas can also represent semi-structured data, all of the
values in a given Avro or Parquet file must conform to the same schema.
The [Iceberg specification](https://iceberg.apache.org/spec/)
defines data types and metadata schemas for how large relational tables can be
managed as a collection of Avro, ORC, and/or Parquet files.

### The Semi-structured Pattern

JSON, on the other hand, is the ubiquitous example of the semi-structured pattern.
Each JSON value is self-describing in terms of its
structure and types, though the JSON type system is limited.

When a sequence of JSON objects is organized into a stream
(perhaps [separated by newlines](https://en.wikipedia.org/wiki/JSON_streaming#NDJSON))
each value can take on any form.
When all the values have the same form, the JSON sequence
begins to look like a relational table, but the lack of a comprehensive type system,
a union type, and precise semantics for columnar layout limits this interpretation.

[BSON](https://bsonspec.org/)
and [Ion](https://amzn.github.io/ion-docs/)
were created to provide a type-rich elaboration of the
semi-structured model of JSON along with performant binary representations
though there is no mechanism for precisely representing the type of
a complex value like an object or an array other than calling it
type "object" or type "array", e.g., as compared to "object with field s
of type string" or "array of number".

[JSON Schema](https://json-schema.org/)
addresses JSON's lack of schemas with an approach to augment
one or more JSON values with a schema definition itself expressed in JSON.
This creates a parallel type system for JSON, which is useful and powerful in many
contexts, but introduces schema-management complexity when simply trying to represent
data in its natural form.

### The Hybrid Pattern

As the utility and ease of the semi-structured design pattern emerged,
relational system design, originally constrained by the tabular-structured
design pattern, has embraced the semi-structured design pattern
by adding support for semi-structured table columns.
This leads to a temptation to "just put JSON in a column."

[SQL++](https://asterixdb.apache.org/docs/0.9.7.1/sqlpp/manual.html)
pioneered the extension of SQL to semi-structured data by
adding support for referencing and unwinding complex, semi-structured values,
and most modern SQL query engines have adopted variations of this model
and have extended the relational model with a semi-structured column type,
often referred to as a "JSON column".

But once you have put a number of columns of JSON data into a relational
table, is it still appropriately called "structured"?
Instead, we call this approach the hybrid tabular-/semi-structured pattern,
or more simply, _"the hybrid pattern"_.

## A Super-structured Pattern

The [super data model](../formats/model.md) removes the tabular and schema concepts from
the underlying data model altogether and replaces them with a granular and
modern type system inspired by general-purpose programming languages.
Instead of defining a single, composite schema to
which all values must conform, the type system allows each value to freely
express its type in accordance with the type system.

In this approach, data in SuperDB is neither tabular nor semi-structured but
instead "super-structured".

In particular, SuperDB's [record type](../formats/model.md#21-record) looks like a schema but when values are
serialized, the model is very different.  A sequence of values need not
comprise a record-type declaration followed by a sequence of
homogeneously-typed record values, but instead,
is a sequence of arbitrarily typed values which may or may not all
be records.

Yet when a sequence of values _in fact conforms to a uniform record type_,
then such a collection of records looks precisely like a relational table.
Here, the record type
of such a collection corresponds to a well-defined schema consisting
of field names (i.e., column names) where each field has a specific type.
[Named types](../formats/model.md#3-named-type) are also available, so by simply naming a particular record type
(i.e., a schema), a relational table can be projected from a super-structured data
with a simple query for that named type.

But unlike traditional relational tables, these tables based on super-structured data can have arbitrary
structure in each column as the model allows the fields of a record
to have arbitrary types.  This is very different compared to the hybrid pattern:
here all data at all levels conforms to the same data model such that the
tabular-structured and semi-structured patterns are representable in a single model.
Unlike the hybrid pattern, systems based on super-structured data have
no need to simultaneously support two very different data models.

In other words, SuperDB unifies the relational data model of SQL tables
with the document model of JSON into a _super-structured_
design pattern enabled by the type system.
An explicit, uniquely-defined type of each value precisely
defines its entire structure, i.e., its super-structure.  There is
no need to traverse each hierarchical value --- as with JSON, BSON, or Ion ---
to discover each value's structure.

And because SuperDB derives its design from the vast landscape
of existing formats and data models, it was deliberately designed to be
a superset of --- and thus interoperable with --- a broad range of formats
including JSON, BSON, Ion, Avro, ORC, Parquet, CSV, JSON Schema, and XML.

As an example, most systems that are based on semi-structured data would
say the JSON value
```
{"a":[1,"foo"]}
```
is of type object and the value of key `a` is type array.
In SuperDB, however, this value's type is type `record` with field `a`
of type `array` of type `union` of `int64` and `string`,
expressed succinctly in Super (SUP) format as
```
{a:[int64|string]}
```
This is super-structuredness in a nutshell.

### Schemas

While the super data model removes the schema constraint,
the implication here is not that schemas are unimportant;
to the contrary, schemas are foundational.  Schemas not only define agreement
and semantics between communicating entities, but also serve as the cornerstone
for organizing and modeling data for data engineering and business intelligence.

That said, schemas often create complexity in system designs
where components might simply want to store and communicate data in some
meaningful way.  For example, an ETL pipeline should not break when upstream
structural changes prevent data from fitting in downstream relational tables.
Instead, the pipeline should continue to operate and the data should continue
to land on the target system without having to fit into a predefined table,
while also preserving its super-structure.

This is precisely what SuperDB enables.  A system layer above and outside
the scope of the data layer can decide how to adapt to the structural
changes with or without administrative intervention.

To this end, whether all the values must conform to a schema and
how schemas are managed, revised, and enforced is all outside the scope of the super data model;
rather, the data model provides a flexible and rich foundation
for schema interpretation and management.

### Type Combinatorics

A common objection to using a type system to represent schemas is that
diverse applications generating arbitrarily structured data can produce
a combinatorial explosion of types for each shape of data.

In practice, this condition rarely arises.  Applications generating
"arbitrary" JSON data generally conform to a well-defined set of
JSON object structures.

A few rare applications carry unique data values as JSON object keys,
though this is considered bad practice.

Even so, this is all manageable in the super data model as types are localized
in scope.  The number of types that must be defined in a stream of values
is linear in the input size.  Since data is self-describing and there is
no need for a global schema registry in SuperDB, this hypothetical problem is moot.

### Analytics Performance

One might think that removing schemas from the super data model would conflict
with an efficient columnar format, which is critical for
high-performance analytics.
After all, database
tables and formats like Parquet and ORC all require schemas to organize values
and then rely upon the natural mapping of schemas to columns.

Super-structure, on the other hand, provides an alternative approach to columnar structure.
Instead of defining a schema and then fitting a sequence of values into their appropriate
columns based on the schema, values self-organize into columns based on their
super-structure.  Here columns are created dynamically as data is analyzed
and each top-level type induces a specific set of columns.  When all of the
values have the same top-level type (i.e., like a schema), then the columnar
object is just as performant as a traditional schema-based columnar format like Parquet.

### First-class Types

With [first-class types](../super-sql/types/type.md), any type can also be a value, which means that in
a properly designed query and analytics system based on the super data model, a type can appear
anywhere that a value can appear.  In particular, types can be grouping keys.

This is very powerful for data discovery and introspection.  For example,
to count the different shapes of data, you might have a SuperSQL query,
operating on each input value as `this`, that has the form:

```sql
  SELECT count(), typeof(this) AS shape GROUP BY shape, count
```
Likewise, you could select a sample value of each shape like this:

```sql
  SELECT shape FROM (
    SELECT any(this) AS sample, typeof(this) AS shape GROUP BY shape,sample
  )
```
The SuperSQL language provides shortcuts that express such operations in ways
that more directly leverage the nature of super-structured data. For example,
the above two queries could be written as:
```
  count() by shape:=typeof(this)
  any(this) by typeof(this) | cut any
```

### First-class Errors

In SQL based systems, errors typically
result in cryptic messages or null values offering little insight as to the
actual cause of the error.

By comparison, SuperDB includes [first-class errors](../super-sql/types/error.md).
When combined with the super data model,
error values may appear anywhere in the output and operators
can propagate or easily wrap errors so complicated analytics pipelines
can be debugged by observing the location of errors in the output results.

## The Data Model and Formats

The concept of super-structured data and first-class types and errors
is solidified in the [data model specification](../formats/model.md),
which defines the model but not the serialization formats.

A set of companion documents define a family of tightly integrated
serialization formats that all adhere to the same super data model,
providing a unified approach to row, columnar, and human-readable formats:

* [Super (SUP)](../formats/sup.md) is a human-readable format for super-structured data.  All JSON
documents are SUP values as the SUP format is a strict superset of the JSON syntax.
* [Super Binary (BSUP)](../formats/bsup.md) is an efficient binary representation
of a sequence of arbitrarily-typed values in either rows, somewhat like Avro, or
columns, somewhat like Parquet or ORC, that embodies the super data model for
heterogeneous and self-describing schemas.

Because all of the formats conform to the same super data model, conversions between
a human-readable form and a binary row or columnar form can
be trivially carried out with no loss of information.  This is the best of both worlds:
the same data can be easily expressed in and converted between a human-friendly
and easy-to-program text form alongside efficient row and columnar formats.
