# Playground

If you have `super` [installed](install.md),
a common pattern for experimentation is to
"echo" some input to the `super -c` command, e.g.,
```
echo <values> | super -c <query> -
```
But you can also experiment with SuperDB using the browser-embedded
playground.  The SuperDB code has been compiled to WebAssembly
and executes a `super -c <query> -` command like this:
```mdtest-spq
# spq
SELECT upper(message) AS out
# input
{id:0,message:"Hello"}
{id:1,message:"Goodbye"}
# expected output
{out:"HELLO"}
{out:"GOODBYE"}
```
The QUERY and INPUT panes are editable.  So go ahead and experiment.
Try changing `upper` to `lower` in the query text
and you should get this alternative output in the RESULT panel above:
```
{out:"hello"}
{out:"goodbye"}
```
The input in the playground examples are generally formatted as
[SUP](../formats/sup.md) but the `super` playground command auto-detects
the format, so you can experiment with other text-based formats like CSV or JSON.
For example, if you change the input above to
```
id,message
0,"Hello"
1,"Goodbye"
```
`super` will detect this as CSV and you will get the same result.

## Examples

To explore a broad range of SuperSQL functionality,
try browsing the documentation for
[pipe operators](../super-sql/operators/intro.md),
[SQL operators](../super-sql/sql/intro.md#sql-operator),
or
[functions](../super-sql/functions/intro.md).

Each operator and function has a section of examples
with playgrounds where you can edit
the example queries and inputs to explore how SuperSQL works.

Here are a few examples to get going.

---

**_Hello, world_**

```mdtest-command
super -s -c "SELECT 'hello, world' as s"
```
produces this SUP output
```mdtest-output
{s:"hello, world"}
```

---

**_Some values of available [data types](../super-sql/types/intro.md)_**

```mdtest-spq
# spq
SELECT in as out
# input
{in:1}
{in:1.5}
{in:[1,"foo"]}
{in:set["apple","banana"]}
# expected output
{out:1}
{out:1.5}
{out:[1,"foo"]}
{out:set["apple","banana"]}
```

---

**_The types of various data_**

```mdtest-spq
# spq
SELECT typeof(in) as typ
# input
{in:1}
{in:1.5}
{in:[1,"foo"]}
{in:set["apple","banana"]}
# expected output
{typ:<int64>}
{typ:<float64>}
{typ:<[int64|string]>}
{typ:<set[string]>}
```

---

**_A simple [aggregation](../super-sql/operators/aggregate.md)_**
```mdtest-spq
# spq
sum(val) by key | sort key
# input
{key:"foo",val:1}
{key:"bar",val:2}
{key:"foo",val:3}
# expected output
{key:"bar",sum:2}
{key:"foo",sum:4}
```

---

**_Read CSV input and [cast](../super-sql/functions/types/cast.md) a to an integer from default float_**
```mdtest-spq
# spq
a:=a::int64
# input
a,b
1,foo
2,bar
# expected output
{a:1,b:"foo"}
{a:2,b:"bar"}
```

---

**_Read JSON input and cast to an integer from default float_**
```mdtest-spq
# spq
a:=a::int64
# input
{"a":1,"b":"foo"}
{"a":2,"b":"bar"}
# expected output
{a:1,b:"foo"}
{a:2,b:"bar"}
```

---

**_Make a schema-rigid Parquet file using fuse, then output the Parquet file
as [SUP](../formats/sup.md)_**
```mdtest-command-skip
echo '{a:1}{a:2}{b:3}' | super -o tmp.parquet -c fuse -
super -s tmp.parquet
```
produces
```mdtest-output-skip
{a:1::(int64|null),b:null::(int64|null)}
{a:2::(int64|null),b:null::(int64|null)}
{a:null::(int64|null),b:3::(int64|null)}
```
