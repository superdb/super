# is_ok

test the validity of values

## Synopsis

```
is_ok(val: any) -> bool
```

## Description

The `is_ok` function is inspired by the Rust language pattern of the same name
and returns true when `val` is not `none` or an error.
It is a shortcut for `ok(val)!=none`.

This function is often used to determine the existence of fields in a
record, e.g., `is_ok(x)` is true when `this` is a record and has
the field `x`, provided its value is not an error or none.
As with `ok`, the type checker reports an error for a field that no
value can have.

It's also useful in shaping messy data when applying conditional logic based on the
presence of certain fields:
```
switch
  case a.is_ok() ( ... )
  case b.is_ok() ( ... )
  default ( ... )
```

## Examples

---

```mdtest-spq
# spq
values {yes:is_ok(foo),no:is_ok(bar)}
# input
{foo:10}
# expected output
{yes:true,no:false}
```

---

```mdtest-spq
# spq
values {yes: foo[1].is_ok(),no:foo[4].is_ok()}
# input
{foo:[1,2,3]}
# expected output
{yes:true,no:false}
```

---

```mdtest-spq
# spq
values {yes:foo.bar.is_ok(),no:foo.baz.is_ok()}
# input
{foo:{bar:"value"}}
# expected output
{yes:true,no:false}
```

---

```mdtest-spq
# spq
values {yes:is_ok(foo+1),no:is_ok(bar+1)}
# input
{foo:10}
# expected output
{yes:true,no:false}
```

---

```mdtest-spq
# spq
values bar.is_ok()
# input
1
# expected output
false
```
