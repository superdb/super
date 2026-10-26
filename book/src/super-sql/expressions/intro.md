# Expressions

Expressions are the means to carry out calculations and utilize familiar
query-language elements like literal values, function calls, subqueries,
and so forth.

Within [pipe operators](../operators/intro.md),
expressions may reference [input values](inputs.md) either via the [special value](../intro.md#pipe-scoping)
`this` or implied field references to `this`, while
within [SQL clauses](../sql/intro.md), input is referenced with table and
column references.

For example, [values](../operators/values.md), [where](../operators/where.md),
[cut](../operators/cut.md), [put](../operators/put.md),
[sort](../operators/sort.md) and so forth all utilize various expressions
as part of their semantics.

Likewise, the projected columns of a SQL
[SELECT](../sql/select.md) utilize the very same expression syntax
used by pipe operators.

While SQL expressions and pipe expressions share an identical syntax,
their semantics diverge in some key ways:
* SQL expressions that reference `this` have [semantics](../sql/intro.md#this)
  that depend on the SQL clause that expression appears in,
* relational tables and/or columns cannot be referenced using aliases in pipe scoping,
* double-quoted string [literals](literals.md) may be used in pipe expressions but are interpreted
  as [identifiers](../queries.md#identifiers) in SQL expressions.

## Expression Syntax

Expressions are composed from operands and operators over operands.

Operands include
  * [inputs](inputs.md),
  * [literals](literals.md),
  * [formatted strings](f-strings.md),
  * [function calls](functions.md),
  * [subqueries](subqueries.md), or
  * other expressions.

Operators include
  * [arithmetic](./arithmetic.md) to add, subtract, multiply, divide, etc.,
  * [cast](cast.md) to convert values from one type to another,
  * [comparisons](comparisons.md) to compare two values resulting in a [Boolean](../types/bool.md),
  * [concatenation](concat.md) of strings,
  * [conditionals](conditional.md) including C-style `?:` operator and SQL `CASE` expressions,
  * [containment](containment.md) to test for the existing value inside an [array](../types/array.md) or [set](../types/set.md),
  * [dot](dot.md) to access a field of a [record](../types/record.md) (or a SQL column of a table),
  * [exists](exists.md) for SQL compatibility to test for non-empty subqueries,
  * [indexing](index.md) to select and slice elements from
      an array, set, record, [map](../types/map.md), string, or [bytes](../types/bytes.md),
  * [logic](logic.md) to combine predicates using Boolean logic, and
  * [slices](slices.md) to extract subsequences from arrays, sets, strings, and bytes.

## Identifier Resolution

An identifier that appears as an operand in an expression is resolved to
the entity that it represents using lexical [scoping](../queries.md#scope).

For identifiers that appear in the context of call syntax, i.e., having the form
```
<func> ( <args> )
```
then `<func>` is one of:
* the name of a [built-in function](../functions/intro.md),
* the name of a [declared function](../declarations/functions.md),
* a [lambda expression](functions.md#lambda-expressions), or
* a function parameter that resolves to a
[function reference](functions.md#function-references) or
[lambda expression](functions.md#lambda-expressions).

Identifiers that correspond to an in-scope function may also be referenced
with the syntax
```
& <name>
```
as a [function reference](functions.md#function-references)
and must appear as an argument to an operator or function; otherwise, such
expressions are errors.

For identifiers that resolve to in-scope declarations, the resolution is
as follows:
* [constants](../declarations/constants.md) resolve to their defined constant values,
* [types](../declarations/types.md) resolve to their named type,
* [queries](../declarations/queries.md) resolve to an implied subquery invocation, and
* [functions](../declarations/functions.md) and [operators](../declarations/operators.md)
  produce an error.

For other instances of identifiers,
the identifier is presumed to be an [input reference](inputs.md)
and is resolved as such.

## Precedence

When multiple operators appear in an unparenthesized sequence,
ambiguity may arise by the order of evaluation as expressions are
not always evaluated in a strict left-to-right order.  Precedence rules
determine the operator order when such ambiguity exists where higher
precedence operators are evaluated before lower precedence operators.

For example,
```
1 + 2 * 3
```
is `7` not `9` because multiplication has higher precedence than addition
and the above expression is equivalent to
```
1 + ( 2 * 3 )
```
Operators have the following precedence from highest to lowest:

* `[]` [indexing](index.md)
* `-`, `+` [unary sign](arithmetic.md#unary-sign)
* `||` [string concatenation](concat.md)
* `*`, `/`, `%` [multiplication, division, modulo](arithmetic.md)
* `-`, `+` [subtraction, addition](arithmetic.md)
* `=`, `>=`, `>`, `<=`, `<`, `<>`, `!=`, `is` [comparisons](comparisons.md)
* `not`, `!` [logical NOT](logic.md), `exists` [existence](exists.md)
* `like`, `in`, `between` [comparisons](comparisons.md)
* `and` [logical AND](logic.md)
* `or` [logical OR](logic.md)
* `??` none coalescing, which replaces a none or null with its right-hand side (see [ok](../functions/generics/ok.md))
* `?:` [ternary conditional](conditional.md#ternary-conditional)

Some operators like [case expressions](conditional.md#case-expressions)
do not have any such ambiguity as keywords delineate their sub-expressions
and thus do not have any inherent precedence.

## Coercion

>[!NOTE]
> A forthcoming version of this documentation will describe the coercion
rules for automatically casting of values for type compatibility in expressions.
