# The Sum Type Hiding in Your JSON

_November 2026_

A few years ago, the DuckDB team published
[Shredding Deeply Nested JSON, One Vector at a Time](https://duckdb.org/2023/03/03/json.html),
a tour of querying a day of [GitHub Archive](https://www.gharchive.org/)
events straight from JSON files.  It's a great post and a great data set,
because GitHub events are about as messy as real-world JSON gets.

We're going to take the same data and the same queries and look at them
through the lens of the [algebraic data model](../adm.md) (ADM).  If you
want the background on the data, their post has it.  We'll dive right in.

## The Trouble with `payload`

Every GitHub event has the same handful of top-level fields: an `id`, a
`type`, an `actor`, a `repo`, a `created_at` time, and a `payload`.  All of
the trouble is in `payload`, whose shape depends on the `type`.
A `WatchEvent` payload has a single field.  A `PullRequestEvent` payload
has hundreds.

The DuckDB post handles this in three ways.  First it leaves `payload`
out of the table altogether.  Then it writes out a `STRUCT` type by hand
for the part of one event type's payload that it cares about.  And then it
reads `payload` as a `JSON` column and parses it at query time.

If you've read the [ADM section](../adm.md), this will look familiar.
It's the shapes example at scale: a `type` field says which kind of thing a
value is and another field varies along with it.  In other words, a GitHub
event is a _sum type_ with one variant for each kind of event, written down
as JSON, where the sum type's tag had to become a string.  A data model
with only product types has no good place to put that, which is why
`payload` gets left out, hand-typed one variant at a time, or kept as JSON.

So let's see what happens when the data model has sum types.

## Fetching the Data

The data is 24 hourly files for February 8, 2023, about 2.3GB of gzipped
JSON holding 4,434,953 events:
```
mkdir gharchive_gz
cd gharchive_gz
for h in $(seq 0 23); do
  wget https://data.gharchive.org/2023-02-08-$h.json.gz
done
cd ..
```
To load it, we convert it to [BSUP](../formats/bsup.md):
```
super -o gha.bsup gharchive_gz/*.json.gz
```
That's the whole import.  There is no schema to write, no sample size to
tune, and no `payload` to leave behind.  Every event is stored with its
own precise type, and we can ask how many different types there are:
```
super -s -c "by typeof(this) | count()" gha.bsup
```
```
22845
```
That's 22,845 types for 15 kinds of events.  An optional field here and
an empty array there, and nearly every combination shows up somewhere.

## Fusing the Data

To check a query, the compiler needs one type that describes all of this
data, and [type fusion](../super-sql/type-fusion.md) computes it.  SuperDB
does this for you under the covers, but you can ask for the fused type
yourself.  It's big:
```
super -S -c "aggregate fuse(this)" gha.bsup | wc -l
```
```
    1253
```
This is SuperDB's version of schema inference, with one difference that
matters: The fused type is something the compiler computes to check
your queries.  The data doesn't have to be rewritten to fit it.

## The Same Queries

Now we can run the queries from the DuckDB post, and since SuperSQL is
(aspirationally) compatible with SQL, we can run them more or less as
written.  Here are the events by type:
```
super -s -c "
  SELECT type, count(*) AS count
  FROM 'gha.bsup'
  GROUP BY type
  ORDER BY count DESC"
```
```
{type:"PushEvent",count:2359096}
{type:"CreateEvent",count:624062}
{type:"PullRequestEvent",count:366090}
{type:"IssueCommentEvent",count:238660}
{type:"WatchEvent",count:231486}
{type:"DeleteEvent",count:154383}
{type:"PullRequestReviewEvent",count:131107}
{type:"IssuesEvent",count:88917}
{type:"PullRequestReviewCommentEvent",count:79540}
{type:"ForkEvent",count:64233}
{type:"CommitCommentEvent",count:36823}
{type:"ReleaseEvent",count:23004}
{type:"MemberEvent",count:14872}
{type:"PublicEvent",count:14500}
{type:"GollumEvent",count:8180}
```
the activity on the `duckdb/duckdb` repo:
```
super -s -c "
  SELECT type, count(*) AS count
  FROM 'gha.bsup'
  WHERE repo.name = 'duckdb/duckdb'
  GROUP BY type
  ORDER BY count DESC"
```
```
{type:"PullRequestEvent",count:35}
{type:"IssueCommentEvent",count:30}
{type:"WatchEvent",count:29}
{type:"PushEvent",count:15}
{type:"PullRequestReviewEvent",count:14}
{type:"IssuesEvent",count:9}
{type:"PullRequestReviewCommentEvent",count:7}
{type:"ForkEvent",count:3}
```
and the people most active on its pull requests:
```
super -s -c "
  SELECT actor.login, count(*) AS count
  FROM 'gha.bsup'
  WHERE repo.name = 'duckdb/duckdb'
    AND type = 'PullRequestEvent'
  GROUP BY actor.login
  ORDER BY count DESC
  LIMIT 5"
```
```
{login:"Mytherin",count:19}
{login:"Mause",count:4}
{login:"carlopi",count:3}
{login:"Tmonster",count:2}
{login:"lnkuiper",count:2}
```
The answers match the DuckDB post, and we never excluded `payload` to get
them.

The payload queries are where the DuckDB post had to stop and write out a
`STRUCT` type.  Here there is nothing to do.  The largest number of
assignees on a pull request is
```
super -s -c "
  SELECT max(len(payload.pull_request.assignees)) AS max_assignees
  FROM 'gha.bsup'
  WHERE type = 'PullRequestEvent'"
```
```
{max_assignees:10}
```
which is the answer they got, and we got it without describing `payload`
to anyone.  And if we misspell a field, the query fails before any data
is read instead of returning a column full of `NULL`:
```
super -s -c "
  SELECT max(len(payload.pull_request.asignees)) AS max_assignees
  FROM 'gha.bsup'
  WHERE type = 'PullRequestEvent'"
```
```
no such field "asignees" at line 2, column 39:
  SELECT max(len(payload.pull_request.asignees)) AS max_assignees
                                      ~~~~~~~~
```

## Improved Schema Inference

To be fair to DuckDB, a lot has changed since that post.  Its schema
inference now handles all of this data.  With `union_by_name`, DuckDB reads
every file and combines every shape it finds, `payload` included, into a
single table:
```
CREATE TABLE gha AS
  FROM read_json('gharchive_gz/*.json.gz', union_by_name=true);
```
There's no need to leave anything behind as `JSON`, and with real `STRUCT`
columns, DuckDB also catches a misspelled field.  This is all excellent
engineering if the outcome must be constrained to the relational model.

But look at what the table is.  It's one enormous product type where every
event has every field of every kind of payload, and the fields that
don't apply are `NULL`.  This is the _wide table_ workaround from the
[ADM section](../adm.md#the-frankentype).  The schema can't
say that a `PushEvent` has commits and a `WatchEvent` doesn't, and a `NULL`
can't say whether a field was empty or was never there.

The fused type above has the same weakness.  It says that a `payload` might
have a `pull_request`, but it doesn't say _which events_ have one.  We know
more than that.  We know these are 15 kinds of events.

## Shaping the Data into a Sum Type

So let's tell the data model what we know.  And rather than write out 15
event types by hand, each with hundreds of fields, we'll compute them:
```
super -o events.bsup -c "
  type Event = (
    from gha.bsup
    | fuse(this) by type
    | collect_map(map{type:optionize(fuse)})
    | sum_type(this)
  )
  from gha.bsup | values this::Event"
```
The subquery fuses the events of each `type` separately, makes a
[named type](../super-sql/types/named.md) from each result, using the
value of the `type` field as its name, and forms the
[sum type](../super-sql/functions/types/sum_type.md) of all of them.  That
type is called `Event`, and the last line casts every event to it.
(If the `values` operator is new to you, see
[Values, Not Columns](values.md).)

Now count the types again:
```
super -s -c "by typeof(this) | count()" gha.bsup
super -s -c "by typeof(this) | count()" events.bsup
```
```
22845
1
```
The 22,845 types have become one type with 15 variants, few enough that
we can list them:
```
super -s -c "count() by variant:=nameof(under(this)) | sort -r count" events.bsup
```
```
{variant:"PushEvent",count:2359096}
{variant:"CreateEvent",count:624062}
{variant:"PullRequestEvent",count:366090}
{variant:"IssueCommentEvent",count:238660}
{variant:"WatchEvent",count:231486}
{variant:"DeleteEvent",count:154383}
{variant:"PullRequestReviewEvent",count:131107}
{variant:"IssuesEvent",count:88917}
{variant:"PullRequestReviewCommentEvent",count:79540}
{variant:"ForkEvent",count:64233}
{variant:"CommitCommentEvent",count:36823}
{variant:"ReleaseEvent",count:23004}
{variant:"MemberEvent",count:14872}
{variant:"PublicEvent",count:14500}
{variant:"GollumEvent",count:8180}
```
Each event now says what kind of event it is in its type, and we can pick
out a kind of event with a
[type assertion](../super-sql/expressions/type-assertions.md):
```
super -s -c "values this.(PushEvent) | count()" events.bsup
```
```
2359096
```
That's the `PushEvent` count from above.  Note that we never wrote down the
`PushEvent` type.  It was computed from the data, and it's stored in
`events.bsup` along with the data, so the query above works without
declaring anything.

The type checker knows what each kind of event has, too.  A push
doesn't have a pull request, and asking for one is now an error:
```
super -s -c "values this.(PushEvent) | values payload.pull_request | head 1" events.bsup
```
```
no such field "pull_request" at line 1, column 42:
values this.(PushEvent) | values payload.pull_request | head 1
                                         ~~~~~~~~~~~~
```
With the fused type, the checker could only know that some event somewhere
has a `pull_request`.  With the sum type, it knows which ones.

Now go back to the payload queries.  With a type assertion in place of
the `WHERE` clause, a query says what it means: take the pull request
events, then look inside them.
```
super -s -c "
  values this.(PullRequestEvent)
  | aggregate max_assignees:=max(len(payload.pull_request.assignees))" events.bsup
```
```
{max_assignees:10}
```
The same goes for counting the actions taken on pull requests, which the
DuckDB post did by digging into a `JSON` column:
```
super -s -c "
  values this.(PullRequestEvent)
  | count() by action:=payload.action
  | sort -r count" events.bsup
```
```
{action:"opened",count:189096}
{action:"closed",count:174914}
{action:"reopened",count:2080}
```
We can also find who gets assigned the most pull requests.  A pull request
has both a single `assignee` and a list of `assignees`, and the DuckDB post
combines the two with a `UNION ALL`.  Here an array expression with a spread
does the same job, and [unnest](../super-sql/operators/unnest.md) turns the
array into a sequence of values:
```
super -s -c "
  values this.(PullRequestEvent)
  | unnest [payload.pull_request.assignee, ...payload.pull_request.assignees]
  | count() by login:=this?.login
  | where login.is_ok()
  | sort -r count
  | head 5" events.bsup
```
```
{login:"poad",count:494}
{login:"vinayakkulkarni",count:268}
{login:"tmtmtmtm",count:198}
{login:"fisker",count:98}
{login:"icemac",count:84}
```
These are the same five people and the same counts as in the DuckDB post.
A pull request with nobody assigned has no `assignee`, so `?.` yields
nothing for its login, and the `where` clause drops that group.

The fields that all events share are still right there, with no assertion
needed:
```
super -s -c "
  where repo.name='duckdb/duckdb'
  | count() by type
  | sort -r count" events.bsup
```
```
{type:"PullRequestEvent",count:35}
{type:"IssueCommentEvent",count:30}
{type:"WatchEvent",count:29}
{type:"PushEvent",count:15}
{type:"PullRequestReviewEvent",count:14}
{type:"IssuesEvent",count:9}
{type:"PullRequestReviewCommentEvent",count:7}
{type:"ForkEvent",count:3}
```

## From 22,845 Types to One

We started with 18GB of JSON whose structure nobody wrote down.  We ended
with a file of typed events where every value belongs to one of 15 named
types, the types came from the data, and the queries are checked against
them.  At no point did we write a schema, leave out a column, or fall back
to `JSON`.

This is what the ADM buys you.  A relational system, even a very good one,
can only describe this data as a single wide row.  A sum type describes it
as what it is, and the type checker can then tell a push from a pull request.

Try it on your own messy JSON, and stay tuned.
