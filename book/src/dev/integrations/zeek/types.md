# Type System

As the [super-structured data model](../../../formats/model.md) was in many ways inspired by the
[Zeek TSV log format](https://docs.zeek.org/en/current/tutorial/logs.html#zeek-log-formats),
SuperDB's rich storage formats ([SUP](../../../formats/sup.md),
[BSUP](../../../formats/bsup.md), etc.) maintain comprehensive interoperability
with Zeek.

When Zeek is configured to output its logs in
JSON format, much of the rich type information is lost in translation, but
this can be restored by following the guidance for [shaping Zeek JSON](shaping.md).
On the other hand, Zeek TSV can be converted to Zed storage formats and back to
Zeek TSV without any loss of information.

This document describes how the super-structured type system is able to represent each of
the types that may appear in Zeek logs.

SuperDB maintains an internal super-structured
representation of any Zeek data that is read or imported. Therefore, knowing
the equivalent types will prove useful when performing operations in the
[SuperSQL](../../../super-sql/intro.md) such as
[type casting](../../../super-sql/expressions/cast.md) or looking at the data
when output as SUP.

## Equivalent Types

The following table summarizes which Zed data type corresponds to each
[Zeek data type](https://docs.zeek.org/en/current/reference/zeekscript/types.html)
that may appear in a Zeek TSV log. While most types have a simple 1-to-1
mapping from Zeek to Zed and back to Zeek again, the sections linked from the
**Additional Detail** column describe cosmetic differences and other subtleties
applicable to handling certain types.

| Zeek Type  | Zed Type   | Additional Detail |
|------------|------------|-------------------|
| [`bool`](https://docs.zeek.org/en/current/reference/zeekscript/types.html#type-bool)         | [`bool`](../../../formats/model.md#1-primitive-types)     | |
| [`count`](https://docs.zeek.org/en/current/reference/zeekscript/types.html#type-count)       | [`uint64`](../../../formats/model.md#1-primitive-types)   | |
| [`int`](https://docs.zeek.org/en/current/reference/zeekscript/types.html#type-int)           | [`int64`](../../../formats/model.md#1-primitive-types)    | |
| [`double`](https://docs.zeek.org/en/current/reference/zeekscript/types.html#type-double)     | [`float64`](../../../formats/model.md#1-primitive-types)  | See [`double` details](#double) |
| [`time`](https://docs.zeek.org/en/current/reference/zeekscript/types.html#type-time)         | [`time`](../../../formats/model.md#1-primitive-types)     | |
| [`interval`](https://docs.zeek.org/en/current/reference/zeekscript/types.html#type-interval) | [`duration`](../../../formats/model.md#1-primitive-types) | |
| [`string`](https://docs.zeek.org/en/current/reference/zeekscript/types.html#type-string)     | [`string`](../../../formats/model.md#1-primitive-types)   | See [`string` details about escaping](#string) |
| [`port`](https://docs.zeek.org/en/current/reference/zeekscript/types.html#type-port)         | [`uint16`](../../../formats/model.md#1-primitive-types)   | See [`port` details](#port) |
| [`addr`](https://docs.zeek.org/en/current/reference/zeekscript/types.html#type-addr)         | [`ip`](../../../formats/model.md#1-primitive-types)       | |
| [`subnet`](https://docs.zeek.org/en/current/reference/zeekscript/types.html#type-subnet)     | [`net`](../../../formats/model.md#1-primitive-types)      | |
| [`enum`](https://docs.zeek.org/en/current/reference/zeekscript/types.html#type-enum)         | [`string`](../../../formats/model.md#1-primitive-types)   | See [`enum` details](#enum) |
| [`set`](https://docs.zeek.org/en/current/reference/zeekscript/types.html#type-set)           | [`set`](../../../formats/model.md#23-set)                 | See [`set` details](#set) |
| [`vector`](https://docs.zeek.org/en/current/reference/zeekscript/types.html#type-vector)     | [`array`](../../../formats/model.md#22-array)              | |
| [`record`](https://docs.zeek.org/en/current/reference/zeekscript/types.html#type-record)     | [`record`](../../../formats/model.md#21-record)            | See [`record` details](#record) |

>[!NOTE]
> The [Zeek data types](https://docs.zeek.org/en/current/reference/zeekscript/types.html)
> page describes the types in the context of the
> [Zeek scripting language](https://docs.zeek.org/en/current/tutorial/scripting/index.html).
> The Zeek types available in scripting are a superset of the data types that
> may appear in Zeek log files. The encodings of the types also differ in some
> ways between the two contexts. However, we link to this reference because
> there is no authoritative specification of the Zeek TSV log format.

## Example

The following example shows a TSV log that includes each Zeek data type, how
it's output as SUP by [`super`](../../../command/super.md), and then how it's written back out again as a Zeek
log. You may find it helpful to refer to this example when reading the
[type-specific details](#type-specific-details).

### Viewing the TSV log:

```
cat zeek_types.log
```

### Output:

```mdtest-input zeek_types.log
#separator \x09
#set_separator	,
#empty_field	(empty)
#unset_field	-
#fields	my_bool	my_count	my_int	my_double	my_time	my_interval	my_printable_string	my_bytes_string	my_port	my_addr	my_subnet	my_enum	my_set	my_vector	my_record.name	my_record.age
#types	bool	count	int	double	time	interval	string	string	port	addr	subnet	enum	set[string]	vector[string]	string	count
T	123	456	123.4560	1592502151.123456	123.456	smile😁smile	\x09\x07\x04	80	127.0.0.1	10.0.0.0/8	tcp	things,in,a,set	order,is,important	Jeanne	122
```

### Reading the TSV log, outputting as SUP, and saving a copy:

```mdtest-command
super -S -i zeek zeek_types.log | tee zeek_types.sup
```

### Output:

```mdtest-output
type port = uint16
type zenum = string
{
  my_bool: true::(bool|null),
  my_count: 123::uint64::(uint64|null),
  my_int: 456::(int64|null),
  my_double: 123.456::(float64|null),
  my_time: 2020-06-18T17:42:31.123456Z::(timestamp|null),
  my_interval: 2m3.456s::(duration|null),
  my_printable_string: "smile😁smile"::(string|null),
  my_bytes_string: "\t\u0007\u0004"::(string|null),
  my_port: 80::port::(port|null),
  my_addr: 127.0.0.1::(ip|null),
  my_subnet: 10.0.0.0/8::(net|null),
  my_enum: "tcp"::zenum::(zenum|null),
  my_set: set[
    "a",
    "in",
    "set",
    "things"
  ]::set[string|null]::(null|set[string|null]),
  my_vector: [
    "order",
    "is",
    "important"
  ]::[string|null]::(null|[string|null]),
  my_record: {
    name: "Jeanne"::(string|null),
    age: 122::uint64::(uint64|null)
  }
}
```

### Reading the saved SUP output and outputting as Zeek TSV:

```mdtest-command
super -f zeek zeek_types.sup
```

### Output:
```mdtest-output
#separator \x09
#set_separator	,
#empty_field	(empty)
#unset_field	-
#fields	my_bool	my_count	my_int	my_double	my_time	my_interval	my_printable_string	my_bytes_string	my_port	my_addr	my_subnet	my_enum	my_set	my_vector	my_record.name	my_record.age
#types	bool	count	int	double	time	interval	string	string	port	addr	subnet	enum	set[string]	vector[string]	string	count
T	123	456	123.456	1592502151.123456	123.456000	smile😁smile	\x09\x07\x04	80	127.0.0.1	10.0.0.0/8	tcp	a,in,set,things	order,is,important	Jeanne	122
```

## Type-Specific Details

As `super` acts as a reference implementation for SuperDB storage formats such as
SUP and BSUP, it's helpful to understand how it reads the following Zeek data
types into readable text equivalents in the SUP format, then writes them back
out again in the Zeek TSV log format. Other implementations of the Zed storage
formats (should they exist) may handle these differently.

Multiple Zeek types discussed below are represented via a
[type definition](../../../formats/sup.md#22-type-decorators) to one of Zed's
[primitive types](../../../formats/model.md#1-primitive-types). The Zed type
definitions maintain the history of the field's original Zeek type name
such that `super` may restore it if the field is later output in
Zeek TSV format. Knowledge of its original Zeek type may also enable special
operations in Zed that are unique to values known to have originated as a
specific Zeek type, though no such operations are currently implemented in
`super`.

### `double`

As they do not affect accuracy, "trailing zero" decimal digits on Zeek `double`
values will _not_ be preserved when they are formatted into a string, such as
via the `-f sup|zeek|table` output options in `super` (e.g., `123.4560` becomes
`123.456`).
s
### `enum`

As they're encountered in common programming languages, enum variables
typically hold one of a set of predefined values. While this is
how Zeek's `enum` type behaves inside the Zeek scripting language,
when the `enum` type is output in a Zeek log, the log does not communicate
any such set of "allowed" values as they were originally defined. Therefore,
these values are represented with a type name bound to the Zed `string`
type. See the text above regarding [type definitions](#type-specific-details)
for more details.

### `port`

The numeric values that appear in Zeek logs under this type are represented
in Zed with a type name of `port` bound to the `uint16` type. See the text
above regarding [type names](#type-specific-details) for more details.

### `set`

Because order within sets is not significant, no attempt is made to maintain
the order of `set` elements as they originally appeared in a Zeek log.

### `string`

Zeek's `string` data type is complicated by its ability to hold printable ASCII
and UTF-8 as well as arbitrary unprintable bytes represented as `\x` escapes.
Because such binary data may need to legitimately be captured (e.g. to record
the symptoms of DNS exfiltration), it's helpful that Zeek has a mechanism to
log it. Unfortunately, Zeek's use of the single `string` type for these
multiple uses leaves out important details about the intended interpretation
and presentation of the bytes that make up the value. For instance, one Zeek
`string` field may hold arbitrary network data that _coincidentally_ sometimes
form byte sequences that could be interpreted as printable UTF-8, but they are
_not_ intended to be read or presented as such. Meanwhile, another Zeek
`string` field may be populated such that it will _only_ ever contain printable
UTF-8. These details are currently only captured within the Zeek source code
itself that defines how these values are generated.

SuperSQL includes a [primitive type](../../../formats/model.md#1-primitive-types)
called `bytes` that's suited to storing the former "always binary" case and a
`string` type for the latter "always printable" case. However, Zeek logs do
not currently communicate details that would allow an implementation to know
which Zeek `string` fields to store as which of these two Zed data types.
Instead, the Zed system does what the Zeek system does when writing strings
to JSON: any `\x` escapes used in Zeek TSV strings are translated into valid
Zed UTF-8 strings by escaping the backslash before the `x`.  In this way,
you can still see binary-corrupted strings that are generated by Zeek in
the Zed data formats.

Unfortunately there is no way to distinguish whether a `\x` escape occurred
or whether that string pattern happened to occur in the original data.  A nice
solution would be to convert Zeek strings that are valid UTF-8 strings into
Zed strings and convert invalid strings into a Zed `bytes` type, or we could
convert both of them into a Zed union of `string` and `bytes`.  If you have
interest in a capability like this, please [let us know](https://www.brimdata.io/join-slack/) and we can elevate
the priority.

If Zeek were to provide an option to output logs directly in a super-structured
storage formats, this would create an opportunity to
assign the appropriate SuperDB `bytes` or `string` type at the point of origin,
depending on what's known about how the field's value is intended to be
populated and used.

### `record`

Zeek's `record` type is unique in that every Zeek log line effectively _is_ a
record, with its schema defined via the `#fields` and `#types` directives in
the headers of each log file. The word "record" never appears explicitly in
the schema definition in Zeek logs.

Embedded records also subtly appear within Zeek log lines in the form of
dot-separated field names. A common example in Zeek is the
[`id`](https://docs.zeek.org/en/current/scripts/base/init-bare.zeek.html#type-conn_id)
record, which captures the source and destination IP addresses and ports for a
network connection as fields `id.orig_h`, `id.orig_p`, `id.resp_h`, and
`id.resp_p`. When reading such fields into their Zed equivalent, `super` restores
the hierarchical nature of the record as it originally existed inside of Zeek
itself before it was output by its logging system. This enables operations in
Zed that refer to the record at a higher level but affect all values lower
down in the record hierarchy.

For instance, revisiting the data from our example, we can output all fields within
`my_record` using Zed's [cut](../../../super-sql/operators/cut.md) operator.

#### Command:

```mdtest-command
super -f zeek -c 'cut my_record' zeek_types.sup
```

#### Output:

```mdtest-output
#separator \x09
#set_separator	,
#empty_field	(empty)
#unset_field	-
#fields	my_record.name	my_record.age
#types	string	count
Jeanne	122
```
