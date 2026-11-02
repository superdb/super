# named_type

form a named type from a name and a type

## Synopsis

```
named_type(name: string, t: type) -> type
```

## Description

The `named_type` function returns the [named type](../../types/named.md)
`name` for the type `t`, e.g., `named_type("port",<uint16>)` is `<port>`
where `port` is `uint16`.

If `name` is already bound to a different type, an error is returned.

## Examples

---

_Form a named type_

```mdtest-spq
# spq
values named_type("Point",record_type(this))
# input
{x:<float64>,y:<float64>}
# expected output
type Point={x:float64,y:float64}
<Point>
```
