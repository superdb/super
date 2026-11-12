# dedup_type

name record types shared by fields in a map of types

## Synopsis

```
dedup_type(m: map{string:type}) -> map{string:type}
```

## Description

The `dedup_type` function takes a map of names to types, like the argument of
[sum_type](sum_type.md), and returns the map with its repeated record types
replaced by [named types](../../types/named.md) that its types share.

A record type is replaced if it isn't named and is the type of more than one
field with the same name, at any depth in the map's types, including
optional fields.  The name of its named type is the field name in CamelCase,
i.e., the field name's words, separated by underscores or other characters
that aren't letters or digits, are capitalized and joined, so the type of
field `home_address` is named `HomeAddress`.  Records within records are
named first, so an outer record type can refer to the named types of its
inner records.

A record type isn't replaced if
* its name is already bound to a different type,
* another record type that's replaced would have the same name, or
* it's within the body of a named type.

## Examples

---

_Name a record type shared by two types_

```mdtest-spq
# spq
values dedup_type(this)
# input
map{
  "Foo":<{x:int64,thing:{s:string,b:bool}}>,
  "Bar":<{q:[string],thing:{s:string,b:bool}}>
}
# expected output
type Thing={s:string,b:bool}
map{"Bar":<{q:[string],thing:Thing}>,"Foo":<{x:int64,thing:Thing}>}
```

---

_Name nested record types_

```mdtest-spq
# spq
values dedup_type(this)
# input
map{
  "Home":<{home_address:{street:string,geo:{lat:float64,lon:float64}}}>,
  "Trip":<{home_address:{street:string,geo:{lat:float64,lon:float64}},stops:[{geo:{lat:float64,lon:float64}}]}>
}
# expected output
type Geo={lat:float64,lon:float64}
type HomeAddress={street:string,geo:Geo}
map{"Home":<{home_address:HomeAddress}>,"Trip":<{home_address:HomeAddress,stops:[{geo:Geo}]}>}
```

---

_Compute a sum type of events with a shared record type_

```mdtest-spq
# spq
fuse(this) by kind
| collect_map(map{kind:fuse})
| values sum_type(dedup_type(this))
# input
{kind:"Push",ref:"main",actor:{id:1,login:"ann"}}
{kind:"Watch",action:"started",actor:{id:2,login:"bob"}}
# expected output
type Actor={id:int64,login:string}
type Push={kind:string,ref:string,actor:Actor}
type Watch={kind:string,action:string,actor:Actor}
<Push|Watch>
```
