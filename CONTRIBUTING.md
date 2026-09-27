# Contributing to dbimp

`dbimp` holds `database/sql` drivers for databases that have no idiomatic Go
driver. Read two documents before you change anything:

- [`docs/decisions/`](docs/decisions/README.md) holds every decision, one
  file each, with an index. [`docs/PLAN.md`](docs/PLAN.md) holds the open
  questions. Do not decide an open question yourself. Ask Ken.
- [`docs/BACKLOG.md`](docs/BACKLOG.md) holds the planned work, in order.

[`AGENTS.md`](AGENTS.md) holds the rules. It is written for a coding agent,
and everything in it applies to a person. [`CLAUDE.md`](CLAUDE.md) holds one
line that imports it for Claude Code (D71).

## Before you send a change

Run these in the repository root. `gofmt -l .` must print nothing.

```bash
gofmt -l . && go vet ./... && go build ./... && go test -race -count=2 ./...
golangci-lint run ./...
```

The unit tests need no server. The integration tests of a driver run against
the server that its `<DRIVER>_DSN` variable names, and they skip when it is
empty. Start the server with `dbrun` from
[`dbmeta`](https://github.com/xo/dbmeta), in a checkout next to this one. Do
not start a container by hand (D9).

```bash
(cd ../dbmeta/test && go run ./cmd/dbrun start <release>)
export <DRIVER>_DSN=$(cd ../dbmeta/test && go run ./cmd/dbrun dsn --json <release> | jq -r '.[0].url')
go test -race -count=1 -run Integration ./<driver>/...
```

## Writing text

Load the `simple-english` skill before you write any text that a user can
read. That includes `README.md` and every other document, code comments,
error messages, commit messages and the text of test failures.
Follow it for that text. See D12 in `docs/decisions/`.

## Agent skills

The repository carries two agent skills. A skill is a set of instructions
that a coding agent loads for a task. `simple-english` sets how prose is
written, and `go-pedantry` sets how Go is written.

`skills-lock.json` names the source of each skill. The `skills` command from
npm writes that file. Version 1.7.0 is the version that `dbmeta` measured.
The command writes each skill into two folders. Codex and the other agents
read `.agents/skills/<name>`, and Claude Code reads `.claude/skills/<name>`.

To add or update a skill, run this command in the repository root. The
example updates `simple-english`. `skills-lock.json` holds the source for
each skill:

```bash
npx skills@1.7.0 add AminBlg/SimpleEnglish --skill simple-english --agent codex claude-code --copy -y
```

Keep `--copy`. Without it, the command writes `.claude/skills/<name>` as a
symbolic link. A Windows checkout writes a symbolic link as a text file, and
Claude Code then loads no skill and reports nothing. `TestSkillsAreCopies`
fails on a link, and it fails when the two folders differ. See D11 in
`docs/decisions/`.

`.claude/settings.local.json` holds the Claude Code permissions of one
person. The root `.gitignore` ignores it.
