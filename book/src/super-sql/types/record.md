# Records

Records conform to the
[record type](../../formats/model.md#21-record) in the
super data model and follow the
[syntax](../../formats/sup.md#251-record-type)
of records in the [SUP format](../../formats/sup.md), i.e.,
a record type has the form
```
{ <name> : <type>, <name> : <type>, ... }
```
where `<name>` is an identifier or string
and `<type>` is any type.

Any SUP text defining a [record value](../../formats/sup.md#241-record-value)
is a valid record literal in the SuperSQL language.

For example, this is a simple record value
```
{number:1,message:"hello,world"}
```
whose type is
```
{number:int64,message:string}
```
An empty record value and an empty record type are both represented as `{}`.

Records can be created by reading external data (SUP files,
database data, Parquet values, JSON objects, etc) or by
constructing instances using
[record expressions](#record-expressions) or other
SuperSQL functions that produce records.

## Record Expressions

Record values are constructed from a _record expression_ that is comprised of
zero or more comma-separated elements contained in braces:
```
{ <element>, <element>, ... }
```
where an `<element>` has one of three forms:

* a named field of the form `<name> : <expr>`  where `<name>` is an
identifier or string and `<expr>` is any [expression](../expressions/intro.md),
* any [expression](../expressions/intro.md) by itself where the field name
  is derived from the expression text as defined below, or
* a spread expression of the form `...<expr>` where `<expr>` is an arbitrary
[expression](../expressions/intro.md) that should evaluate to a record value.

The spread form inserts all of the fields from the resulting record.
If a spread expression results in a non-record type (e.g., errors), then that
part of the record is simply elided.  Note that the field names for
the spread come from the constituent record values.
When the spread expression results in a [union](union.md) value, the value
under the union is spread, so a record member of the union contributes its
fields and a member that isn't a record contributes none, e.g.,
```mdtest-spq {data-layout="stacked"}
# spq
values {...this, x:1}
# input
{a:1}::({a:int64}|{b:string})
{b:"foo"}::({a:int64}|{b:string})
1::(int64|{a:int64})
# expected output
{a:1,x:1}
{b:"foo",x:1}
{x:1}
```

The fields of a record expression are evaluated left to right and when
field names collide the rightmost instance of the name determines that
field's value.

## Derived Field Names

When an expression is present without a field name,
the field name is derived from the expression text as follows:
* for a dotted path expression, the name is the last element of the path;
* for a function or aggregate function, the name is the name of the function;
* for a double-quoted string, the name is the text between the quotes;
* for `this`, the name is `that`;
* otherwise, the name is the expression text formatted in a canonical form.

>[!NOTE]
> The double-quote rule follows from the dual meaning of double quotes for
> [string types](string.md): in a SQL expression double quotes denote a field
> identifier, while in a pipe expression they denote a string. The derived field
> name comes from the quoted text in both cases. A consequence in pipe expressions
> is that a double-quoted string and a single-quoted string of equal value
> derive different names, since only the former matches this rule and the
> latter falls through to the canonical form.

## Examples

---

_A simple record literal_

```mdtest-spq
# spq
values {a:1,b:2,s:"hello"}
# input

# expected output
{a:1,b:2,s:"hello"}
```

---

_A record expression with spreads operating on various input values_

```mdtest-spq
# spq
values {a:0},{x}, {...r}, {a:0,...r,b:3}
# input
{x:1,y:2,r:{a:1,b:2}}
# expected output
{a:0}
{x:1}
{a:1,b:2}
{a:1,b:3}
```

---

_A record literal with casts_

```mdtest-spq {data-layout="stacked"}
# spq
type CustomString=string
values {b:true,u:1::uint8,a:[1,2,3],s:"hello"::CustomString}
# input

# expected output
type CustomString=string
{b:true,u:1::uint8,a:[1,2,3],s:"hello"::CustomString}
```

---

_Various derived field names_

```mdtest-spq {data-layout="stacked"}
# spq
values {a.b,upper(a.b),"x y",'x y',this,a.b || "d"}
# input
{a:{b:"c"}}
# expected output
{b:"c",upper:"C","x y":"x y","\"x y\"":"x y",that:{a:{b:"c"}},"a.b||\"d\"":"cd"}
```

---

_Derived field names inside a SELECT_

```mdtest-spq
# spq
select {1+2*3} as x,sum(a)
# input
{a:1}
{a:2}
# expected output
{x:{"1+2*3":7},sum:3}
```
