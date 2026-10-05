# Strings

The `string` type represents any valid
[UTF-8 string](https://en.wikipedia.org/wiki/UTF-8).

For backward compatibility with SQL, syntactic aliases for type `string`
are defined as follows:
* `CHARACTER VARYING`
* `CHARACTER`
* `TEXT`
* `VARCHAR`

A string is formed by enclosing the string's unicode characters in
quotation marks whereby the following escape sequences allowed:

| Sequence | Unicode Character      |
|----------|------------------------|
| `\"`     | quotation mark  U+0022 |
| `\'`     | apostrophe      U+0027 |
| `\\`     | reverse solidus U+005C |
| `\/`     | solidus         U+002F |
| `\b`     | backspace       U+0008 |
| `\f`     | form feed       U+000C |
| `\n`     | line feed       U+000A |
| `\r`     | carriage return U+000D |
| `\t`     | tab             U+0009 |
| `\uXXXX` |                 U+XXXX |

The backslash character (`\`) and the control characters (U+0000 through U+001F)
must be escaped.

In SQL expressions, the quotation mark is a single quote character (`'`)
and in pipe expressions, the quotation mark may be either single quote or
double quote (`"`).

In single-quote strings, the single-quote character must
be escaped and in double-quote strings, the double-quote character must be
escaped.

## Raw String

Raw strings or _r-strings_
are expressed as the character `r` followed by a single- or double-quoted
string, where any backslash characters are treated literally and not as an
escape sequence.  For example, `r'foo\bar'` is equivalent to `'foo\\bar'`.

Unlike other string syntaxes, raw strings may have embedded newlines, e.g.,
a literal newline inside of the string rather than the character sequence `\n`
as in
```
r"foo
bar"
```

## Formatted Strings

Formatted strings or
[_f-strings_](../expressions/f-strings.md) are expressed
as the character `f` followed by a single- or double-quoted
string and may contain embedded expressions denoted within
curly braces `{` `}`.

## Examples
---
_Various strings_

```mdtest-spq
# spq
values 'hello, world', len('foo'), "SuperDB", "\"quoted\"", 'foo'||'bar'
# input

# expected output
"hello, world"
3
"SuperDB"
"\"quoted\""
"foobar"
```
---
_String literal vs field identifier in a SQL SELECT statement_

```mdtest-spq
# spq
SELECT 'x' as s, "x" as x
# input
{x:1}
{x:2}
# expected output
{s:"x",x:1}
{s:"x",x:2}
```
---
_Formatted strings_

```mdtest-spq
# spq
values f'{x} + {y} is {x+y}'
# input
{x:1,y:3}
{x:2,y:4}
# expected output
"1 + 3 is 4"
"2 + 4 is 6"
```
---
_Raw strings_

```mdtest-spq
# spq
values r'foo\nbar\t'
# input

# expected output
"foo\\nbar\\t"
```
---
_Raw strings with embedded newline_

```mdtest-spq
# spq
values r'foo
bar'
# input

# expected output
"foo\nbar"
```
