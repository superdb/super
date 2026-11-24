# grep

search strings inside of values

## Synopsis

```
grep(re: string, e: any) -> bool
```

## Description

The `grep` function searches all of the strings in its input value `e`
 using the `re` argument, which is a
[regular expression](../../queries.md#regular-expression).
If the pattern matches for any string, then the result is `true`.  Otherwise, it is `false`.

>[!NOTE]
> If `re` can match only one fixed string, as when it has no regular
> expression metacharacters (e.g., `"baz"`), `grep` looks for that string as
> a substring and ignores case, as a
> [keyword search](../../operators/search.md#keyword) does.
> Otherwise, the match is case-sensitive, so
> to ignore case in a regular expression, start it with `(?i)`.

The entire input value is traversed:
* for records, each field value is traversed or descended if a complex type,
* for arrays and sets, each element is traversed or descended if a complex type, and
* for maps, each key and value is traversed or descended if a complex type.

## Examples

---

_Reach into nested records_

```mdtest-spq
# spq
grep("baz", this)
# input
{foo:10}
{bar:{s:"baz"}}
# expected output
{bar:{s:"baz"}}
```

---

_It only matches string fields_

```mdtest-spq
# spq
grep("10", this)
# input
{foo:10}
{bar:{s:"baz"}}
# expected output
```

---

_Regular expression_

```mdtest-spq
# spq
grep("foo|baz", this)
# input
"foo"
{bar:{s:"baz"}}
# expected output
"foo"
{bar:{s:"baz"}}
```

---

_Regular expression with a non-this argument_

```mdtest-spq
# spq
grep('b.*', s.ok())
# input
{s:"bar"}
{s:"foo"}
{s:"baz"}
{t:"baz"}
# expected output
{s:"bar"}
{s:"baz"}
```

---

_A pattern without metacharacters ignores case, but a regular expression doesn't_

```mdtest-spq
# spq
values {
  input: this,
  plain: grep("BAZ", this),
  regexp: grep("B.Z", this),
  ignore_case: grep("(?i)B.Z", this)
}
# input
"baz"
# expected output
{input:"baz",plain:true,regexp:false,ignore_case:true}
```
