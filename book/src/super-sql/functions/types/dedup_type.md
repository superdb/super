# dedup_type

name record types shared by fields in a map of types

## Synopsis

```
dedup_type(m: map{string:type}) -> map{string:type}
```

## Description

The `dedup_type` function takes a map of names to types, like the argument of
[sum_type](sum_type.md), and returns the map with each record type that
its types share replaced by a [named type](../../types/named.md).

A record type is shared if it isn't named and appears more than once within
the map's types, at any depth, e.g., as the type of two fields, the element
type of an array, or a member of a union.  (A record type that's within a
shared record type isn't shared by appearing in it, since the shared type's
body appears only once, but it is if it also appears elsewhere.)  The
types of the map's entries themselves, empty record types, and the bodies of
named types are left as is.

The name of a shared type is derived from the names of the fields with that
type, each in CamelCase (so `home_address` is `HomeAddress`) and in the singular
if the type is the element type of an array, set, or map (so `commits` is
`Commit`).  The name is
* the field name, if all of the fields have the same name, else
* the words the field names end with, if they end with the same words
  (e.g., `Address` for `home_address` and `work_address`), else
* the most frequent field name, else
* the field names tied for most frequent, joined in alphabetical order
  (e.g., `BaseHead` for `head` and `base`).

Each shared type gets a unique name.  When different types derive the same
name, they're ranked by how many times they appear (and then by the order they
first appear), the first gets the name, and the others are versioned with the
suffixes `_v2`, `_v3`, and so on (e.g., `User` and `User_v2`).  A name that's
already bound to a different type in the query is skipped in the same way,
while one bound to the same type is reused.

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

_Name types shared by fields with different names and version name collisions_

```mdtest-spq
# spq
values dedup_type(this)
# input
map{
  "Order":<{ship_address:{street:string,zip:string},user:{id:int64}}>,
  "Cart":<{user:{id:int64}}>,
  "Account":<{home_address:{street:string,zip:string},user:{id:int64,email:string}}>,
  "Profile":<{user:{id:int64,email:string},admin:{id:int64,email:string}}>
}
# expected output
type User_v2={id:int64}
type Address={street:string,zip:string}
type User={id:int64,email:string}
map{"Cart":<{user:User_v2}>,"Order":<{ship_address:Address,user:User_v2}>,"Account":<{home_address:Address,user:User}>,"Profile":<{user:User,admin:User}>}
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
