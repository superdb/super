# Function Calls

Functions compute a result from zero or more input arguments that
are passed by value as positional arguments.

A function call is an [expression](intro.md) having the form
```
<entity> ( [ <arg> [ , <arg> ... ]] )
```
where the `<entity>` is either an [identifier](../queries.md#identifiers) or
a [lambda expression](#lambda-expressions).  When the `<entity>` is
an identifier, it is one of
* the name of [built-in function](../functions/intro.md),
* the name of a [declared function](../declarations/functions.md) that is in [scope](../queries.md#scope), or
* a parameter name that resolves to a [function reference](#function-references)
  where the entity called is inside of a
  [declared function](../declarations/functions.md).

Each argument `<arg>` is either
* an [expression](intro.md) that is evaluated before the function is called
and passed as a value, or
* a [function reference](#function-references).

Functions are not first-class values and cannot be assigned to values
as there are no function values in the [ADM](../../formats/model.md).
Instead, functions may only be called or passed as a reference to another function.

## Lambda Expressions

A lambda expression is an [anonymous function](https://en.wikipedia.org/wiki/Anonymous_function) having the form
```
lambda [ <param> [ , <param> ... ]] : <expr>
```
where `<expr>` is any [expression](intro.md) defining the function and
each `<param>` is an identifier defining the positional parameters of the
function.

For example,
```
lambda x:x+1
```
is an anonymous function that adds one to its argument.

Like named functions, lambda expressions may only be called or passed
to another function as an argument.

For example,
```
lambda x:x+1 (2)
```
is the value `3` and
```
f(lambda x:x+1, 2)
```
calls the function `f` with the lambda as its first argument and the value `2`
as its second argument.

## Function References

The syntax for referencing a function by name is
```
& <name>
```
where `<name>` is an [identifier](../queries.md#identifiers) corresponding to
either a [built-in function](../functions/intro.md)
or a [declared function](../declarations/functions.md) that is in [scope](../queries.md#scope).

>[!NOTE]
> Many languages form function references simply by referring to their name
> without the need for a special symbol like`&`.  However, an ambiguity arises
> here between a field reference, which is not declared, and a function name.

For example,
```
&upper
```
is a reference to the built-in function [upper](../functions/strings/upper.md).

## Examples

---

_Sample calls to various built-in functions_

```mdtest-spq
# spq
values pow(2,3), lower("ABC")||upper("def"), typeof(1)
# input

# expected output
8.
"abcDEF"
<int64>
```

---

_Calling a lambda function_

```mdtest-spq
# spq
values lambda x:x+1 (2)
# input

# expected output
3
```
---

_Passing a lambda function_

```mdtest-spq
# spq
fn square(g,val):g(val)*g(val)
values square(lambda x:x+1, 2)
# input

# expected output
9
```
---

_Passing function references_

```mdtest-spq
# spq
fn inc(x):x+1
fn apply(val,sfunc,nfunc):
  case typeof(val)
  when <string> then sfunc(val)
  when <int64> then nfunc(val)
  else val
  end
values apply(this,&upper,&inc)
# input
1
"foo"
true
# expected output
2
"FOO"
true
```

---

_Function references may not be assigned to values_

```mdtest-spq fails
# spq
fn f():null
values {x:&f}
# input

# expected output
parse error at line 2, column 11:
values {x:&f}
      === ^ ===
```
