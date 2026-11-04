# Match

A match expression is a multi-way [type assertion](type-assertions.md)
that selects a result according to the type of a value and has the form
```
match [<name>:=]<expr> [ with <assignments> ]
when <type-1> [then] <result-1>
[ when <type-2> [then] <result-2> ]
...
[ else <else-result> ]
[ end ]
```
where `<expr>` is any [expression](intro.md) and each `<type-n>` is any
[type](../types/intro.md).  The value of `<expr>` is compared with each type
in turn and the result of the first matching `when` becomes the value of the
match expression.  A value matches a type by the same rules as a
[type assertion](type-assertions.md), so a value of a
[union](../types/union.md), option, or fusion type matches the type of
its underlying value and [named types](../types/named.md) are distinct.

If no case matches, the result is `<else-result>` or, without an
`else` clause, `none`, which disappears from a record, array, or map,
is dropped from the output of the [values](../operators/values.md) operator,
and is typically handled with the `??` operator.
An [error](../types/error.md) or `missing` value is passed through
unless a case has its type.

A result is evaluated with `this` bound to the value of `<expr>` asserted to
the type of its case, i.e., `<expr>.(<type>)`, so `this` in
`match x when int64 this+1` is an `int64`.  The else result is evaluated
with `this` bound to the value of `<expr>`.  To keep the rest of a record,
use a [spread](../types/record.md), e.g., `{...this,x:match x when int64 this+1 else 0}`.

To refer to other values in a result, give the match a name with `:=` or
list the values in a `with` clause.  Then `this` in a result is a record
whose first field is the (asserted) value of `<expr>` and whose other fields
are the `with` values, where each field's name is given with `:=` or, as for
[put](../operators/put.md), derived from its expression (e.g., `upper(s)` is
named `upper` and `this` is named `that`).  For example,
```
match s:=upper(s) with that:=this when string s||that.suffix
```
evaluates its result on `{s:upper(s).(string),that:this}`.

The `then` and `end` keywords are optional.  Without `end`, a nested match
extends as far as possible, i.e., any `when` or `else` that follows belongs to
the innermost match, so a nested match should be parenthesized or ended with
`end`.

## Examples

---

_Dispatch on the type of a value_

```mdtest-spq
# spq
values match this
  when int64 this+1
  when string upper(this)
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
    when Push "push to "||ref
    when Watch "watch "||action
# input
{ref:"main"}
{action:"started"}
# expected output
"push to main"
"watch started"
```

---

_Refer to other values with a with clause_

```mdtest-spq
# spq
values match x with y when int64 x+y when string x else y
# input
{x:1,y:10}
{x:"a",y:20}
{x:true,y:30}
# expected output
11
"a"
30
```

---

_Without an else, an unmatched value is none_

```mdtest-spq
# spq
values {n:match x when int64 this*2}
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
values {
  passed: match this when int64 this else 0,
  caught: match this when int64 this when error(string) "caught" else 0
}
# input
1
error("oops")
# expected output
{passed:1,caught:1}
{passed:error("oops"),caught:"caught"}
```
