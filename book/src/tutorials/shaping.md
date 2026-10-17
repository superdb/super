# Shaping

>**TODO: update this to use cast, fuse, type fusion, etc?
> This should be more about why SuperSQL is good for data shaping in general,
> rather than focusing on particular operators.**

## Shaping

Data that originates from heterogeneous sources typically has
inconsistent structure and is thus difficult to reason about or query.
To unify disparate data sources, data is often cleaned up to fit into
a well-defined set of schemas, which combines the data into a unified
store like a data warehouse.

In Zed, this cleansing process is called "shaping" the data, and Zed leverages
its rich, [super-structured](../formats/model.md)
type system to perform core aspects of data transformation.
In a data model with nesting and multiple scalar types (such as Zed or JSON),
shaping includes converting the type of leaf fields, adding or removing fields
to "fit" a given shape, and reordering fields.

While shaping remains an active area of development, the core functions in Zed
that currently perform shaping are:

* [`cast`](../super-sql/functions/types/cast.md) - coerce a value to a different type
XXX

They all have the same signature, taking two parameters: the value to be
transformed and a [type value](../super-sql/types/type.md) for the target type.

>[!NOTE]
> Another type of transformation that's needed for shaping is renaming fields,
> which is supported by the [rename](../super-sql/operators/rename.md) operator.
> Also, the [values](../super-sql/operators/values.md) operator
> is handy for simply emitting new, arbitrary record literals based on
> input values and mixing in these shaping functions in an embedded record literal.
> The [fuse](../super-sql/aggregates/fuse.md) aggregate function is also useful for fusing
> values into a common schema, though a type is returned rather than values.

In the examples below, we will use the following named type `connection`:
```
type socket = { addr:ip, port:port=uint16 }
type connection = {
    kind: string,
    client: socket,
    server: socket,
    vlan: uint16
}
```
We'll also use this sample JSON input:
```
{
  "kind": "dns",
  "server": {
    "addr": "10.0.0.100",
    "port": 53
  },
  "client": {
    "addr": "10.47.1.100",
    "port": 41772
  },
  "uid": "C2zK5f13SbCtKcyiW5"
}
```

### Cast

The `cast` function applies a cast operation to each leaf value that matches the
field path in the specified type.

In the following example we cast the address fields to type `ip`, the port fields to type `port`
(which is a [named type](../super-sql/types/named.md) for type `uint16`) and the address port pairs to
type `socket` without modifying the `uid` field or changing the
order of the `server` and `client` fields:

```mdtest-spq-skip {data-layout="stacked"}
# spq
type socket = { addr:ip, port:port=uint16 }
type connection = {
    kind: string,
    client: socket,
    server: socket,
    vlan: uint16
}

cast(this, <connection>)
# input
{
  "kind": "dns",
  "server": {
    "addr": "10.0.0.100",
    "port": 53
  },
  "client": {
    "addr": "10.47.1.100",
    "port": 41772
  },
  "uid": "C2zK5f13SbCtKcyiW5"
}
# expected output
{kind:"dns",server:{addr:10.0.0.100,port:53::(port=uint16)}::=socket,client:{addr:10.47.1.100,port:41772}::socket,uid:"C2zK5f13SbCtKcyiW5"}
```

### Crop

Cropping is useful when you want records to "fit" a schema tightly.

In the following example we remove the `uid` field since it is not in the `connection` type:

```mdtest-spq-skip {data-layout="stacked"}
# spq
type socket = { addr:ip, port:port=uint16 }
type connection = {
    kind: string,
    client: socket,
    server: socket,
    vlan: uint16
}

crop(this, <connection>)
# input
{
  "kind": "dns",
  "server": {
    "addr": "10.0.0.100",
    "port": 53
  },
  "client": {
    "addr": "10.47.1.100",
    "port": 41772
  },
  "uid": "C2zK5f13SbCtKcyiW5"
}
# expected output
{kind:"dns",server:{addr:"10.0.0.100",port:53},client:{addr:"10.47.1.100",port:41772}}
```

### Fill

Use `fill` when you want to fill out missing fields with nulls.

