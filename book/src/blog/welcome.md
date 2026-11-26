# Welcome to SuperDB

_November 2026_

SuperDB didn't start out as a database project.  We set out to build a
search and analytics stack for security data, the kind of data that comes
from everywhere at once: network monitors, endpoint agents, cloud audit
logs, and identity systems, each with its own format and its own idea of
what an event looks like.  Before we could search any of it, we had to get
all of it into one place, and that turned out to be the hard part.

The more we looked at how this was done, the more dumbfounded we were by
the state of data engineering.  There seemed to be two ways to land data
and neither one was good.

The easy way was to turn everything into JSON.  JSON takes anything, which
is its appeal, but much of our data was richly typed to begin with.
A [Zeek](https://zeek.org/) log, for example, knows an IP address from a
string, a timestamp from a number, and a set from a list, and once it's
serialized as JSON, all of that is gone.  Addresses and times become
strings, every number becomes the same kind of number, and the reader is
left to guess what the writer knew.

The careful way was to figure out a schema for everything and land the data
in tables.  Tables keep the types, but then somebody has to define a schema
for every source up front and keep up as fields come and go and new kinds
of events appear.  And whatever doesn't fit gets dropped, coerced, or
stuffed into a JSON column, which lands you right back where you started.

Throwing away the types or doing the schema work before you've even seen
the data seemed like a strange choice to have to make, and we kept thinking
there ought to be a better way.  Eventually that question took over, and we
pivoted from security to work on the data problem itself.

What followed was a multi-year research project.  We wanted data to keep
its precise types without anyone having to declare a schema in advance, and
we wanted a single system that treats tables and JSON alike rather than
bolting one onto the other.  Where we ended up is the
[algebraic data model](../adm.md) (ADM), which borrows the algebraic types
of modern programming languages and turns out to contain both relational
tables and JSON as special cases.

A better model isn't much use, though, if you have to start over to adopt
it.  The world's data lives in JSON files and SQL tables, and it isn't
going anywhere.  The piece that finally brought it all together, and the
one we rallied around, is [type fusion](../super-sql/type-fusion.md).
Fusion takes values of many different types and combines their types into
a single type that describes all of them, while keeping track of the
original type of each value.  That makes it the bridge from messy JSON to
the ADM: you load your data as it is, with no schema, and SuperDB computes
a precise type that a type checker and a query engine can work with.
And since a relational table is just a special case of the model, your SQL
keeps working.  The ADM can be adopted a little at a time, in a way that
fits the world as we know it.

So now we're making the turn from research to production.  Over the coming
quarters, we'll be building production-quality infrastructure on the ADM,
and we'll use this blog to explain the ideas along the way.  The first
posts start with a day of real-world data in
[The Sum Type Hiding in Your JSON](gharchive.md), look at what JSON throws
away in [JSON Can't Tell a Map from a Record](json-map-or-record.md),
show where this is all headed in
[Algebraic Types, End to End](end-to-end.md), and explain the operator you'll
see in nearly every query in [Values, Not Columns](values.md).

We're optimistic about where this leads.  Programming languages worked out
years ago how to describe data that varies in shape, and there's no reason
a database can't speak the same language.  We can picture data keeping its
types all the way from the program that creates it to the query that reads
it, with no schema to renegotiate at every hop, and people and AI agents
alike getting a real error when a query is wrong instead of a quiet wrong
answer.  We've wanted that system since the day we first tried to land a
pile of security logs, and it finally feels within reach.

Stay tuned.
