# Contributing

Thank you for your interest in SuperDB!

SuperDB is early in its life cycle and will be expanding quickly.  Please star and/or
watch the repo so you can follow and track our progress.

## Talk to us first

The best way to start is a conversation.  Come talk to us on
[Slack](https://www.brimdata.io/join-slack/) about what you're using SuperDB for
and what's getting in your way.  [Issues](https://github.com/superdb/super/issues)
are also welcome for bug reports and feature requests.  Either way, the most
helpful thing you can tell us is the real-world use case behind your request.

## Pull requests

Please don't open a pull request until a maintainer has agreed to the change and
your approach, on Slack or in an issue.

Writing the code for a fix is rarely the hard part for us.  The hard part is
deciding what the right fix is, making sure it fits the design of the language and
the rest of the system, and reviewing it.  A pull request we haven't discussed
adds review work without saving us that effort.

Pull requests that weren't discussed first may be closed without review.  This isn't
a judgment of you or your code.  If you have a real need behind the change, tell us
about it on Slack and we'll go from there.  We may also fix an issue ourselves even
when a pull request for it exists.

## Using AI tools

You're welcome to use AI tools.  We use them too.  If you do:

* Say so in the pull request description, along with which tools you used and how
  you checked the result (for example, the tests you ran and any reviews you did).
* Make sure you understand the change and can answer questions about it yourself.
* Write issues, pull request descriptions, and comments yourself, or edit what a
  tool wrote so it's short and accurate.
* Don't let an agent open issues or pull requests, or post comments, without a
  person approving each one.
* Don't point an agent at our issue list, or at many projects at once, to generate
  pull requests for problems that don't affect you.

If you use an AI coding agent, it should follow [AGENTS.md](AGENTS.md).

## Development

Compiling `super` requires Go 1.27 or later and uses [Go modules](https://github.com/golang/go/wiki/Modules).
Compilation for 32-bit target environments is not currently supported
(see [super/4044](https://github.com/superdb/super/issues/4044)).
Dependencies are specified in the [`go.mod` file](./go.mod) and fetched
automatically by commands like `go build` and `go test`.  No explicit
fetch commands are necessary.  However, you must set the environment
variable `GO111MODULE=on` if your repo is at
`$GOPATH/src/github.com/superdb/super`.

When `go.mod` or its companion `go.sum` are modified during development, run
`go mod tidy` and then commit the changes to both files.

To use a local checkout of a dependency, use `go mod edit`:
```
go mod edit -replace=github.com/org/repo=../repo
```

### Testing

Before any PRs are merged to main, all tests must pass.

To run unit tests, execute:
```
make test-unit
```

System tests require Python 3.3 or better and the
[AWS CLI](https://aws.amazon.com/cli/).  To run them, execute:

```
make test-system
```
