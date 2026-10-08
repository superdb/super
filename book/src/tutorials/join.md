## Pipe Joins

> **TODO: updage this in light of progress on join and integrate SQL discussions**

Joins in SuperSQL pipe context are a bit different than SQL joins.
Pipe joins use multi-column records to combine data instead of
SQL selections that combine data in a projection from different relations.

This document covers the SuperSQL [join](../super-sql/operators/join.md) operator
in some depth with a number example illustrating different join patterns.

### Example Data

The first input data source for our usage examples is `fruit.json`, which describes
the characteristics of some fresh produce.

```mdtest-input fruit.json
{"name":"apple","color":"red","flavor":"tart"}
{"name":"banana","color":"yellow","flavor":"sweet"}
{"name":"avocado","color":"green","flavor":"savory"}
{"name":"strawberry","color":"red","flavor":"sweet"}
{"name":"dates","color":"brown","flavor":"sweet","note":"in season"}
{"name":"figs","color":"brown","flavor":"plain"}
```

The other input data source is `people.json`, which describes the traits
and preferences of some potential eaters of fruit.

```mdtest-input people.json
{"name":"morgan","age":61,"likes":"tart"}
{"name":"quinn","age":14,"likes":"sweet","note":"many kids enjoy sweets"}
{"name":"jessie","age":30,"likes":"plain"}
{"name":"chris","age":47,"likes":"tart"}
```

### Inner Join

We'll start by outputting only the fruits liked by at least one person.
The name of the matching person is copied into a field of a different name in
the joined results.

Because we're performing an inner join (the default), the
explicit `inner` is not strictly necessary, but including it clarifies our intention.

The query `inner-join.spq`:
```mdtest-input inner-join.spq
from fruit.json
| inner join (
  from people.json
) as {f,p} on f.flavor=p.likes
| values {...f, eater:p.name}
| sort flavor
```

Executing the query:
```mdtest-command
super -s -I inner-join.spq
```
produces
```mdtest-output
{name:"figs",color:"brown",flavor:"plain",eater:"jessie"}
{name:"banana",color:"yellow",flavor:"sweet",eater:"quinn"}
{name:"strawberry",color:"red",flavor:"sweet",eater:"quinn"}
{name:"dates",color:"brown",flavor:"sweet",note:"in season",eater:"quinn"}
{name:"apple",color:"red",flavor:"tart",eater:"morgan"}
{name:"apple",color:"red",flavor:"tart",eater:"chris"}
```

### Left Join

> **TODO: this statement is not aligned with our audience**

By performing a left join that targets the same key fields, now all of our
fruits will be shown in the results even if no one likes them (e.g., `avocado`).

As another variation, we'll also copy over the age of the matching person. By
referencing only the field name rather than using `:=` for assignment, the
original field name `age` is maintained in the results.

The query `left-join.spq`:
```mdtest-input left-join.spq
from fruit.json
| left join (
  from people.json
) as {f,p} on f.flavor=p.likes
| f.eater := p.name, f.age := p.age
| values f
| sort flavor
```

Executing the query:

```mdtest-command
super -s -I left-join.spq
```
produces
```mdtest-output
{name:"figs",color:"brown",flavor:"plain",eater:"jessie",age:30}
{name:"avocado",color:"green",flavor:"savory",eater:error({message:"'.': applied to non-record",on:error({message:"no such field p",on:{f:{name:"avocado",color:"green",flavor:"savory"}}})}),age:error({message:"'.': applied to non-record",on:error({message:"no such field p",on:{f:{name:"avocado",color:"green",flavor:"savory"}}})})}
{name:"banana",color:"yellow",flavor:"sweet",eater:"quinn",age:14}
{name:"strawberry",color:"red",flavor:"sweet",eater:"quinn",age:14}
{name:"dates",color:"brown",flavor:"sweet",note:"in season",eater:"quinn",age:14}
{name:"apple",color:"red",flavor:"tart",eater:"morgan",age:61}
{name:"apple",color:"red",flavor:"tart",eater:"chris",age:47}
```

### Right join

**TODO: this statement is not aligned with our audience**

