# Enums

The `enum` type represents a set of symbols, e.g., to represent
categories by name.
It conforms to the definition of the
[enum type](../../formats/model.md#26-enum)
in the super-structured data model and follows the
[syntax](../../formats/sup.md#256-enum-type)
of enums in the [SUP format](../../formats/sup.md), i.e.,
an enum type has the form
```
enum ( <name>, <name>, ... )
```
where `<name>` is an identifier or string.

For example, this is a simple enum type:
```
enum(hearts,diamonds,spades,clubs)
```
and this is a value of that type:
```
'hearts'::enum(hearts,diamonds,spades,clubs)
```

Enum serialization in the SUP format is fairly verbose as the set of
symbols must be enumerated anywhere the type appears.  In the binary
BSUP format, the enum symbols are encoded efficiently just once.

## Examples

---
```mdtest-spq {data-layout="stacked"}
# spq
const suit = <enum(hearts,diamonds,spades,clubs)>
values f"The value {this} {is(this, suit)? 'is' : 'is not'} a suit enum"
# input
"hearts"
"diamonds"::enum(hearts,diamonds,spades,clubs)
# expected output
"The value hearts is not a suit enum"
"The value diamonds is a suit enum"
```
