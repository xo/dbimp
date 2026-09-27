# D3. The repository uses the xo layout

Status: Amended by D71 and D72.

This matches `dbmeta` and `cql`:

- The root holds `README.md`, `CLAUDE.md` and `CONTRIBUTING.md`, and no other
  Markdown. Every other document is in `docs/`, and each one is named in the
  table in `CLAUDE.md` and in the table in `README.md`.
- `CLAUDE.md` follows the order that `dbmeta/CLAUDE.md` uses: purpose, a
  "Which document to read" table, numbered hard rules with their reasons,
  layout, Go conventions, linting, the commands to run before committing, and
  how to write documentation. It also carries the table of sibling
  repositories from `usql/CLAUDE.md`.
- `CONTRIBUTING.md` holds the same material for a person, and it is shorter.
  No repository has an `AGENTS.md`, and this one has none.
- `docs/PLAN.md` holds decisions and open questions only.
- `docs/BACKLOG.md` holds work items, in the format that
  `usql/docs/BACKLOG.md` uses.
- Examples are `Example` tests. Golden files go in `testdata/`.
- CI is one GitHub Actions workflow, `.github/workflows/test.yml`, on
  `ubuntu-latest`, with `go-version-file: go.mod`. There is no Makefile and no
  matrix of operating systems.
- There is one `.gitignore`, at the root. `.gitattributes` sets
  `* text=auto eol=lf`, as in `dbmeta` and `cql`.

`docs_test.go` holds these rules: `TestTheRootHoldsFourDocuments`, which
held three documents until D71,
`TestEveryDocumentIsInTheTable`, `TestEveryLinkResolves`,
`TestEveryDecisionReferenceExists`, `TestTheDecisionIndexIsComplete` and
`TestEveryTestNameInTheDocsExists`.
