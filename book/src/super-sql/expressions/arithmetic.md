# Arithmetic

Arithmetic operations (`*`, `/`, `%`, `+`, `-`) follow customary syntax
and semantics and are left-associative with multiplication and division having
precedence over addition and subtraction.  `%` is the modulo operator.

## Unary Sign

Any number may be signed with a unary operator having the form:
```
- <expr>
```
and
```
+ <expr>
```
where `<expr>` is any [expression](intro.md) that results in a [number](../types/numbers.md) type.

## Type Checking

Arithmetic on a value that isn't a number is an error, so when the type of an
operand is a [union](../types/union.md), the compiler requires each of its
types to be a number (or `null` or an [error](../types/error.md), which pass
through arithmetic).  Otherwise, the union must first be narrowed to its
numeric types, e.g., with a [type assertion](type-assertions.md) like
`x.(int64)??0`, or with a filter like `where is(x, <int64>)`.

## Examples

---

```mdtest-spq
# spq
values 2*3+1, 11%5, 1/0, +1, -1
# input

# expected output
7
1
error("divide by zero")
1
-1
```

---

_Arithmetic on a union with a non-numeric type is an error_

```mdtest-spq fails
# spq
values {x:1},{x:"foo"} | values x+1
# input

# expected output
type mismatch: int64|string + int64 at line 1, column 33:
values {x:1},{x:"foo"} | values x+1
                                ~~~
```

---

_Narrow the union with a type assertion_

```mdtest-spq
# spq
values {x:1},{x:"foo"} | values (x.(int64)??0)+1
# input

# expected output
2
1
```
