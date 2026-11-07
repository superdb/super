# json_stringify

format a value as JSON text

## Synopsis

```
json_stringify(val: any) -> string
```

## Description

The `json_stringify` function formats `val` as JSON text in the same way as
the JSON [output format](../../../command/output.md) (`-j`), e.g., to send a record as
the body of an HTTP request made by [from](../../operators/from.md).
This is analogous to JavaScript's `JSON.stringify()` function and is the
inverse of [parse_sup](../parsing/parse_sup.md) for JSON values.

## Examples

---

```mdtest-spq
# spq
values json_stringify(this)
# input
{a:1,b:[true,"x"],c:null}
"hello"
1.5
# expected output
"{\"a\":1,\"b\":[true,\"x\"],\"c\":null}"
"\"hello\""
"1.5"
```