>[!NOTE]
> In SQL, a right join is called a _right outer join_.

Next we'll change the join type from `left` to `right`. Notice that this causes
the `note` field from the right-hand input to appear in the joined results.

The query `right-join.spq`:
```mdtest-input right-join.spq
from fruit.json
| right join (
  from people.json
) as {f,p} on f.flavor=p.likes
| p.fruit:=f.name
| values p
| sort likes
```
Executing the query:
```mdtest-command
super -s -I right-join.spq
```
produces
```mdtest-output
{name:"jessie",age:30,likes:"plain",fruit:"figs"}
{name:"quinn",age:14,likes:"sweet",note:"many kids enjoy sweets",fruit:"banana"}
{name:"quinn",age:14,likes:"sweet",note:"many kids enjoy sweets",fruit:"strawberry"}
{name:"quinn",age:14,likes:"sweet",note:"many kids enjoy sweets",fruit:"dates"}
{name:"morgan",age:61,likes:"tart",fruit:"apple"}
{name:"chris",age:47,likes:"tart",fruit:"apple"}
```

### Anti join

> **TODO: nibs brought up right anti join, which duckdb doesn't seem to do**

The join type `anti` allows us to see which fruits are not liked by anyone.
Note that with anti join only values from the left-hand input appear in the
results.

The query `anti-join.spq`:
```mdtest-input anti-join.spq
from fruit.json
| anti join (
  from people.json
) as {fruit,people} on fruit.flavor=people.likes
| values fruit
```
Executing the query:
```mdtest-command
super -s -I anti-join.spq
```
produces
```mdtest-output
{name:"avocado",color:"green",flavor:"savory"}
```

### Inputs from Pools

> **TODO: no more file operator**

In the examples above, we used the
[`file` operator](../super-sql/operators/from.md) to read our respective inputs
from named file sources.  However, if the inputs are stored in pools in a [SuperDB data lake](../command/db.md),
we would instead specify the sources as data pools using the
[`from` operator](../super-sql/operators/from.md).

Here we'll load our input data to pools in a temporary data lake, then execute
our inner join using `super db`.

The query `inner-join-pools.spq`:

```mdtest-input inner-join-pools.spq
from fruit
| inner join (
  from people
) as {fruit,people} on fruit.flavor=people.likes
| values {...fruit, eater:people.name}
| sort flavor, name
```

Populating the pools, then executing the query:

```mdtest-command
export SUPER_DB=lake
super db init -q
super db create -q -orderby flavor:asc fruit
super db create -q -orderby likes:asc people
super db load -q -use fruit fruit.json
super db load -q -use people people.json
super db -s -I inner-join-pools.spq
```
produces
```mdtest-output
{name:"figs",color:"brown",flavor:"plain",eater:"jessie"}
{name:"banana",color:"yellow",flavor:"sweet",eater:"quinn"}
{name:"dates",color:"brown",flavor:"sweet",note:"in season",eater:"quinn"}
{name:"strawberry",color:"red",flavor:"sweet",eater:"quinn"}
{name:"apple",color:"red",flavor:"tart",eater:"morgan"}
{name:"apple",color:"red",flavor:"tart",eater:"chris"}
```

### Alternate Syntax

In addition to the syntax shown so far, `join` supports an alternate syntax in
which left and right inputs are specified by the two branches of a preceding
[`fork`](../super-sql/operators/fork.md),
[`from`](../super-sql/operators/from.md), or
[`switch`](../super-sql/operators/switch.md) operator.

