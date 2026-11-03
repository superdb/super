# Match

A match expression selects a result according to the type of a value and
has the form
```
match <expr>
case <type-1> [then] <result-1>
[ case <type-2> [then] <result-2> ]
...
[ else <else-result> ]
[ end ]
```
where `<expr>` is any [expression](intro.md) and each `<type-n>` is any
[type](../types/intro.md).  The value of `<expr>` is compared with each type
in turn and the result of the first matching `case` becomes the value of the
match expression.  A value matches a type by the same rules as a
[type assertion](type-assertions.md), so a value of a
[union](../types/union.md), option, or fusion type matches the type of
its underlying value and [named types](../types/named.md) are distinct.

If no case matches, the result is `<else-result>` or, without an
`else` clause, `none`, which disappears from a record, array, or map
and is typically handled with the `??` operator.
An [error](../types/error.md) or `missing` value is passed through
unless a case has its type.

When `<expr>` is a field reference like `x` or `a.b` (or `this`), its value
is narrowed to the type of the matching case, i.e., a result is evaluated
with `x` replaced by `x.(<type>)`, so `x` in `case int64 x+1` is an `int64`,
including in any subquery in the result.  A reference that uses `?.`, like
`a?.b`, isn't narrowed.

The `then` and `end` keywords are optional.  Without `end`, a nested match
extends as far as possible, i.e., any `case` or `else` that follows belongs to
the innermost match, so a nested match should be parenthesized or ended with
`end`.

## Examples

---

_Dispatch on the type of a value_

```mdtest-spq
# spq
values match this
  case int64 this+1
  case string upper(this)
  else "other"
# input
1
"a"
true
# expected output
2
"A"
"other"
```

---

_Match the variants of a sum type_

```mdtest-spq
# spq
type Push = {ref:string}
type Watch = {action:string}
values cast(this, <Push|Watch>)
| values match this
    case Push "push to "||ref
    case Watch "watch "||action
# input
{ref:"main"}
{action:"started"}
# expected output
"push to main"
"watch started"
```

---

_Without an else, an unmatched value is none_

```mdtest-spq
# spq
values {n:match x case int64 x*2}
# input
{x:1}
{x:"a"}
# expected output
{n:2}
{}
```

---

_Errors pass through unless a case has an error type_

```mdtest-spq
# spq
values match this case int64 this case error(string) "caught"
# input
1
error("oops")
# expected output
1
"caught"
```
