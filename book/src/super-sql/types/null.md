# Nulls

The null type represents a type that has just one value:
the special value `null`.

In relational SQL, a null typically indicates an unknown value.
Unfortunately, this concept is overloaded as unknown values may arise
from runtime errors, missing data, or an intentional value of null.

Because SuperSQL has [_first-class errors_](error.md) (obviating the need to
serialize error conditions as nulls) and [_sum types_](union.md) (obviating
the need to flatten sum types into columns and occupy the absent component
types with nulls), the use of null values is discouraged.

That said, SuperSQL supports the null value for backward compatibility with
their pervasive use in SQL, database systems, programming languages, and serialization
formats.

As in SQL, to test if a value is null, it cannot be compared to another null
value, which by definition, is always false, i.e., two unknown values cannot
be known to be equal.  Instead the [`IS NULL`](../expressions/comparisons.md) operator or
[coalesce](../functions/generics/coalesce.md) function should be used.

## Examples
---
_The null value_

```mdtest-spq
# spq
values typeof(null)
# input

# expected output
<null>
```

---

_Test for null with IS NULL_

```mdtest-spq
# spq
values
  this == null,
  this != null,
  this IS NULL,
  this IS NOT NULL
# input

# expected output
null
null
true
false
```

---

_Error values are not null values_

```mdtest-spq
# spq
values {out:y}
# input
{x:1}
{x:2,y:3}
null
# expected output
{out:error("no such field y")}
{out:3}
{out:null}
```

---

_Use coalesce to easily skip over nulls and missing values_

```mdtest-spq
# spq
const DEFAULT = 100
values coalesce(y,x,DEFAULT)
# input
{x:1}
{x:2,y:3}
{x:4,y:null}
null
# expected output
1
3
4
100
```
