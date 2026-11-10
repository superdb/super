# Errors

Errors in SuperSQL are _first class_ and conform
with the [error type](../../formats/model.md#27-error) in the
super-structured data model.

Error types have the form
```
error ( <type> )
```
where `<type>` is any type.

Error values can be created with the error function of the form
```
error ( <value > )
```
where `<value>` is any value.

Error values can also be created by reading external data (SUP files or
database data) that contains serialized error values
or they can arise when any operator or function encounters
an error and produces an error value to describe the condition.

In general, expressions and functions that result in errors simply return
a value as an `error` type as a result.  This encourages a powerful flow-style
of error handling where errors simply propagate from one operation to the
next and land in the output alongside non-error values to provide a very helpful
context and rich information for tracking down the source of errors.  There is
no need to check for error conditions everywhere or look through auxiliary
logs to find out what happened.

The value underneath an error can be accessed using the
[`under`](../functions/generics/under.md) function.

For example,
```
values [1,2,3] | put x:=1
```
produces the error
```
error({message:"put: not a record",on:[1,2,3]})
```
and the original value in the `on` field can be recovered with `under`, i.e.,
```
values [1,2,3] | put x:=1 | values under(this).on
```
produces
```
[1,2,3]
```

## Structured Errors

First-class errors are particularly useful for creating structured errors.
When a SuperSQL query encounters a problematic condition,
instead of silently dropping the problematic error
and logging an error obscurely into some hard-to-find system log as so many
ETL pipelines do, the offending value can be wrapped as an error and
propagated to its output.

For example, suppose a bad value shows up:
```
{kind:"bad", stuff:{foo:1,bar:2}}
```
A data-shaping query applied to ingested data
could catch the bad value (e.g., as a default
case in a [`switch`](../operators/switch.md)) and propagate it as
an error using the expression:
```
values error({message:"unrecognized input type",on:this})
```
then such errors could be detected and searched for downstream with the
[`is_error`](../functions/errors/is_error.md) function.
For example,
```
is_error(this)
```
on the wrapped error from above produces
```
error({message:"unrecognized input",input:{kind:"bad", stuff:{foo:1,bar:2}}})
```
There is no need to create special tables in a complex warehouse-style ETL
to land such errors as they can simply land next to the output values themselves.

And when transformations cascade one into the next as different stages of
an ETL pipeline, errors can be wrapped one by one forming a "stack trace"
or lineage of where the error started and what stages it traversed before
landing at the final output stage.

Errors will unfortunately and inevitably occur even in production,
but having a first-class data type to manage them all while allowing them to
peacefully coexist with valid production data is a novel and
useful approach that SuperSQL enables.

## Missing

SuperDB's heterogeneous data model allows for queries
that operate over different types of data whose structure and type
may not be known ahead of time, e.g., different
types of records with different field names and varying structure.
Thus, a reference to a field, e.g., `this.x` may be valid for some values
that include a field called `x` but not valid for those that do not.

What is the value of `x` when the field `x` does not exist?

A similar question faced SQL when it was adapted in various different forms
to operate on semi-structured data like JSON or XML.  SQL already had the `null` value
so perhaps a reference to a missing value could simply be `null`.

But JSON also has `null`, so a reference to `x` in the JSON value
```
{"x":null}
```
and a reference to `x` in the JSON value
```
{}
```
would have the same value of `null`.  Furthermore, an expression like
`x is null`
could not differentiate between these two cases.

To solve this problem, the `missing` value was proposed to represent the value that
results from accessing a field that is not present.  Thus, `x is null` and
`x is missing` could disambiguate the two cases above.

SuperSQL, instead, recognizes that the SQL value `missing` is a paradox:
I'm here but I'm not.

In reality, a `missing` value is not a value.  It's an error condition
that resulted from trying to reference something that didn't exist.

So why should we pretend that this is a bona fide value?  SQL adopted this
approach because it lacks first-class errors.

But SuperSQL has first-class errors so
a reference to something that does not exist is an error.
For example,
```mdtest-spq
# spq
values x
# input
{x:1}
{y:2}
# expected output
1
error("no such field x")
```

SuperSQL does have a type called `missing`, but it plays a different role.
When values of different shapes are described by one type, e.g., after
[type fusion](../type-fusion.md), the type must say which fields are absent
from some of the values.  A field of type `int64|missing`, for instance,
is an `int64` where it's present and absent elsewhere.
So `missing` describes the shape of data rather than being a value that a
query produces: reading such a field where it's absent is still an error,
as above.
See [None and Missing](none.md) for how `missing` relates to `none`.

## Examples

---

_Any value can be an error_

```mdtest-spq
# spq
error(this)
# input
0
"foo"
10.0.0.1
{x:1,y:2}
# expected output
error(0)
error("foo")
error(10.0.0.1)
error({x:1,y:2})
```

---

_Divide by zero error_

```mdtest-spq
# spq
1/this
# input
0
# expected output
error("divide by zero")
```

---

_The error type corresponding to an error value_

```mdtest-spq
# spq
typeof(1/this)
# input
0
# expected output
<error(string)>
```

---

_Coalesce replaces `error("missing")` values with a default value_

```mdtest-spq
# spq
values coalesce(x, 0)
# input
{x:1}
{y:2}
# expected output
1
0
```