In the following example we add a null-valued `vlan` field since the input value is missing it and
the `connection` type has it:
```mdtest-spq-skip {data-layout="stacked"}
# spq
type socket = { addr:ip, port:port=uint16 }
type connection = {
    kind: string,
    client: socket,
    server: socket,
    vlan: uint16
}

fill(this, <connection>)
# input
{
  "kind": "dns",
  "server": {
    "addr": "10.0.0.100",
    "port": 53
  },
  "client": {
    "addr": "10.47.1.100",
    "port": 41772
  },
  "uid": "C2zK5f13SbCtKcyiW5"
}
# expected output
{kind:"dns",server:{addr:"10.0.0.100",port:53},client:{addr:"10.47.1.100",port:41772},uid:"C2zK5f13SbCtKcyiW5",vlan:null::uint16}
```

### Order

The `order` function changes the order of fields in its input to match the
order in the specified type, as field order is significant in Zed records.

The following example reorders the `client` and `server` fields to match
the input but does nothing about the `uid` field as it is not in the
`connection` type.

```mdtest-spq-skip {data-layout="stacked"}
# spq
type socket = { addr:ip, port:port=uint16 }
type connection = {
    kind: string,
    client: socket,
    server: socket,
    vlan: uint16
}

order(this, <connection>)
# input
{
  "kind": "dns",
  "server": {
    "addr": "10.0.0.100",
    "port": 53
  },
  "client": {
    "addr": "10.47.1.100",
    "port": 41772
  },
  "uid": "C2zK5f13SbCtKcyiW5"
}
# expected output
{kind:"dns",client:{addr:"10.47.1.100",port:41772},server:{addr:"10.0.0.100",port:53},uid:"C2zK5f13SbCtKcyiW5"}
```

