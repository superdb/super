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

When the operands are coercible to like types, the result is the truth value
of the comparison.  Otherwise, the result is `false`.  To compare values of
different types, consider the [compare](../functions/generics/compare.md) function.

If either operand to a comparison
is `error("missing")`, then the result is `error("missing")`.

## Examples

---

_Various scalar comparisons_

```mdtest-spq
# spq
values 1 > 2, 1 < 2, "b" > "a", 1 > "a"
# input

# expected output
false
true
true
false
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
