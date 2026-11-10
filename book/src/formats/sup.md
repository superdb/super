# Super (SUP) Format

## 1. Introduction

Super (SUP) is the human-readable, text-based serialization format for
[super-structured data](model.md).

SUP builds upon the elegant simplicity of JSON with [type decorators](#22-type-decorators).
Where the type of a value is not implied by its syntax, a type decorator
is appended to the value interposed with double colons
to establish a concrete type for every value expressed in source text.

SUP is also a superset of JSON in that all JSON documents are valid
SUP values.

## 2. The SUP Format

A SUP text is a sequence of UTF-8 characters organized either as a bounded input
or an unbounded stream.

The input text is organized as a sequence of one or more values and
[type declarations](#27-type-declarations) optionally
separated by and interspersed with whitespace.
Single-line (`//`) and multi-line (`/* ... */`) comments are
treated as whitespace and ignored.

All subsequent references to characters and strings in this section refer to
the Unicode code points that result when the stream is decoded.
If an input text includes data that is not valid UTF-8, then the text is invalid.

### 2.1 Names

SUP _names_ encode record fields, enum symbols, and named types.
A name is either an _identifier_ or a [quoted string](#231-strings).
Names are referred to as `<name>` below.

An _identifier_ is case-sensitive and can contain Unicode letters, `$`, `_`,
and digits `[0-9]`, but may not start with a digit.

### 2.2 Type Decorators

A value may be explicitly typed by tagging it with a type decorator.
The syntax for a decorator is a double-colon appended type:
```
<value>::<type>
```
For union values, multiple decorators might be
required to distinguish the union-member type from the possible set of
union types when there is ambiguity, as in
```
123.::float32::(int64|float32|float64)
```
In contrast, this union value is unambiguous:
```
123.::(int64|float64)
```

The syntax of a union value decorator is
```
<value>::<type>[::<type> ...]
```
where the rightmost type must be a union type if more than one decorator
is present.

A decorator of a complex value applies to the values within it, e.g.,
```
{p:80}::{p:uint16}
```
is the same as `{p:80::uint16}`.  A record decorator matches the fields of
the record value by name, and a field of the decorator that is
[optional](#249-option-value) and absent from the record value is none,
e.g., `{a:1}::{a:int64,b?:string}` is the same as
`{a:1,b?:none::string}`.

It is an error for the decorator to be type incompatible with its referenced value.

### 2.3 Primitive Values

The type names and format for
[primitive values](model.md#1-primitive-types) is as follows:

| Type       | Value Format                                                  |
|------------|---------------------------------------------------------------|
| `uint8`    | decimal string representation of any unsigned, 8-bit integer  |
| `uint16`   | decimal string representation of any unsigned, 16-bit integer |
| `uint32`   | decimal string representation of any unsigned, 32-bit integer |
| `uint64`   | decimal string representation of any unsigned, 64-bit integer |
| `int8`     | decimal string representation of any signed, 8-bit integer    |
| `int16`    | decimal string representation of any signed, 16-bit integer   |
| `int32`    | decimal string representation of any signed, 32-bit integer   |
| `int64`    | decimal string representation of any signed, 64-bit integer   |
| `duration` | a _duration string_ representing signed 64-bit nanoseconds |
| `timestamp` | an RFC 3339 date/time string representing signed 64-bit nanoseconds from epoch |
| `float16`  | a _non-integer string_ representing an IEEE-754 binary16 value |
| `float32`  | a _non-integer string_ representing an IEEE-754 binary32 value |
| `float64`  | a _non-integer string_ representing an IEEE-754 binary64 value |
| `bool`     | the string `true` or `false` |
| `bytes`    | a sequence of bytes encoded as a hexadecimal string prefixed with `0x` |
| `string`   | a double-quoted UTF-8 string |
| `ip`       | a string representing an IP address in [IPv4 or IPv6 format](https://tools.ietf.org/html/draft-main-ipaddr-text-rep-02#section-3) |
| `net`      | a string in CIDR notation representing an IP address and prefix length as defined in RFC 4632 and RFC 4291. |
| `uuid`     | a string of 32 hexadecimal digits in five groups separated by hyphens (8-4-4-4-12) as defined in RFC 9562 |
| `type`     | a type in canonical form as described in [Section 2.5](#25-types) enclosed in angle brackets |
| `null`     | the string `null` |
| `none`     | the string `none` |

The [reserved primitive types](model.md#1-primitive-types)
(e.g., `uint128`, `timestamp(3)`, and `date`) have no value format
as their values are not yet defined.  Their names may appear in types.

The format of a _duration string_
is an optionally-signed concatenation of decimal numbers,
each with optional fraction and a unit suffix,
such as "300ms", "-1.5h" or "2h45m", representing a 64-bit nanosecond value.
Valid time units are
"ns" (nanosecond),
"us" (microsecond),
"ms" (millisecond),
"s" (second),
"m" (minute),
"h" (hour),
"d" (day),
"w" (7 days), and
"y" (365 days).
Note that each of these time units accurately represents its calendar value,
except for the "y" unit, which does not reflect leap years and so forth.
Instead, "y" is defined as the number of nanoseconds in 365 days.

The format of floating point values is a _non-integer string_
conforming to any floating point representation that cannot be
interpreted as an integer, e.g., `1.` or `1.0` instead of
`1` or `1e3` instead of `1000`.  Unlike JSON, a floating point number can
also be one of:
`+Inf`, `-Inf`, or `NaN`.

A floating point value may be expressed with an integer string provided
a type decorator is applied, e.g., `123::float64`.

The following primitive types represent _implied-type_ values:
`int64`, `uint64`, `duration`, `timestamp`, `float64`, `bool`, `bytes`,
`string`, `ip`, `net`, `uuid`, `type`, `null`, and `none`.
Values for these types are determined by the format of the value and
thus do not need decorators to clarify the underlying type, e.g.,
```
123::int64
```
is the same as `123`.  An integer string is implied to be an `int64` unless
it is too large, in which case it is implied to be a `uint64`.

Values that do not have implied types must include a type decorator to clarify
its type or appear in a context for which its type is defined (i.e., as a field
value in a record, as an element in an array, etc.).

While a `type` value may represent a complex type, the value itself is a singleton
and thus always a primitive type.  A `type` value is encoded as:
* a left angle bracket `<`, followed by
* a type as [encoded below](#25-types), followed by
* a right angle bracket `>`.

A `timestamp` value corresponds to 64-bit Unix epoch nanoseconds and thus
not all possible RFC 3339 date/time strings are valid.  In addition,
nanosecond epoch times overflow on April 11, 2262.

#### 2.3.1 Strings

Double-quoted `string` syntax is the same as that of JSON as described
in [RFC 8259](https://tools.ietf.org/html/rfc8259#section-7).  Notably,
the following escape sequences are recognized:

| Sequence | Unicode Character      |
|----------|------------------------|
| `\"`     | quotation mark  U+0022 |
| `\\`     | reverse solidus U+005C |
| `\/`     | solidus         U+002F |
| `\b`     | backspace       U+0008 |
| `\f`     | form feed       U+000C |
| `\n`     | line feed       U+000A |
| `\r`     | carriage return U+000D |
| `\t`     | tab             U+0009 |
| `\uXXXX` |                 U+XXXX |

In `\uXXXX` sequences, each `X` is a hexadecimal digit, and letter
digits may be uppercase or lowercase.

The behavior of an implementation that encounters an unrecognized escape
sequence in a `string` type is undefined.

`\u` followed by anything that does not conform to the above syntax
is not a valid escape sequence.  The behavior of an implementation
that encounters such invalid sequences in a `string` type is undefined.

These escaping rules apply also to quoted field names in record values and
record types, enum symbols, and type names.

### 2.4 Complex Values

Complex values are built from primitive values and/or other complex values
and conform to the super data model's complex types:
[record](model.md#21-record),
[array](model.md#22-array),
[set](model.md#23-set),
[map](model.md#24-map),
[union](model.md#25-union),
[enum](model.md#26-enum),
[error](model.md#27-error),
[option](model.md#29-option), and
[fusion](model.md#28-fusion).

Complex values have an implied type when their constituent values all have
implied types.

#### 2.4.1 Record Value

A record value has the form:
```
{ <name> : <value>, <name> : <value>, ... }
```
where `<name>` is a [SUP name](#21-names) and `<value>` is
any optionally-decorated value inclusive of other records.
Each name/value pair is called a _field_.
There may be zero or more fields.

An [optional field](#249-option-value) has the form
```
<name> ?: <value>
```

#### 2.4.2 Array Value

An array value has the form:
```
[ <value>, <value>, ... ]
```
If the elements of the array are not of uniform type, then the implied type of
the array elements is a union of the types present.

An array value may be empty.  An empty array value without a type decorator is
presumed to be an empty array of type `[none]`.

#### 2.4.3 Set Value

A set value has the form:
```
set[ <value>, <value>, ... ]
```
where duplicate values are removed.

If the elements of the set are not of uniform type, then the implied type of
the set elements is a union of the types present.

A set value may be empty.  An empty set value without a type decorator is
presumed to be an empty set of type `set[none]`.

#### 2.4.4 Map Value

A map value has the form:
```
map{ <key> : <value>, <key> : <value>, ... }
```
where zero or more comma-separated, key/value pairs are present.

Whitespace around keys and values is generally optional, but to
avoid ambiguity, whitespace must separate an IPv6 key from the colon
that follows it.

An empty map value without a type decorator is
presumed to be an empty map of type `map{none:none}`.

#### 2.4.5 Union Value

A union value is a value that conforms to one of the types within a union type.
If the value appears in a context in which the type is unknown or ambiguous,
then the value must be decorated as [described above](#22-type-decorators).

#### 2.4.6 Enum Value

An enum type represents a symbol from a finite set of symbols
referenced by name.

An enum value is formed from a string representing the symbol followed
by an enum type decorator:
```
<string>::enum(<name>[,<name>...])
```
where each `<name>` is [SUP name](#21-names).

An enum value must appear in a context where the enum type is known, i.e.,
with an explicit enum type decorator or within a complex type where the
contained enum type is defined by the complex type's decorator.

A sequence of enum values might look like this:
```
type flip=enum(HEADS,TAILS)
"HEADS"::flip
"TAILS"::flip
"HEADS"::flip
```

#### 2.4.7 Error Value

An error value has the form:
```
error(<value>)
```
where `<value>` is any value.

#### 2.4.8 Fusion Value

A fusion value has the form:
```
fusion(<value>, <type-value>)
```
where `<value>` is a value of the fusion's supertype and `<type-value>`
is a [type value](#23-primitive-values) of its subtype, e.g.,
```
fusion(1::(int64|string), <int64>)
```

A value of type `fusion(all)` has the form:
```
<value>::any
```
where the value's subtype is the type of `<value>`.

#### 2.4.9 Option Value

An option value whose value is present has the form:
```
some(<value>)
```
where `<value>` may not be an option value,
and an option value whose value is absent has the form:
```
none::<type>
```
where `<type>` is the type of the option's value or the option type itself,
e.g., `none::int64` and `none::option(int64)` are the same value.
A value whose type is not an option type
may also be decorated with an option type, e.g., `1::option(int64)` is
the same as `some(1)`.

In a record value, an optional field `<name>?:<value>` is a field whose
value is an option value, where a `<value>` that is not an option value
is the same as `some(<value>)`, e.g.,
```
{a?:1,b?:none::string}
```
is the same as `{a:some(1),b:none::string}`.

### 2.5 Types

A primitive type is simply the name of the primitive type, i.e., `string`,
`uint16`, etc.  The `timestamp`, `timestamp_ntz`, and `time` types
may include a precision of 0, 3, 6, or 9 fractional digits, e.g.,
`timestamp(3)`, where `timestamp` is the same as `timestamp(9)`.
Complex types are defined as follows.

#### 2.5.1 Record Type

A _record type_ has the form:
```
{ <name> : <type>, <name> : <type>, ... }
```
where `<name>` is a [SUP name](#21-names) and
`<type>` is any type.

The order of the record fields is significant,
e.g., type `{a:int32,b:int32}` is distinct from type `{b:int32,a:int32}`.

An optional field has the form `<name> ?: <type>`, which is the same as
`<name> : option(<type>)`.

#### 2.5.2 Array Type

An _array type_ has the form:
```
[ <type> ]
```

#### 2.5.3 Set Type

A _set type_ has the form:
```
set[ <type> ]
```

#### 2.5.4 Map Type

A _map type_ has the form:
```
map{ <key-type> : <value-type> }
```
where `<key-type>` is the type of the keys and `<value-type>` is the
type of the values.

#### 2.5.5 Union Type

A _union type_ has the form:
```
<type> | <type> | ...
```
where there are at least two types in the list and
none is itself a union type.
A union type may be enclosed in parentheses, which are needed
where a union type would otherwise be ambiguous, e.g., when a decorator
is a union type as in `1::(int64|string)`.

Since a union type is an ordered set of types in the type system's order,
the order of the types in the list is not significant, e.g.,
`string|int64` is the same type as `int64|string`.

#### 2.5.6 Enum Type

An _enum type_ has the form:
```
enum( <name>, <name>, ... )
```
where `<name>` is a [SUP name](#21-names).
Each enum name must be unique and the order is significant, e.g.,
enum type `enum(HEADS,TAILS)` is not equal to type `enum(TAILS,HEADS)`.

#### 2.5.7 Error Type

An _error type_ has the form:
```
error( <type> )
```
where `<type>` is the type of the underlying values wrapped as an error.

#### 2.5.8 Named Type

A named type is referenced by its name, which is a [SUP name](#21-names),
and is defined by a [type declaration](#27-type-declarations).

#### 2.5.9 Fusion Type

A _fusion type_ has the form:
```
fusion( <type> )
```
where `<type>` is the fusion's supertype.

#### 2.5.10 Option Type

An _option type_ has the form:
```
option( <type> )
```
where `<type>` is the type of the option's value.

#### 2.5.11 Decimal Type

A _decimal type_ has the form:
```
decimal( <precision> [, <scale>] )
```
where `<precision>` and `<scale>` are integers and the scale defaults to 0.

### 2.6 Null and None Values

The [null value](model.md#11-null-and-none) is represented by
the string `null` and has type `null`.  A null value may be decorated
with `null` or with a union type that includes `null`, e.g.,
```
null::(int64|null)
```

The none value is represented by the string `none` and
has type `none`.  When decorated with a type that is not `none`,
it is an [option value](#249-option-value).

### 2.7 Type Declarations

A _type declaration_ binds a name to a type and has the form
```
type <name> = <type>
```
where `<name>` is a [SUP name](#21-names) and `<type>` is any type.
A type name may be any name other than the name of a
[primitive type](#23-primitive-values) (including `all` and `any`).

Type declarations appear in the sequence of values before or between values
but never inside a value, e.g., `{a:type x=int64 1}` is invalid.
A declaration binds the name for the value that follows it and for
all subsequent values in the sequence, so a type is declared once
and then referenced by name in any number of values, e.g.,
```
type port=uint16
{src:80::port,dst:8080::port}
{src:443::port,dst:9000::port}
```

A type name may be bound to only one type in a sequence of values.
A name may be declared again with the same type, but it is an error to
declare a name with a different type.

The declarations preceding a value are bound together, so they may refer to
each other in any order and to themselves, which allows recursive and
mutually recursive types, e.g.,
```
type tree={value:int64,children:forest}
type forest=[tree]
{value:1,children:[{value:2,children:[]}]}::tree
```
A declaration may not refer to a name that is declared only after a
subsequent value.  Declarations at the end of the input that are not followed
by a value have no effect.

When an implementation formats values as SUP, it emits the declaration of
each named type in a value's type (including the types of its type values)
before the first value in which the named type appears.

## 3. Examples

The simplest SUP value is a single value, perhaps a string like this:
```
"hello, world"
```
There's no need for a type declaration here.  It's explicitly a string.

A relational table might look like this:
```
type city_schema={city:string,state:string,population:uint32}
{city:"Berkeley",state:"CA",population:121643}::city_schema
{city:"Broad Cove",state:"ME",population:806}::city_schema
{city:"Sioux Falls",state:"SD",population:192517}::city_schema
```
The text here depicts three record values.  It declares a type called `city_schema`
and decorates each record value with the type.
When all the values in a sequence have the same record type, the sequence
can be interpreted as a _table_, where record values form the _rows_
and the fields of the records form the _columns_.  In this way, these
three records form a relational table conforming to the schema `city_schema`.

In contrast, text representing a semi-structured sequence of log lines
might look like this:
```
type socket={addr:ip,port:uint16}
type conn={info:string,src:socket,dst:socket}
type access_list={info:string,nets:[net]}
{
    info: "Connection Example",
    src: { addr: 10.1.1.2, port: 80 },
    dst: { addr: 10.0.1.2, port: 20130 }
}::conn
{
    info: "Connection Example 2",
    src: { addr: 10.1.1.8, port: 80 },
    dst: { addr: 10.1.2.88, port: 19801 }
}::conn
{
    info: "Access List Example",
    nets: [ 10.1.1.0/24, 10.1.2.0/24 ]
}::access_list
{ metric: "A", ts: 2020-11-24T08:44:09.586441-08:00, value: 120 }
{ metric: "B", ts: 2020-11-24T08:44:20.726057-08:00, value: 0.86 }
{ metric: "A", ts: 2020-11-24T08:44:32.201458-08:00, value: 126 }
{ metric: "C", ts: 2020-11-24T08:44:43.547506-08:00, value: { x:10, y:101 } }
```
In this case, the type declarations define a record type called `socket`,
which is embedded in a second record type called `conn`:
* `socket` is a record with typed fields `addr` and `port` where `port` is an unsigned 16-bit integer, and
* `conn` is a record with typed fields `info`, `src`, and `dst`.

The decorator of each `conn` value establishes the type of its fields, so
the `port` values need no decorators of their own.

The `access_list` type is a record whose
`nets` field is an array of networks and illustrates the helpful range of
primitive types.

Finally, there are four more values that show SUP's efficacy for
representing metrics.  Here, there are no type decorators as all of the field
types are implied by their syntax, and hence, the top-level record type is implied.
For instance, the `ts` field is an RFC 3339 date and time string,
unambiguously the primitive type `timestamp`.  Further,
note that the `value` field takes on different types and even a complex record
type on the last line.  In this case, there is a different top-level
record type implied by each of the three variations of type of the `value` field.

## 4. Grammar

Here is a left-recursive pseudo-grammar of SUP.  Note that not all
acceptable inputs are semantically valid as type mismatches may arise.
For example, union and enum values must both appear in a context
that defines their type.

```
<sup> = <sup> <item> | <item>

<item> = <value> | <type-decl>

<type-decl> = "type" <name> "=" <type>

<value> = <any> | <any> <decorators>

<decorators> = "::" <type> | <decorators> "::" <type>

<any> = <primitive> | <type-value> | <record> | <array> | <set> | <map> |
        <error-value> | <fusion-value> | <some-value>

<primitive> = primitive value as defined above

<record> = "{" <flist> "}"  |  "{"  "}"

<flist> = <flist> "," <field> | <field>

<field> = <name> ":" <value> | <name> "?:" <value>

<name> = <identifier> | <quoted-string>

<quoted-string> = quoted string as defined above

<identifier> = as defined above

<array> = "[" <vlist> "]"  |  "["  "]"

<vlist> = <vlist> "," <value> | <value>

<set> = "set[" <vlist> "]"  |  "set["  "]"

<map> = "map{" <mlist> "}"  |  "map{"  "}"

<mlist> = <mvalue> | <mlist> "," <mvalue>

<mvalue> = <value> ":" <value>

<type-value> = "<" <type> ">"

<error-value> = "error(" <value> ")"

<fusion-value> = "fusion(" <value> "," <type-value> ")"

<some-value> = "some(" <value> ")"

<type> = <primitive-type> | <record-type> | <array-type> | <set-type> |
         <map-type> | <union-type> | <enum-type> | <error-type> |
         <fusion-type> | <option-type> | <decimal-type> | <name> |
         "(" <type> ")"

<primitive-type> = uint8 | uint16 | etc. as defined above

<record-type> = "{" <tflist> "}"  |  "{" "}"

<tflist> = <tflist> "," <tfield> | <tfield>

<tfield> = <name> ":" <type> | <name> "?:" <type>

<array-type> = "[" <type> "]"

<set-type> = "set[" <type> "]"

<map-type> = "map{" <type> ":" <type> "}"

<union-type> = <type> "|" <tlist>

<tlist> = <tlist> "|" <type> | <type>

<enum-type> = "enum(" <nlist> ")"

<nlist> = <nlist> "," <name> | <name>

<error-type> = "error(" <type> ")"

<fusion-type> = "fusion(" <type> ")"

<option-type> = "option(" <type> ")"

<decimal-type> = "decimal(" <integer> ")" | "decimal(" <integer> "," <integer> ")"
```