As an alternative to the `order` function,
[record expressions](../super-sql/types/record.md#record-expressions)
can be used to reorder fields without specifying types. For example:

```mdtest-spq-skip {data-layout="stacked"}
# spq
type socket = { addr:ip, port:port=uint16 }
type connection = {
    kind: string,
    client: socket,
    server: socket,
    vlan: uint16
}

values {kind,client,server,...this}
# input
{
  "kind": "dns",
  "server": {
    "addr": "10.0.0.100",
    "port": 53
  },
  "client": {
    "addr": "10.47.1.100",
    "port": 41772
  },
  "uid": "C2zK5f13SbCtKcyiW5"
}
# expected output
{kind:"dns",client:{addr:"10.47.1.100",port:41772},server:{addr:"10.0.0.100",port:53},uid:"C2zK5f13SbCtKcyiW5"}
```

### Shape

The `shape` function brings everything together by applying `cast`,
`fill`, and `order` all in one step.

In the following example we reorder the `client` and `server` fields to match
the input but do not impact the `uid` field as it is not in the `connection` type.

```mdtest-spq-skip {data-layout="stacked"}
# spq
type socket = { addr:ip, port:port=uint16 }
type connection = {
    kind: string,
    client: socket,
    server: socket,
    vlan: uint16
}

shape(this, <connection>)
# input
{
  "kind": "dns",
  "server": {
    "addr": "10.0.0.100",
    "port": 53
  },
  "client": {
    "addr": "10.47.1.100",
    "port": 41772
  },
  "uid": "C2zK5f13SbCtKcyiW5"
}
# expected output
{kind:"dns",client:{addr:10.47.1.100,port:41772::(port=uint16)}::=socket,server:{addr:10.0.0.100,port:53}::socket,vlan:null::uint16,uid:"C2zK5f13SbCtKcyiW5"}
```

To get a tight shape of the target type,
apply `crop` to the output of `shape`, e.g.,
to dropping the `uid` after shaping:
```mdtest-spq-skip {data-layout="stacked"}
# spq
type socket = { addr:ip, port:port=uint16 }
type connection = {
    kind: string,
    client: socket,
    server: socket,
    vlan: uint16
}

shape(this, <connection>)
| crop(this, <connection>)
# input
{
  "kind": "dns",
  "server": {
    "addr": "10.0.0.100",
    "port": 53
  },
  "client": {
    "addr": "10.47.1.100",
    "port": 41772
  },
  "uid": "C2zK5f13SbCtKcyiW5"
}
# expected output
{kind:"dns",client:{addr:10.47.1.100,port:41772::(port=uint16)}::=socket,server:{addr:10.0.0.100,port:53}::socket,vlan:null::uint16}
```

## Error Handling

A failure during shaping produces an [error value](../super-sql/types/error.md)
in the problematic leaf field.

In the next two examples, we use a malformed variation of our input data. When
we apply our shaper to it, we now see two errors.

```mdtest-spq-skip {data-layout="stacked"}
# spq
type socket = { addr:ip, port:port=uint16 }
type connection = {
    kind: string,
    client: socket,
    server: socket,
    vlan: uint16
}

shape(this, <connection>)
# input
{
  "kind": "dns",
  "server": {
    "addr": "10.0.0.100",
    "port": 53
  },
  "client": {
    "addr": "39 Elm Street",
    "port": 41772
  },
  "vlan": "available"
}
# expected output
{kind:"dns",client:{addr:error({message:"cannot cast to ip",on:"39 Elm Street"}),port:41772::(port=uint16)},server:{addr:10.0.0.100,port:53::port}::=socket,vlan:error({message:"cannot cast to uint16",on:"available"})}
```

Since these error values are nested inside an otherwise healthy record, adding
[`has_error(this)`](../super-sql/functions/errors/has_error.md) downstream in our pipeline
could help find or exclude such records.  If the failure to shape _any_ single
field is considered severe enough to render the entire input record unhealthy,
[a conditional expression](../super-sql/expressions/conditional.md)
could be applied to wrap the input record as an error while including detail
to debug the problem, e.g.,

```mdtest-spq-skip {data-layout="stacked"}
# spq
type socket = { addr:ip, port:port=uint16 }
type connection = {
    kind: string,
    client: socket,
    server: socket,
    vlan: uint16
}

values {original: this, shaped: shape(this, <connection>)}
| values has_error(shaped)
  ? error({
      msg: "shaper error (see inner errors for details)",
      original,
      shaped
    })
  : shaped
# input
{
  "kind": "dns",
  "server": {
    "addr": "10.0.0.100",
    "port": 53
  },
  "client": {
    "addr": "39 Elm Street",
    "port": 41772
  },
  "vlan": "available"
}
# expected output
error({msg:"shaper error (see inner errors for details)",original:{kind:"dns",server:{addr:"10.0.0.100",port:53},client:{addr:"39 Elm Street",port:41772},vlan:"available"},shaped:{kind:"dns",client:{addr:error({message:"cannot cast to ip",on:"39 Elm Street"}),port:41772::(port=uint16)},server:{addr:10.0.0.100,port:53::port}::=socket,vlan:error({message:"cannot cast to uint16",on:"available"})}})::error({msg:string,original:{kind:string,server:{addr:string,port:int64},client:{addr:string,port:int64},vlan:string},shaped:{kind:string,client:{addr:error({message:string,on:string}),port:port=uint16},server:socket={addr:ip,port:port},vlan:error({message:string,on:string})}})
```

If you require awareness about changes made by the shaping functions that
aren't surfaced as errors, a similar wrapping approach can be used with a
general check for equality. For example, to treat cropped fields as an error,
we can execute

```mdtest-spq-skip {data-layout="stacked"}
# spq
type socket = { addr:ip, port:port=uint16 }
type connection = {
    kind: string,
    client: socket,
    server: socket,
    vlan: uint16
}

values {original: this, cropped: crop(this, <connection>)}
| values original==cropped
  ? original
  : error({msg: "data was cropped", original, cropped})
# input
{
  "kind": "dns",
  "server": {
    "addr": "10.0.0.100",
    "port": 53
  },
  "client": {
    "addr": "10.47.1.100",
    "port": 41772
  },
  "uid": "C2zK5f13SbCtKcyiW5"
}
# expected output
error({msg:"data was cropped",original:{kind:"dns",server:{addr:"10.0.0.100",port:53},client:{addr:"10.47.1.100",port:41772},uid:"C2zK5f13SbCtKcyiW5"},cropped:{kind:"dns",server:{addr:"10.0.0.100",port:53},client:{addr:"10.47.1.100",port:41772}}})
```

## Type Fusion

Type fusion is another important building block of data shaping.
Here, types are operated upon by fusing them together, where the
result is a single fused type.
Some systems call a related process "schema inference" where a set
of values, typically JSON, is analyzed to determine a relational schema
that all the data will fit into.  However, this is just a special case of
type fusion as fusion is fine-grained and based on Zed's type system rather
than having the narrower goal of computing a schema for representations
like relational tables, Parquet, Avro, etc.

Type fusion utilizes two key techniques.

The first technique is to simply combine types with a type union.
For example, an `int64` and a `string` can be merged into a common
type of union `int64|string`, e.g., the value sequence `1 "foo"`
can be fused into the single-type sequence:
```
1::(int64|string)
"foo"::(int64|string)
```
The second technique is to merge fields of records, analogous to a spread
expression.  Here, the value sequence `{a:1}{b:"foo"}` may be
fused into the single-type sequence:
```
{a:1,b:null::string}
{a:null::int64,b:"foo"}
```

Of course, these two techniques can be powerfully combined,
e.g., where the value sequence `{a:1}{a:"foo",b:2}` may be
fused into the single-type sequence:
```
{a:1::(int64|string),b:null::int64}
{a:"foo"::(int64|string),b:2}
```

To perform fusion, Zed currently includes two key mechanisms
(though this is an active area of development):
* the [`fuse` operator](../super-sql/operators/fuse.md), and
* the [`fuse` aggregate function](../super-sql/aggregates/fuse.md).

### Fuse Operator

The `fuse` operator reads all of its input, computes a fused type using
the techniques above, and outputs the result, e.g.,
```mdtest-spq
# spq
blend
# input
{x:1}
{y:"foo"}
{x:2,y:"bar"}
# expected output
{x:1::(int64|missing),y:none::(string|missing)}
{x:none::(int64|missing),y:"foo"::(string|missing)}
{x:2::(int64|missing),y:"bar"::(string|missing)}
```

Whereas a type union for field `x` is produced in the following:
```mdtest-spq-skip {data-layout="stacked"}
# spq
blend
# input
{x:1}
{x:"foo",y:"foo"}
{x:2,y:"bar"}
# expected output
{x:1::(int64|string),y:null::string}
{x:"foo"::(int64|string),y:"foo"}
{x:2::(int64|string),y:"bar"}
```

### Fuse Aggregate Function

The `fuse` aggregate function is most often useful during data exploration and discovery
where you might interactively run queries to determine the shapes of some new
or unknown input data and how those various shapes relate to one another.

For example, in the example sequence above, we can use the `fuse` aggregate function to determine
the fused type rather than transforming the values, e.g.,
```mdtest-spq
# spq
blend(this)
# input
{x:1}
{x:"foo",y:"foo"}
{x:2,y:"bar"}
# expected output
<{x:int64|string,y:string|missing}>
```

Since the `fuse` here is an aggregate function, it can also be used with
grouping keys.  Supposing we want to divide records into categories and fuse
the records in each category, we can use a grouped aggregation.  In this simple example, we
will fuse records based on their number of fields using the
[`len` function:](../super-sql/functions/generics/len.md)
```mdtest-spq-skip {data-layout="stacked"}
# spq
fuse(this) by len(this) | sort len
# input
{x:1}
{x:"foo",y:"foo"}
{x:2,y:"bar"}
# expected output
{len:1,fuse:<{x:int64}>}
{len:2,fuse:<{x:int64|string,y:string}>}
```

Now, we can turn around and write a "shaper" for data that has the patterns
we "discovered" above, e.g.,
```mdtest-spq-skip {data-layout="stacked"}
# spq
switch len(this)
    case 1 ( pass )
    case 2 ( values shape(this, <{x:int64|string,y:string}>) )
    default ( values error({kind:"unrecognized shape",value:this}) )
| sort this desc
# input
{x:1}
{x:"foo",y:"foo"}
{x:2,y:"bar"}
{a:1,b:2,c:3}
# expected output
error({kind:"unrecognized shape",value:{a:1,b:2,c:3}})
{x:"foo"::(int64|string),y:"foo"}
{x:2::(int64|string),y:"bar"}
{x:1}
```