Here we'll use the alternate syntax to perform the same inner join shown earlier
in the [Inner Join section](#inner-join).

The query `inner-join-alternate.spq`:
```mdtest-input inner-join-alternate.spq
fork
  ( from fruit.json )
  ( from people.json )
| inner join as {fruit,people} on fruit.flavor=people.likes
| values {...fruit, eater:people.name}
| sort flavor
```

Executing the query:
```mdtest-command
super -s -I inner-join-alternate.spq
```
produces
```mdtest-output
{name:"figs",color:"brown",flavor:"plain",eater:"jessie"}
{name:"banana",color:"yellow",flavor:"sweet",eater:"quinn"}
{name:"strawberry",color:"red",flavor:"sweet",eater:"quinn"}
{name:"dates",color:"brown",flavor:"sweet",note:"in season",eater:"quinn"}
{name:"apple",color:"red",flavor:"tart",eater:"morgan"}
{name:"apple",color:"red",flavor:"tart",eater:"chris"}
```

### Self Joins

In addition to the named files and pools like we've used in the prior examples,
SuperSQL also works on a single sequence of data that is split and
joined to itself.  Here we'll combine our file sources into a stream that we'll
pipe into `super` via stdin.  Because `join` requires two separate inputs, here
we'll use the `has()` function inside a `switch` operation to identify the
records in the stream that will be treated as the left and right sides.  Then
we'll use the [alternate syntax for `join`](#alternate-syntax) to read those two
inputs.

The query `inner-join-streamed.spq`:

```mdtest-input inner-join-streamed.spq
switch
  case color.is_ok() ( pass )
  case age.is_ok() ( pass )
| inner join as {fruit,people} on fruit.flavor=people.likes
| values {...fruit, eater:people.name}
| sort flavor
```

Executing the query:
```mdtest-command
cat fruit.json people.json | super -s -I inner-join-streamed.spq -
```
produces
```mdtest-output
{name:"figs",color:"brown",flavor:"plain",eater:"jessie"}
{name:"banana",color:"yellow",flavor:"sweet",eater:"quinn"}
{name:"strawberry",color:"red",flavor:"sweet",eater:"quinn"}
{name:"dates",color:"brown",flavor:"sweet",note:"in season",eater:"quinn"}
{name:"apple",color:"red",flavor:"tart",eater:"morgan"}
{name:"apple",color:"red",flavor:"tart",eater:"chris"}
```

### Multi-value Joins

The equality test in a `join` accepts only one named key from each input.
However, joins on multiple matching values can still be performed by making the
values available in comparable complex types, such as embedded records.

To illustrate this, we'll introduce some new input data `inventory.json`
that represents a vendor's available quantity of fruit for sale. As the colors
indicate, they separately offer both ripe and unripe fruit.

```mdtest-input inventory.json
{"name":"banana","color":"yellow","quantity":1000}
{"name":"banana","color":"green","quantity":5000}
{"name":"strawberry","color":"red","quantity":3000}
{"name":"strawberry","color":"white","quantity":6000}
```

Let's assume we're interested in seeing the available quantities of only the
ripe fruit in our `fruit.json`
records. In the query `multi-value-join.spq`, we create the keys as
embedded records inside each input record, using the same field names and data
types in each. We'll leave the created `fruitkey` records intact to show what
they look like, but since it represents redundant data, in practice we'd
typically [`drop`](../super-sql/operators/drop.md) it after the `join` in our pipeline.

```mdtest-input multi-value-join.spq
from fruit.json | put fruitkey:={name,color}
| inner join (
  from inventory.json | put invkey:={name,color}
) on left.fruitkey=right.invkey
| values {...left, quantity:right.quantity}
```

Executing the query:
```mdtest-command
super -s -I multi-value-join.spq
```
produces
```mdtest-output
{name:"banana",color:"yellow",flavor:"sweet",fruitkey:{name:"banana",color:"yellow"},quantity:1000}
{name:"strawberry",color:"red",flavor:"sweet",fruitkey:{name:"strawberry",color:"red"},quantity:3000}
```

### Joining More Than Two Inputs

While the `join` operator takes only two inputs, more inputs can be joined by
extending the pipeline.

To illustrate this, we'll introduce some new input data in `prices.json`.

```mdtest-input prices.json
{"name":"apple","price":3.15}
{"name":"banana","price":4.01}
{"name":"avocado","price":2.50}
{"name":"strawberry","price":1.05}
{"name":"dates","price":6.70}
{"name":"figs","price": 1.60}
```

In our query `three-way-join.spq` we'll extend the pipeline we used
previously for our inner join by piping its output to an additional join
against the price list.

```mdtest-input three-way-join.spq
from fruit.json
| inner join (
  from people.json
) as {fruit,people} on fruit.flavor=people.likes
| values {...fruit, eater:people.name}
| inner join (
  from prices.json
) as {fruit,prices} on fruit.name=prices.name
| values {...fruit, price:prices.price}
| sort name
```

Executing the query:

```mdtest-command
super -s -I three-way-join.spq
```

produces

```mdtest-output
{name:"apple",color:"red",flavor:"tart",eater:"morgan",price:3.15}
{name:"apple",color:"red",flavor:"tart",eater:"chris",price:3.15}
{name:"banana",color:"yellow",flavor:"sweet",eater:"quinn",price:4.01}
{name:"dates",color:"brown",flavor:"sweet",note:"in season",eater:"quinn",price:6.7}
{name:"figs",color:"brown",flavor:"plain",eater:"jessie",price:1.6}
{name:"strawberry",color:"red",flavor:"sweet",eater:"quinn",price:1.05}
```

### Including the entire opposite record

In the current `join` implementation, explicit entries must be provided in the
`[field-list]` in order to copy values from the opposite input into the joined
results (a possible future enhancement [super/2815](https://github.com/superdb/super/issues/2815)
may improve upon this). This can be cumbersome if your goal is to copy over many
fields or you don't know the names of all desired fields.

One way to work around this limitation is to specify `this` in the field list
to copy the contents of the _entire_ opposite record into an embedded record
in the result.

The query `embed-opposite.spq`:

```mdtest-input embed-opposite.spq
from fruit.json
| inner join (
  from people.json
) as {fruit,people} on fruit.flavor=people.likes
| values {...fruit, eaterinfo:people}
| sort flavor
```

Executing the query:

```mdtest-command
super -s -I embed-opposite.spq
```
produces
```mdtest-output
{name:"figs",color:"brown",flavor:"plain",eaterinfo:{name:"jessie",age:30,likes:"plain"}}
{name:"banana",color:"yellow",flavor:"sweet",eaterinfo:{name:"quinn",age:14,likes:"sweet",note:"many kids enjoy sweets"}}
{name:"strawberry",color:"red",flavor:"sweet",eaterinfo:{name:"quinn",age:14,likes:"sweet",note:"many kids enjoy sweets"}}
{name:"dates",color:"brown",flavor:"sweet",note:"in season",eaterinfo:{name:"quinn",age:14,likes:"sweet",note:"many kids enjoy sweets"}}
{name:"apple",color:"red",flavor:"tart",eaterinfo:{name:"morgan",age:61,likes:"tart"}}
{name:"apple",color:"red",flavor:"tart",eaterinfo:{name:"chris",age:47,likes:"tart"}}
```

If embedding the opposite record is undesirable, the left and right
records can easily be merged with the
[spread operator](../super-sql/types/record.md#record-expressions). Additional
processing may be necessary to handle conflicting field names, such as
in the example just shown where the `name` field is used differently in the
left and right inputs. We'll demonstrate this by augmenting `embed-opposite.spq`
to produce `merge-opposite.spq`.

```mdtest-input merge-opposite.spq
from fruit.json
| inner join (
  from people.json
) as {fruit,people} on fruit.flavor=people.likes
| rename fruit.fruit:=fruit.name
| values {...fruit,...people}
| sort flavor
```

Executing the query:

```mdtest-command
super -s -I merge-opposite.spq
```

produces

```mdtest-output
{fruit:"figs",color:"brown",flavor:"plain",name:"jessie",age:30,likes:"plain"}
{fruit:"banana",color:"yellow",flavor:"sweet",name:"quinn",age:14,likes:"sweet",note:"many kids enjoy sweets"}
{fruit:"strawberry",color:"red",flavor:"sweet",name:"quinn",age:14,likes:"sweet",note:"many kids enjoy sweets"}
{fruit:"dates",color:"brown",flavor:"sweet",note:"many kids enjoy sweets",name:"quinn",age:14,likes:"sweet"}
{fruit:"apple",color:"red",flavor:"tart",name:"morgan",age:61,likes:"tart"}
{fruit:"apple",color:"red",flavor:"tart",name:"chris",age:47,likes:"tart"}
```
