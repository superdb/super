# where

[✅](../intro.md#data-order)&ensp; select values based on a Boolean expression

## Synopsis

```
[where] <expr> [ catch <handler> ]
```

## Description

The `where` operator filters its input by applying a Boolean
[expression](../expressions/intro.md) `<expr>`
to each input value and dropping each value for which the expression evaluates
to `false` or to an error.

The errors of `<expr>` are dropped unless there's a
[catch clause](catch.md#catch-clauses), which sends them to `<handler>`, or a
[catch pragma](../declarations/pragmas.md).

The `where` keyword is optional since it is a [shortcut](intro.md#shortcuts).

When SuperSQL queries are run interactively, it is highly convenient to be able to omit
the "where" keyword, but when `where` filters appear in query source files,
it is good practice to include the optional keyword.

## Examples

---

_An arithmetic comparison_
```mdtest-spq
# spq
where this >= 2
# input
1
2
3
# expected output
2
3
```

---

_The "where" keyword may be dropped_
```mdtest-spq
# spq
this >= 2
# input
1
2
3
# expected output
2
3
```

---

_A filter with Boolean logic_
```mdtest-spq
# spq
where this >= 2 AND this <= 2
# input
1
2
3
# expected output
2
```

---

_A filter with array [containment](../expressions/containment.md) logic_
```mdtest-spq
# spq
where this in [1,4]
# input
1
2
3
4
# expected output
1
4
```

---

_A filter with inverse containment logic_
```mdtest-spq
# spq
where ! (this in [1,4])
# input
1
2
3
4
# expected output
2
3
```

---

_Boolean functions may be called_
```mdtest-spq
# spq
where a.is_ok()
# input
{a:1}
{b:"foo"}
{a:10.0.0.1,b:"bar"}
# expected output
{a:1}
{a:10.0.0.1,b:"bar"}
```

---

_Boolean functions with Boolean logic_
```mdtest-spq
# spq
where a.is_ok() or b.is_ok()
# input
{a:1}
{b:"foo"}
{a:10.0.0.1,b:"bar"}
# expected output
{a:1}
{b:"foo"}
{a:10.0.0.1,b:"bar"}
```
