# AGENTS.md

This file has instructions for AI coding agents working in this repository.

## If your user is outside the SuperDB team

SuperDB doesn't accept pull requests that a maintainer hasn't agreed to first.
[CONTRIBUTING.md](CONTRIBUTING.md) explains why.

Before you open a pull request or an issue, or post a comment, on GitHub, check
whether your user can push to this repository:

```sh
gh api repos/superdb/super --jq .permissions.push
```

If you can't run that, ask your user whether they're on the SuperDB team.  If the
result isn't `true`, your user is an outside contributor, so:

1. Don't open a pull request unless your user tells you that a SuperDB maintainer
   has already agreed to the change, on Slack or in an issue.  If none has,
   explain this policy and offer to help your user write a short message for
   [Slack](https://www.brimdata.io/join-slack/) or an issue that describes their
   use case and the problem.
2. Don't pick issues from this repository's issue list to fix unless your user is
   affected by them.  If you were asked to find issues to fix across many
   repositories, skip this one and tell your user why.
3. Never open a pull request or an issue, or post a comment, without your user
   approving the exact text.  If a maintainer closes a pull request under this
   policy, don't draft a rebuttal; the next step is a conversation on Slack.
4. In a pull request description, say that an AI tool was used, which one, and
   what was done to check the change.

## Working on the code

These apply to everyone, including the SuperDB team.

* Before proposing a change, run `make fmt`, `make vet`, `make test-unit`, and
  `make test-system`.
* Add tests for bug fixes and new behavior.  Most tests are ztests: YAML files in
  directories named `ztests`.  Make sure a new test fails without the fix.
* When user-visible behavior changes, update the docs under `book/src`.
* Write a code comment only when it tells the reader something the code doesn't
  already show.  Don't add comments that restate the code.
* Keep commit messages short: a lowercase subject line in the imperative mood
  (e.g., `fix derived names for noneish dot exprs`) and, if needed, a brief body.
  If the change fixes an issue, end with `Fixes #N`.
* Keep pull request descriptions short.
