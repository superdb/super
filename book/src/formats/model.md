# Super-structured Data Model

Super-structured data is a collection of one or more typed data values.
Each value's type is either a "primitive type", a "complex type",
or a "named type".

## 1. Primitive Types

Primitive types include signed and unsigned integers, IEEE binary
floating point, durations and timestamps, string, byte sequence, Boolean,
IP address, IP network, UUID, the first-class type _type_, and a few
special types (`null`, `none`, `all`, and `any`)
described below.

The primitive types are defined as follows:

| Name               | Definition                                      |
|--------------------|-------------------------------------------------|
| `uint8`            | unsigned 8-bit integer  |
| `uint16`           | unsigned 16-bit integer |
| `uint32`           | unsigned 32-bit integer |
| `uint64`           | unsigned 64-bit integer |
| `uint128`          | unsigned 128-bit integer (reserved) |
| `int8`             | signed 8-bit integer    |
| `int16`            | signed 16-bit integer   |
| `int32`            | signed 32-bit integer   |
| `int64`            | signed 64-bit integer   |
| `int128`           | signed 128-bit integer (reserved) |
| `duration`         | signed 64-bit integer as nanoseconds |
| `timestamp(0)`     | timestamp with 0 fractional digits (reserved) |
| `timestamp(3)`     | timestamp with 3 fractional digits (reserved) |
| `timestamp(6)`     | timestamp with 6 fractional digits (reserved) |
| `timestamp`        | signed 64-bit integer as nanoseconds from the Unix epoch (equivalently `timestamp(9)`) |
| `timestamp_ntz(0)` | timestamp without time zone with 0 fractional digits (reserved) |
| `timestamp_ntz(3)` | timestamp without time zone with 3 fractional digits (reserved) |
| `timestamp_ntz(6)` | timestamp without time zone with 6 fractional digits (reserved) |
| `timestamp_ntz`    | timestamp without time zone with 9 fractional digits (reserved) |
| `date`             | calendar date (reserved) |
| `time(0)`          | time of day with 0 fractional digits (reserved) |
| `time(3)`          | time of day with 3 fractional digits (reserved) |
| `time(6)`          | time of day with 6 fractional digits (reserved) |
| `time`             | time of day with 9 fractional digits (reserved) |
| `float16`          | IEEE-754 binary16 |
| `float32`          | IEEE-754 binary32 |
| `float64`          | IEEE-754 binary64 |
| `bool`             | the Boolean value `true` or `false` |
| `bytes`            | a bounded sequence of 8-bit bytes |
| `string`           | a UTF-8 string |
| `ip`               | an IPv4 or IPv6 address |
| `net`              | an IPv4 or IPv6 address and net mask |
| `interval`         | a calendar interval (reserved) |
| `uuid`             | a 128-bit universally unique identifier |
| `geometry`         | a geometric shape (reserved) |
| `geography`        | a geographic shape (reserved) |
| `type`             | a type value |
| `null`             | the type of the `null` value |
| `none`             | the type of the `none` value |
| `all`              | the type of a value of any type held by a [fusion](#28-fusion) |
| `any`              | the top type, to which every type conforms |

A type marked "reserved" is part of the type system, so it may appear in
a type, but its values are not yet defined.  The time zone of a
`timestamp_ntz`, `date`, or `time` value is unspecified.

The _type_ type provides for first-class types. Even though a type value can
represent a complex type, the value itself is a singleton.

Since every type in the type system is uniquely defined, two type values
are equal if and only if the types they represent are equal.

### 1.1 Null and None

The _null_ type is a primitive type whose only value is `null`.
Since `null` has its own type, a value that is either a value of some
type `T` or null is a [union](#25-union) of `T` and `null`,
e.g., a nullable string has type `string|null`.

The _none_ type is a primitive type whose only value is `none`, which
represents the absence of a value.  A record field whose value is `none`
is absent from the record, e.g., in a record type formed by
[type fusion](../super-sql/type-fusion.md), a field that is absent from
the original record has a union type with a `none` member.
When cast to an [option](#29-option) type, `none` yields that option's
"none" value.  It is also the element type of an empty array
or set and the key and value type of an empty map whose types are not
otherwise known.

### 1.2 All and Any

The _any_ type is the top type of the type system: every type conforms
to `any`, but no value has type `any`.  A value cast to `any` has type
[`fusion(all)`](#28-fusion), which holds a value of any type along with its
type.

The _all_ type is the type of the value held by a `fusion(all)` value.
It appears only within the type `fusion(all)`.

## 2. Complex Types

Complex types are composed of primitive types and/or other complex types.
The categories of complex types include:
* _record_ - an ordered collection of zero or more named values called fields,
* _array_ - an ordered sequence of zero or more values called elements,
* _set_ - an unordered collection of zero or more unique values called elements,
* _map_ - a collection of zero or more key/value pairs where the keys are of a
uniform type called the key type and the values are of a uniform type called
the value type,
* _union_ - a type representing values whose type is any of a specified collection of two or more unique types,
* _enum_ - a type representing a finite set of symbols typically representing categories,
* _error_ - any value wrapped as an "error",
* _fusion_ - a value of a supertype together with the type of the value it represents,
* _option_ - a value that is either present or absent, and
* _decimal_ - a fixed-point decimal number of a given precision and scale.

The type system comprises a total order:
* All [named types](#3-named-type) are ordered before any other types.
* All primitive types are ordered before any complex types.
* The order of primitive types corresponds to the order in the table above.
* The order of complex type categories corresponds to the order above.
* For complex types of the same category, the order is defined below.

### 2.1 Record

A record comprises an ordered set of zero or more named values
called "fields".  The field names must be unique in a given record
and the order of the fields is significant, e.g., type `{a:string,b:string}`
is distinct from type `{b:string,a:string}`.

A field name is any UTF-8 string.

A field value is a value of any type.

A field that may be absent from a record value is an _optional field_,
whose type is an [option](#29-option) type.  When the field's value is
the option's none value, the field is logically absent but still present
in the value's type.

A record type is uniquely defined by its ordered list of field-type pairs.

The type order of two records is as follows:
* A record type with fewer fields than the other is ordered before the other.
* Record types with the same number of fields are ordered according to
     * the lexicographic order of the field names from left to right,
     * or if all the field names are the same, the type order of the field types from left to right.

### 2.2 Array

An array is an ordered sequence of zero or more values called "elements"
all conforming to the same type.

An array value may be empty.  An empty array whose type is not otherwise
known has element type `none`.

An array type is uniquely defined by its single element type.

The type order of two arrays is defined as the type order of the
two array element types.

An array of mixed-type values (such as a mixed-type JSON array) is representable
as an array with elements of type `union`.

### 2.3 Set

A set is an unordered collection of zero or more unique values called "elements"
all conforming to the same type.

A set may be empty.  An empty set whose type is not otherwise known has
element type `none`.

A set of mixed-type values is representable as a set with
elements of type `union`.

A set type is uniquely defined by its single element type.

The type order of two sets is defined as the type order of the
two set element types.

### 2.4 Map

A map represents a collection of zero or more key-value pairs, where the keys
have a common type and the values have a common type.

The keys of a map value must be unique.

A map value may be empty.  An empty map whose type is not otherwise known
has key type and value type `none`.

A map type is uniquely defined by its key type and value type.

The type order of two map types is as follows:
* the type order of their key types,
* or if they are the same, then the order of their value types.

### 2.5 Union

A union represents a value that may be any one of a specific set
of two or more unique types that comprise its "union type".

A union type is uniquely defined by an ordered set of two or more unique types
where the order corresponds to the type system's total order.
A union's types may not themselves be union types, though they may be
named types whose underlying types are unions.

Union values are tagged in that
each union value explicitly conforms to exactly one of the union's types.
The union tag is an integer indicating the position of its type in the union
type's ordered list of types.

The type order of two union types is as follows:
* The union type with fewer types than the other is ordered before the other.
* Two union types with the same number of types are ordered according to
the type order of the constituent types in left to right order.

### 2.6 Enum

An enum represents a symbol from a finite set of one or more unique symbols
referenced by name.  An enum symbol may be any UTF-8 string.

An enum type is uniquely defined by its ordered set of unique symbols,
where the order is significant, e.g., two enum types
with the same set of symbols but in different order are distinct.

The type order of two enum types is as follows:
* The enum type with fewer symbols than the other is ordered before the other.
* Two enum types with the same number of symbols are ordered according to
the lexicographic order of their symbols from left to right.

The order among enum values corresponds to the order of the symbols in the enum type.
Order among enum values from different types is undefined.

### 2.7 Error

An error represents any value designated as an error.

An error type is uniquely defined by the type of its value.

The type order of two error types is the type order of the types of
their values.

### 2.8 Fusion

A fusion value is a value of a type called the fusion's _supertype_ together
with the type of the value it represents, called its _subtype_.  The
represented value is recovered by converting the value to its subtype.

Fusion types are created by [type fusion](../super-sql/type-fusion.md),
which allows values of many different types to be stored as values of
a single fused type without losing their original types.
For example, the values `{a:1}` and `{b:"x"}` may be stored as
fusion values of type `fusion({a:int64|none,b:string|none})`
whose subtypes are `{a:int64}` and `{b:string}`.

The type `fusion(all)` holds a value of any type, whose subtype is
the value's type.

A fusion type is uniquely defined by its supertype.

The type order of two fusion types is the type order of their supertypes.

### 2.9 Option

An option represents a value of a given type that is either present
(called "some") or absent (called "none").  The type of an option may
not be an option type or `none`.

An option type is uniquely defined by the type of its value.

An [optional record field](#21-record) is a field whose type is an
option type.

The type order of two option types is the type order of the types of
their values.

### 2.10 Decimal

A decimal type is a fixed-point decimal number with a _precision_
(the total number of decimal digits, from 1 to 38) and
a _scale_ (the number of those digits to the right of the decimal point,
which is no greater than the precision).

A decimal type is uniquely defined by its precision and scale.
Decimal values are reserved and not yet defined.

The type order of two decimal types is as follows:
* the order of their precisions,
* or if they are the same, then the order of their scales.

## 3. Named Type

A _named type_ is a name for a specific data type.
Any value can have a named type and the named type is a distinct type
from its underlying type.  A named type can refer to another named type
and, since a named type may refer to itself, recursive types may be defined.

The binding between a named type and its underlying type is unique
within a sequence of values: a type name may be bound to only one type.

A type name may be any UTF-8 string other than the name of a primitive type.

For example, if "port" is a named type for `uint16`, then two values of
type "port" have the same type but a value of type "port" and a value of type `uint16`
do not have the same type.

Named types are ordered before all other types and are
ordered lexicographically by name.
