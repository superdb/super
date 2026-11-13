# catch

[🎲](../intro.md#data-order)&ensp; send values with errors to a handler

## Synopsis

```
catch ( <query> )
catch halt
catch ignore
catch pass
```

## Description

The `catch` operator passes each input value that has no
[error](../types/error.md) in it to its output and sends each value that does
(i.e., for which [has_error](../functions/errors/has_error.md)`(this)` is true)
to a _handler_.  The handler is one of:

* `( <query> )`, which runs `<query>` on the values that have errors and
  outputs its results along with the passed values,
* `halt`, which is short for `( halt this )` and stops the query at the first
  value with an error (see [halt](halt.md)),
* `ignore`, which drops the values with errors, or
* `pass`, which passes them like any other value.

A bug-free query has no errors except those it produces on purpose, so
`catch` is a way to handle errors programmatically: to send them somewhere
(e.g., to a [debug](debug.md) output, a named output with `output`, or a
table with [load](load.md)), to fail fast with `halt`, or to replace them with
other values.  Since the handler is an ordinary query, a handler can contain
another `catch`.

The order of the handler's results relative to the passed values is
undefined.  Since the values are processed concurrently in batches, a value
that follows an error may be output before a `halt` in the handler stops the
query.

### Catch Clauses

Some operators consume the values of an expression rather than outputting them,
like the predicate of [where](where.md) and [search](search.md) and the
condition of [join](join.md).  The errors of such an expression, called
_side errors_, don't appear in the operator's output and are dropped by
default.  These operators take an optional _catch clause_ that sends their side
errors to a handler instead:
```
where <expr> catch <handler>
search <expr> catch <handler>
... join ... on <expr> catch <handler>
```
where `<handler>` is one of the handlers above.  With `catch pass`, the side
errors are output along with the operator's other output.

The handler's input for each side error is an error of the form
```
error({message:"<op>: <description>",on:<value>,error:<error>})
```
where `<value>` is the value on which the expression produced the error
`<error>`: the input value of a `where` or `search`, and for a `join`, the
record of joined values or, for an error in the key of an equality join, a
record of the value whose key has the error (e.g., `{left:<value>}`). Such a
value matches nothing, so an outer join outputs it as unmatched.

### Catching Everywhere

The [catch pragma](../declarations/pragmas.md) puts a `catch` with its handler
at the output of every operator in its scope and makes its handler the default
handler of side errors.

## Examples

---

_Send values with errors to a handler_
```mdtest-spq
# spq
values 10/this
| catch ( values {bad:this} )
| sort this
# input
1
"two"
5
# expected output
2
10
{bad:error("incompatible types")}
```

---

_Drop values with errors_
```mdtest-spq
# spq
values 10/this | catch ignore
# input
1
"two"
5
# expected output
10
2
```

---

_Stop at the first value with an error_
```mdtest-spq fails
# spq
values 10/this | catch halt
# input
"two"
# expected output
error("incompatible types")
query halted
```

---

_Handle the side errors of a `where` predicate_
```mdtest-spq
# spq
where x + 1 > 2 catch ( values {bad:under(this).on} )
| sort this
# input
{x:1}
{x:"two"}
{x:3}
# expected output
{bad:{x:"two"}}
{x:3}
```

---

_Count the side errors of a `join` condition_
```mdtest-spq
# spq
join (values {id:1},{id:2}) on left.id + 0 = right.id
  catch ( aggregate errors:=count() )
| sort this
# input
{id:1}
{id:"two"}
# expected output
{errors:1}
{left:{id:1},right:{id:1}}
```
