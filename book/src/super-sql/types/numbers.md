# Numbers

Numbers in SuperSQL follow the customary semantics and syntax
of SQL and other programming languages and include:
* [signed integers](#signed-integers),
* [unsigned integers](#unsigned-integers),
* [floating point](#floating-point), and
* [decimal](#decimal).

## Signed Integers

A 64-bit signed integer literal of type `int64` is formed from
an optional minus character (`-`) followed by a sequence of one or more decimal digits
whose value is between `-2^63` and `2^63 - 1` inclusively.

Values of signed integer of other widths can be created when reading external data
that corresponds to such types or by casting numbers to the desired types.
These signed types include:
* `int8`,
* `int16`, and
* `int32`.

>[!NOTE]
> The `int128` type is not yet implemented in SuperDB.

For backward compatibility with SQL, syntactic aliases for signed integers
are defined as follows:
* `BIGINT` maps to `int64`
* `INT` maps to `int32`
* `INTEGER` maps to `int32`
* `SMALLINT` maps to `int16`

## Unsigned Integers

A sequence of one or more decimal digits that has a value greater than
`2^63 - 1` and less than `2^64` exclusively forms an unsigned 64-bit integer literal.

Values of unsigned integer of other widths can be created when reading external data
that corresponds to such types or by casting numbers to the desired types.
These unsigned types include:
* `uint8`,
* `uint16`, and
* `uint32`.

>[!NOTE]
> The `uint128` type is not yet implemented in SuperDB.

## Floating Point

A sequence of one or more decimal digits followed by a decimal point (`.`)
followed optionally by one or more decimal digits forms
a 64-bit IEEE floating point value of type `float64`.
Alternatively, a floating point value may appear in scientific notation
having the form of a mantissa number (integer or with decimal point)
followed by the character `e` or `E` and in turn followed by a signed integer exponent.

Also `Inf`, `+Inf`, `-Inf`, or `NaN` are valid 64-bit floating point numbers.

Floating-point values with widths other than `float64`
can be created when reading external data
that corresponds to such other types or by casting numbers to the desired
floating point type `float32` or `float16`.

For backward compatibility with SQL, syntactic aliases for signed integers
are defined as follows:
* `REAL` maps to `float32`
* `FLOAT` maps to `float64`
* `DOUBLE PRECISION` maps to `float64`

>[!NOTE]
> The `FLOAT(n)` SQL types are not yet implemented by SuperSQL.

## Decimal

>[!NOTE]
> The `decimal` type is not yet implemented in SuperSQL.

## Coercion

Mixed-type numeric values used in expressions are promoted via an implicit
cast to the type that is best compatible with an operation or expected input type.
This process is called _coercion_.

For example, in the expression
```
1::int8 + 1::int16
```
the `1::int8` value is cast to `1::int16` and the result is `2::int16`.

Similarly, in
```
values 1::int8, 1::int16 | aggregate sum(this)
```
the input values to `sum()` are coerced to `int64` and the result is
`2::int64`.

>[!NOTE]
> Further details of coercion rules are forthcoming in a future
> version of this documentation.

## Examples

---

_Signed integers_

```mdtest-spq
# spq
values 1, 0, -1, 9223372036854775807
| values f"{this} is type {typeof(this)}"
# input

# expected output
"1 is type <int64>"
"0 is type <int64>"
"-1 is type <int64>"
"9223372036854775807 is type <int64>"
```

---

_Other signed integer types_

```mdtest-spq {data-layout="stacked"}
# spq
values 1, 200, 70000, 9223372036854775807
| values this::int8, this::int16, this::int32, this::int64
# input

# expected output
1::int8
1::int16
1::int32
1
error({message:"cannot cast to int8",on:200})
200::int16
200::int32
200
error({message:"cannot cast to int8",on:70000})
error({message:"cannot cast to int16",on:70000})
70000::int32
70000
error({message:"cannot cast to int8",on:9223372036854775807})
error({message:"cannot cast to int16",on:9223372036854775807})
error({message:"cannot cast to int32",on:9223372036854775807})
9223372036854775807
```

---

_Unsigned integers_

```mdtest-spq {data-layout="stacked"}
# spq
values 1, 200, 70000, 9223372036854775807
| values this::uint8, this::uint16, this::uint32, this::uint64
| values f"{this} is type {typeof(this)}"
# input

# expected output
"1 is type <uint8>"
"1 is type <uint16>"
"1 is type <uint32>"
"1 is type <uint64>"
"200 is type <uint8>"
"200 is type <uint16>"
"200 is type <uint32>"
"200 is type <uint64>"
error({message:"cannot cast to uint8",on:70000})
error({message:"cannot cast to uint16",on:70000})
"70000 is type <uint32>"
"70000 is type <uint64>"
error({message:"cannot cast to uint8",on:9223372036854775807})
error({message:"cannot cast to uint16",on:9223372036854775807})
error({message:"cannot cast to uint32",on:9223372036854775807})
"9223372036854775807 is type <uint64>"
```

---

_Floating-point numbers_

```mdtest-spq
# spq
values 1., 1.23, 18446744073709551615., 1.e100, 1e6, +Inf, -Inf, NaN
| values f"{this} is type {typeof(this)}"
# input

# expected output
"1 is type <float64>"
"1.23 is type <float64>"
"1.8446744073709552e+19 is type <float64>"
"1e+100 is type <float64>"
"1e+06 is type <float64>"
"+Inf is type <float64>"
"-Inf is type <float64>"
"NaN is type <float64>"
```
---

_Other floating-point types_

```mdtest-spq {data-layout="stacked"}
# spq
values 1., 1.23, 18446744073709551615., 1.e100, +Inf, -Inf, NaN
| values this::float16, this::float32, this::float64
| values f"{this} is type {typeof(this)}"
# input

# expected output
"1 is type <float16>"
"1 is type <float32>"
"1 is type <float64>"
"1.2304688 is type <float16>"
"1.23 is type <float32>"
"1.23 is type <float64>"
"+Inf is type <float16>"
"1.8446744e+19 is type <float32>"
"1.8446744073709552e+19 is type <float64>"
"+Inf is type <float16>"
"+Inf is type <float32>"
"1e+100 is type <float64>"
"+Inf is type <float16>"
"+Inf is type <float32>"
"+Inf is type <float64>"
"-Inf is type <float16>"
"-Inf is type <float32>"
"-Inf is type <float64>"
"NaN is type <float16>"
"NaN is type <float32>"
"NaN is type <float64>"
```
