# Comparisons

Comparison expressions follow customary syntax and semantics and
result in a truth value of type [bool](../types/bool.md) or an [error](../types/error.md).

The binary comparison operators have the form
```
<expr> <op> <expr>
```
where `<op>` is one of
* `<` for less than,
* `<=` for less than or equal,
* `>` for greater than
* `>=` for greater than or equal,
* `==` or `=` for equal,
* `!=` or `<>` for not equal, or
* `like` or `not like` for the SQL string pattern matching.

The `between` comparator has the form
```
<expr> between <lower> and <upper>
```
where `<expr>`, `<lower>`, and `<upper>` are any expressions that result in
an orderable type and are type compatible.  This is shorthand for
```
<expr> >= <lower> and <expr> <= <upper>
```
The null comparators have the form
```
<expr> is null
```
and is true if `<expr>` is a null value of any type.  Likewise,
```
<expr> is not null
```
is true if `<expr>` is not a null value of any type.
As with SQL, any comparison of a null value to any other value is a null
value of type `bool`, i.e., `null::bool`.  This is because comparing an unknown
value with any other value has an unknown result.

The `like` comparator has the form
```
<expr> like <pattern>
```
where `<expr>` is any expression that produces a [string](../types/string.md) type and `<pattern>`
is a constant expression that results in a string type.

>[!NOTE]
> Currently, `<pattern>` must be a constant value and cannot depend on the input.
> Also, the `ilike` operator for case-insensitive matching is not yet supported.
> These capabilities will be included in a future version of SuperSQL.

The `like` comparator is true if the `<pattern>` matches `<expr>` where `<pattern>`
consists of literal characters, `_` for matching any single letter, and `%` for
matching any sequence of characters.

The `not like` comparator has the form
```
<expr> not like <pattern>
```
and is true when the pattern does not match the expression.

String values are compared via byte order in accordance with
[C/POSIX collation](https://www.postgresql.org/docs/current/collation.html#COLLATION-MANAGING-STANDARD)
as found in other SQL databases such as Postgres.

>[!NOTE]
> SuperSQL does not yet support the SQL `COLLATE` keyword and variations.

If either operand to a comparison
is `error("missing")`, then the result is `error("missing")`.

## Type Checking

Comparisons are not polymorphic: the operands must have the same type or
types that are coerced to a common type as in SQL.

* Integers and floating point numbers of any width are comparable with
  each other and are [coerced](../types/numbers.md#coercion) to a common
  number type.
* [Records](../types/record.md) with the same field names in the same order,
  [arrays](../types/array.md), [sets](../types/set.md), and
  [maps](../types/map.md) are comparable when their fields or elements are,
  and are coerced likewise.
* Any value is comparable with `null`.
* An option is comparable with an option whose payload type is comparable
  with its own: their payloads are compared, and `none` equals only `none`.
  An option compared with a value that isn't an option is an error until the
  option is handled, e.g., with `??` or `unwrap`, or the value is wrapped
  with `some`.
* Values of type `bool` and `type`, and records, arrays, sets, maps, and
  enums, have no order, so `<` and `>` are `false` for them while `<=` and
  `>=` are equality.

For values of type [bool](../types/bool.md), `false` is less than `true`.

Values of type [net](../types/network.md) are ordered by address family
(IPv4 before IPv6), then by network address, and then by prefix length.

Any other comparison is a type mismatch, e.g., a number and a string, a number
and a `time` or `duration`, or an `ip` and a `net`.  The compiler reports a
type mismatch as an error when it knows the types of the operands.  Otherwise,
the result of the comparison at runtime is an error value.

When the type of an operand is a [union](../types/union.md), each of its types
must be comparable with the other operand (apart from `null` and
[errors](../types/error.md), which pass through a comparison).  Otherwise, the
union must first be narrowed, e.g., with a
[type assertion](type-assertions.md) like `x.(int64)??0`, or with a filter
like `where is(x, <int64>)`.

A union nested in the type of an operand (e.g., the element type of an array)
is different: its values are compared by value with a type that the union
holds or with a union that shares one of its types.

To compare values of any types without error, use the
[compare](../functions/generics/compare.md) function, which orders values
of different types by type.

>[!NOTE]
> The [`in`](containment.md) operator is false for values whose types
> aren't comparable.

## Examples

---

_Various scalar comparisons_

```mdtest-spq
# spq
values 1 > 2, 1 < 2, "b" > "a", 1 == 1.0, [1,2] == [1.0,2.0], false < true
# input

# expected output
false
true
true
true
true
true
```

---

_A comparison of values of different types is an error_

```mdtest-spq fails
# spq
values 1 > "a"
# input

# expected output
type mismatch: int64 > string at line 1, column 8:
values 1 > "a"
       ~~~~~~~
```

---

_A comparison with a union that has a type that isn't comparable is an error_

```mdtest-spq fails
# spq
values {x:1},{x:"foo"} | where x == 1
# input

# expected output
type mismatch: int64|string == int64 at line 1, column 32:
values {x:1},{x:"foo"} | where x == 1
                               ~~~~~~
```

---

_Narrow the union or use the compare function to compare values of any type_

```mdtest-spq
# spq
values {x:1},{x:"foo"}
| values {narrowed:(x.(int64)??0) == 1, compared:compare(x, 1) == 0}
# input

# expected output
{narrowed:true,compared:true}
{narrowed:false,compared:false}
```

---

_Null comparisons_

```mdtest-spq {data-layout="stacked"}
# spq
values {isNull:this is null,isNotNull:this is not null}
# input
1
null
2
# expected output
{isNull:false,isNotNull:true}
{isNull:true,isNotNull:false}
{isNull:false,isNotNull:true}
```

---

_Comparisons using a like pattern_

```mdtest-spq
# spq
values f"{this} like '%abc_e': {this like '%abc_e'}"
# input
"abcde"
"xabcde"
"abcdex"
"abcdd"
error("missing")
# expected output
"abcde like '%abc_e': true"
"xabcde like '%abc_e': true"
"abcdex like '%abc_e': false"
"abcdd like '%abc_e': false"
error("missing")
```
