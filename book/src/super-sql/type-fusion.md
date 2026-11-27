# Type Fusion

[_Type fusion_](https://openproceedings.org/2017/conf/edbt/paper-62.pdf)
is a process by which a set of input types is merged together
to form one output type where all values of the input types are subtypes
of the output type such that any value of an input type is representable in
a _reasonable way_ (e.g., by inserting null values) as an equivalent value
in the output type.

Type fusion is implemented by the [`fuse`](./aggregates/fuse.md) aggregate function.

A fused type determined by a collection of data can in turn be used in a
cast operation over that collection of data to create a new collection of
data comprising the single, fused type.  The process of transforming data
in this fashion is called _data fusion_.

Data fusion is implemented by the [`fuse`](./operators/fuse.md) operator.

The simplest fusion function is a
[sum type](https://en.wikipedia.org/wiki/Tagged_union), e.g., `int64` and `string` fuse
into `int64|string`.  More interesting fusion functions apply to record types, e.g.,
these two record types
```
{a:int64}
{b:string}
```
might fuse to type
```
{a:int64,b:string}
```

When the output type models a relational schema and the input types are derived
from semi-structured data, then this technique resembles
_schema inference_ in other systems.

>[!NOTE]
> Schema inference also involves the inference of particular primitive data types from
> string data when the strings represent dates, times, IP addresses, etc.
> The [infer](operators/infer.md) operator performs this function and can be easily applied to the
> output of type fusion.

A fused type computed over heterogeneous data represents a typical
design pattern of a data warehouse, where a relational table
with a single very-wide type-fused schema defines slots for all possible
input values and the columns are sparsely populated by each row value
with missing columns set to null.

While super-structured data natively represents heterogeneous data and
fortunately does not require a fused schema to persist data, type fusion
is nonetheless very useful:
* for _data exploration_, when sampling or filtering data to look at
slices of raw data that are fused together;
* for _exporting ADM data_ to other systems and formats,
where formats like Parquet or a tabular structure like CSV require fixed schemas; and,
* for _ETL_, where data might be gathered from APIs using SuperDB,
transformed in a SuperDB pipeline, and written to another data warehouse.

Unfortunately, when data leaves a super format using
type fusion to accomplish this, the original data must be altered
to fit into the rigid structure of these output formats.

## The Mechanism

The foundation of type fusion is to merge record types by their field names
while other types are generally merged with sum types.

For example, type `{a:int64}` merged with type `{b:string}`
would simply be type `{a:int64,b:string}`.

When fields overlap, `{a:int64,c:bool}` merged with type `{b:string,c:bool}`
would naturally be type `{a:int64,b:string,c:bool}`.

But when fields overlap _and_ their types conflict,
then a sum type is used to represent the overlapping types,
e.g., the types
```
{a:int64,c:bool}
{b:string,c:time}
```
are fused as
```
{a:int64,c:string,c:bool|time}
```

Arrays, maps, and sets are merged by merging their constituent types, e.g.,
the element type of the array or set and the key/value types of a map.

For example, the types
```
{a:[int64],b:|[string]|}
{a:[string]}
```
are fused as
```
{a:[int64|string],b:|[string]|}
```

## Detailed Algorithm

Type fusion may be formally defined as a function over types:
```
T = F(T1, T2, ... Tn)
```
where `T` is the fused type and `T1...Tn` are the input types.

When `F()` can be decomposed into an iterative application
of a merge function `m(type, type) -> type`
```
F(T1, T2, ... Tn) = m(m(m(T1, T2), T3), ... Tn)
```
then type fusion may be computing iteratively over a set of arbitrary types.

And when `m()` is commutative and associative, then the fused type can be computed
in parallel without respect to the input order of the data.

The merge function `m(t1,t2)` implemented by SuperSQL combines complex types
by merging their structure and combines otherwise incompatible types
with a union type as follows.

When `t1` and `t2` are different categories of types (e.g., record and array,
a set and a primitive type, two different primitive types, and so forth), then
the merged type is simply their sum type `t1|t2`.

When one type (e.g., `t1`) is a union type and the other (e.g., `t2`) is not,
then the `t2` is added to the elements of `t1` if not already a member of
the union.

When `t1` and `t2` are the same category of type, then they are merged as follows:
* for record types, the fields of `t1` and `t2` are matched by name and for
each matched field present in both types, the merged field is the recursive
merge of the two field types, and any non-matching fields are simply appended to
the resulting record type,
* for array types, the result is an array type whose element type is the recursive
merge of the two element types,
* for set types, the result is a set type whose element type is the recursive
merge of the two element types,
* for map types, the result is a set type whose key type is the recursive
merge of the two key types and whose value type is the recursive merge of the two
value type,
* for union types, the result is a union type comprising all the elemental types of
`t1` and `t2`,
* for error types, the result is an error type comprising the recursive merge of
the two contained types,
* for enum types, the result is the sum type of the two types `t1|t2`
* for named types, the result is the sum type of the two types `t1|t2`.

For further information and examples of type fusion, see the documentation for the
[`fuse`](./aggregates/fuse.md) aggregate function and the
[`fuse`](./operators/fuse.md) pipe operator.
