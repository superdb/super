# Functions

New functions are declared with the syntax
```
fn <id> ( [<param> [, <param> ...]] ) : <expr>
```
where
* `<id>` is an [identifier](../queries.md#identifiers) representing the name of the function,
* each `<param>` is an identifier representing a positional argument to the function, and
* `<expr>` is any [expression](../expressions/intro.md) that implements the function.

Function declarations must appear in the declaration section of a [scope](../queries.md#scope).

The function body `<expr>` may refer to the passed-in arguments by name.

Specifically, the references to the named parameters are
field references of the [special value](../intro.md#pipe-scoping) `this`, as in any expression.
In particular, the value of `this` referenced in a function body
is formed as a [record](../types/record.md) from the actual values passed to the function
where the field names correspond to the parameters of the function.

For example, the function `add` as defined by
```
fn add(a,b): a+b
```
when invoked as
```
values {x:1} | values add(x,2)
```
is passed the record the `{a:x,b:1}`, which after resolving `x` to `1`,
is `{a:1,b:2}` and thus evaluates the expression
```
this.a + this.b
```
which results in `3`.

Any [function references](../expressions/functions.md#function-references) passed to a function do not appear in the `this`
record formed from the parameters.  Instead, function values are expanded at their
call sites in a [macro](https://en.wikipedia.org/wiki/Macro_(computer_science))-like fashion.

Functions may be recursive.  If the maximum call stack depth is exceeded,
the function returns an [error](../types/error.md) value indicating so.  Recursive functions that
run for an extended period of time without exceeding the stack depth will simply
be allowed to run indefinitely and stall the query result.

## Subquery Functions

Since the body of a function is any expression and an expression may be
a subquery, function bodies can be defined as [subqueries](../expressions/subqueries.md).
This leads to the commonly used pattern of a subquery function:
```
fn <id> ( [<param> [, <param> ...]] ) : (
    <query>
)
```
where `<query>` is any [query](../queries.md) and is simply wrapped in parentheses
to form the subquery.

As with any subquery, when multiple results are expected, an [array subquery](../expressions/subqueries.md#array-subqueries)
may be used by wrapping `<query>` in square brackets instead of parentheses:
```
fn <id> ( [<param> [, <param> ...]] ) : [
    <query>
]
```

Note when using a subquery expression in this fashion,
the function's parameters do not appear in the scope of the expressions
embedded in the query.  For example, this function results in a type error:
```mdtest-spq fails {data-layout='no-labels'} {style='margin:auto;width:85%'}
# spq
fn apply(a,val): (
  unnest a
  | collect(this+val)
)
values apply([1,2,3], 1)
# input

# expected output
no such field "val": . applied to int64 at line 3, column 18:
  | collect(this+val)
                 ~~~
```
because the field reference to `val` within the subquery does not exist.
Instead the parameter `val` can be carried into the subquery using
the compound form of [unnest](../operators/unnest.md):
```mdtest-spq {data-layout='no-labels'} {style='margin:auto;width:85%'}
# spq
fn apply(a,val): (
  unnest {outer:val,item:a}
  | collect(outer+item)
)
values apply([1,2,3], 1)
# input

# expected output
[2,3,4]
```

## Examples

---

_A simple function that adds two numbers_

```mdtest-spq
# spq
fn add(a,b): a+b
values add(x,y)
# input
{x:1,y:2}
{x:2,y:2}
{x:3,y:3}
# expected output
3
4
6
```

---

_A simple recursive function_

```mdtest-spq
# spq
fn fact(n): n<=1 ? 1 : n*fact(n-1)
values fact(5)
# input

# expected output
120
```
---
_A subquery function that computes some stats over numeric arrays_

```mdtest-spq
# spq
fn stats(numbers): (
    unnest numbers
    | sort this
    | avg(this),min(this),max(this),mode:=collect(this)
    | mode:=mode[len(mode)/2]
)
values stats(a)
# input
{a:[3,1,2]}
{a:[4]}
# expected output
{avg:2.,min:1,max:3,mode:2}
{avg:4.,min:4,max:4,mode:4}
```
---
_Function arguments are actually fields in the `this` record_

```mdtest-spq
# spq
fn that(a,b,c): this
values that(x,y,3)
# input
{x:1,y:2}
# expected output
{a:1,b:2,c:3}
```
---
_Functions passed as values do not appear in the `this` record_

```mdtest-spq
# spq
fn apply(f,arg):{that:this,result:f(arg)}
fn square(x):x*x
values apply(&square,val)
# input
{val:1}
{val:2}
# expected output
{that:{arg:1},result:1}
{that:{arg:2},result:4}
```
---
_Function parameters do not reach into subquery scope_

```mdtest-spq {data-layout="stacked"} fails
# spq
fn apply(a,val): (
  unnest a
  | collect(this+val)
)
values apply([1,2,3], 1)
# input

# expected output
no such field "val": . applied to int64 at line 3, column 18:
  | collect(this+val)
                 ~~~
```
---
_The compound unnest form brings parameters into subquery scope_

```mdtest-spq {data-layout="stacked"}
# spq
fn apply(a,val): (
  unnest {outer:val,item:a}
  | collect(outer+item)
)
values apply([1,2,3], 1)
# input

# expected output
[2,3,4]
```
