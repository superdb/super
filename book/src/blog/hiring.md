# SuperDB Is Hiring

_November 2026_

The relational model has had a remarkable fifty-year run.  But we think
it's time for it to become a special case of something bigger, and we're
hiring people to help make that happen.

## What We're Building

SuperDB is a new approach to databases.  It's built on the
[algebraic data model](../adm.md) (ADM), where relational tables and JSON
are both special cases of one type system, and on
[SuperSQL](../super-sql/intro.md), a pipe SQL for algebraic data that type
checks your queries even when nobody wrote a schema.

If that's new to you, [Welcome to SuperDB](welcome.md) has the back story,
[The Sum Type Hiding in Your JSON](gharchive.md) shows it at work on 18GB of
real-world data, and [Algebraic Types, End to End](end-to-end.md) shows
where it's headed.

## Who We're Looking For

We're looking for expert-level database developers with Rust experience.

The prototype is built in Go.  It's where we worked out the data model,
the type checker, and the query engine over several years of research.
The production system is being built in Rust, and that's the job.
If you've built a query engine, a storage layer, or a columnar format,
and you have strong opinions about how it should have been done, we'd like
to hear them.

## Why Now

Plenty of people have taken on the relational model, and
[the battlefield is littered with the corpses](../adm.md#failed-attempts-to-replace-the-rm).
The document database crowd brought a knife to a gunfight.  They traded away
types to get flexibility, and SQL simply absorbed JSON as a column type and
carried on.

We're not bringing a knife.  The ADM is a type system big enough to hold the
relational model and the document model at the same time, so SQL and tables
keep working while everything that never fit in a table finally gets a real
type.

## Get in Touch

If you're not afraid to take on the relational data model (and the document
data model while we're at it), join our
[Slack](https://www.brimdata.io/join-slack/) and drop me a DM.

Steve McCanne<br>
Coding CEO
