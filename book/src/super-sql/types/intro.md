# Types

SuperSQL has a comprehensive types system that adheres to the
[super-structured data model](../../formats/model.md)
comprising
[primitive types](../../formats/model.md#1-primitive-types),
[complex types](../../formats/model.md#2-complex-types),
[sum types](union.md),
[named types](named.md),
the [null type](null.md),
the [none and missing types](none.md),
and first-class
[errors](error.md) and [types](type.md).

The syntax of individual literal values as well as types follows
the [SUP format](../../formats/sup.md) in that any legal
SUP value is also a valid SuperSQL literal.

Likewise, any SUP type is also valid type syntax, which may be used
in [cast](../expressions/cast.md) expressions or
[type declarations](../declarations/types.md).

Note that the type decorators in SUP utilize a double colon (`::`)
syntax that is compatible with [cast](../expressions/cast.md) expressions.

Arguments to [functions](../functions/intro.md)
and [operators](../operators/intro.md) are all dynamically typed,
yet certain functions expect certain specific types
or classes of data types.  The following names for these categories of types
are used in throughout the documentation:
* `any` - any SuperSQL data type
* `float` - any floating point type
* `int` - any signed or unsigned integer type
* `number` - either `float` or `int`
* `record` - any [record type](record.md)
* `set` - any [set type](set.md)
* `map` - any [map type](map.md)
* `function` - a function reference of lambda expression

To be clear, none of these categorical names are actual types and may not
be used in a SuperSQL query.  They are simply used to document expected
type categories.

>[!NOTE]
> In a future version of SuperSQL, user-defined function and operator declarations
> will include optional type signatures and these names representing type categories
> may be included in the language for that purpose.
