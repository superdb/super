# search

[✅](../intro.md#data-order)&ensp; select values based on a search expression

## Synopsis

```
search <sexpr> [ catch <handler> ]
? <sexpr> [ catch <handler> ]
```

## Description

The `search` operator provides a traditional keyword experience to SuperSQL
along the lines of web search, email search, or log search.

A search operation filters its input by applying the search expression `<sexpr>`
to each input value and emitting all values that match.

The `search` keyword can be abbreviated as `?`.

The errors of `<sexpr>` are dropped unless there's a
[catch clause](catch.md#catch-clauses), which sends them to `<handler>`, or a
[catch pragma](../declarations/pragmas.md).

### Search Expressions

The search expression syntax is unique to the search operator and provides
a hybrid syntax between keyword search and boolean expressions.

A search expression is a Boolean combination of _search terms_, where
a search term is one of:
* a [regular expression](#regular-expression) wrapped in `/` instead of quotes,
* a [glob](#glob) as described below,
* a textual [keyword](#keyword),
* any [literal](#literal) of a primitive type, or
* any [expression predicate](#expression-predicate).

#### Regular Expression

A search term may be a [regular expression](../queries.md#regular-expression).

To create a regular expression search term, the expression text is
prefixed and suffixed with a `/`.  This distinguishes a regular
expression from a string literal search, e.g.,
```
/foo|bar/
```
searches for the string `"foo"` or `"bar"` inside of any string entity while
```
"foo|bar"
```
searches for the string `"foo|bar"`.

#### Glob

A search term may be a [glob](../queries.md#glob).

Globs are distinguished from keywords by the presence of any wildcard
`*` character.  To search for a string containing such a character,
use a string literal instead of a keyword or escape the character as `\*`
in a keyword.

For example,
```
? foo*baz*.com
```
Searches for any string that begins with `foo`, has the string
`baz` in it, and ends with `.com`.

Note that a glob may look like multiplication but context disambiguates
these conditions, e.g.,
```
a*b
```
is a glob match for any matching string value in the input, but
```
a*b==c
```
is a Boolean comparison between the product `a*b` and `c`.

#### Keyword

Keywords and string literals are equivalent search terms so it is often
easier to quote a string search term instead of using escapes in a keyword.
Keywords are useful in interactive workflows where searches can be issued
and modified quickly without having to type matching quotes.

Keyword search has the look and feel of Web search or email search.

Valid keyword characters include `a` through `z`, `A` through `Z`,
any valid string escape sequence
(along with escapes for `*`, `=`, `+`, `-`), and the unescaped characters:
```
_ . : / % # @ ~
```
A keyword must begin with one of these characters then may be
followed by any of these characters or digits `0` through `9`.

A keyword search is equivalent to
```
grep(<keyword>, this)
```
where `<keyword>` is the quoted string-literal of the unquoted string.
For example,
```
search foo
```
is equivalent to
```
where grep("foo", this)
```

Note that the shorthand `?` may be used in lieu of the "search" keyword.
For example, the simplest SuperSQL query is perhaps a single keyword search, e.g.,
```
? foo
```
As above, this query searches the implied input for values that
contain the string "foo".

#### Literal

Search terms representing non-string values search for both an exact
match for the given value as well as a string search for the term exactly
as it appears as typed.  Such values include:
* integers,
* floating point numbers,
* time values,
* durations,
* IP addresses,
* networks,
* bytes values, and
* type values.

Search terms representing literal strings behave as a keyword search
of the same text.

A search for a value `<value>` represented as the string `<string>` is
equivalent to
```
<value> in this or grep(<string>, this)
```
For example,
```
search 123 and 10.0.0.1
```
which can be abbreviated
```
? 123 10.0.0.1
```
is equivalent to
```
where (123 in this or grep("123", this))
  and (10.0.0.1 in this or grep("10.0.0.1", this))
```

Complex values are not supported as search terms but may be queried with
the [in](../expressions/containment.md) operator, e.g.,
```
{s:"foo"} in this
```

#### Expression Predicate

Any Boolean-valued [function](../functions/intro.md) like
[`is`](../functions/types/is.md),
[`is_ok`](../functions/generics/is_ok.md),
[`grep`](../functions/strings/grep.md),
etc. and any [comparison expression](../expressions/comparisons.md)
may be used as a search term and mixed into a search expression.

For example,
```
? is(this, <foo>) has(bar) baz x==y+z timestamp > 2018-03-24T17:17:55Z
```
is a valid search expression but
```
? /foo.*/ x+1
```
is not.

#### Boolean Logic

Search terms may be combined into boolean expressions using logical operators
`and`, `or`, `not`, and `!`.  `and` may be elided; i.e., concatenation of
search terms is a logical `and`.  `not` (and its equivalent `!`) has highest
precedence and `and` has precedence over `or`.  Parentheses may be used to
override natural precedence.

Note that the concatenation form of `and` is not valid in standard expressions and
is available only in search expressions.

For example,
```
? not foo bar or baz
```
means
```
((not grep("foo", this)) and grep("bar", this)) or grep("baz", this)
```
while
```
? foo (bar or baz)
```
means
```
grep("foo", this) and (grep("bar", this) or grep("baz", this))
```

## Examples

---

_A simple keyword search for "world"_
```mdtest-spq
# spq
search world
# input
"hello, world"
"say hello"
"goodbye, world"
# expected output
"hello, world"
"goodbye, world"
```

---

Search can utilize _arithmetic comparisons_
```mdtest-spq
# spq
search this >= 2
# input
1
2
3
# expected output
2
3
```

---

_The "search" keyword can be abbreviated as "?"_
```mdtest-spq
# spq
? 2 or 3
# input
1
2
3
# expected output
2
3
```

---

_A search with [Boolean logic](#boolean-logic)_
```mdtest-spq
# spq
search this >= 2 AND this <= 2
# input
1
2
3
# expected output
2
```

---

_The AND operator may be omitted through predicate concatenation_
```mdtest-spq
# spq
search this >= 2 this <= 2
# input
1
2
3
# expected output
2
```

---

_Concatenation for keyword search_
```mdtest-spq
# spq
? foo bar
# input
"foo"
"foo bar"
"foo baz bar"
"baz"
# expected output
"foo bar"
"foo baz bar"
```

---

_Boolean functions may be called_
```mdtest-spq
# spq
search is(this, <int64>)
# input
1
"foo"
10.0.0.1
# expected output
1
```

---

_Boolean functions with [Boolean logic](#boolean-logic)_
```mdtest-spq
# spq
search is(this, <int64>) or is(this, <ip>)
# input
1
"foo"
10.0.0.1
# expected output
1
10.0.0.1
```

---

_Search with regular expressions_

```mdtest-spq
# spq
? /foo|bar/
# input
"foo"
{s:"bar"}
{s:"baz"}
# expected output
"foo"
{s:"bar"}
```

---

_A prefix match using a glob_

```mdtest-spq
# spq
? b*
# input
"foo"
{s:"bar"}
{s:"baz"}
{foo:1}
# expected output
{s:"bar"}
{s:"baz"}
```

---

_A suffix match using a glob_

```mdtest-spq
# spq
? *z
# input
"foo"
{s:"bar"}
{s:"baz"}
{foo:1}
# expected output
{s:"baz"}
```

---

_A glob with stars on both sides is like a string search_

```mdtest-spq
# spq
? *a*
# input
"foo"
{s:"bar"}
{s:"baz"}
"a"
# expected output
{s:"bar"}
{s:"baz"}
"a"
```
