# D71. Every xo repository is set up for coding agents the same way

Status: Amends D3 and D11.

Ken decided on 2026-09-27 that every repository in the `xo` namespace is set
up for coding agents the same way, and that dbimp follows the layout of
`dbmeta`. dbmeta D110 is the standard, and this decision adopts it here.

- `AGENTS.md` holds the rules for a coding agent, because Codex and the other
  agents read that file. The rules moved there from `CLAUDE.md`.
- `CLAUDE.md` holds one line, `@AGENTS.md`, which Claude Code imports, so that
  every agent reads the same rules. It is a file and not a symbolic link,
  because a Windows checkout writes a link as a small text file (D11).
  `TestClaudeImportsAgents` holds it.
- `AGENTS.md` opens with the standing rules of every `xo` repository: stage a
  change for review and commit only when Ken says so, load `simple-english`
  before writing text that a person reads, and load `go-pedantry` before
  writing or reviewing Go code. A rule of this repository wins where
  `go-pedantry` disagrees.
- The root holds four documents: `README.md`, `AGENTS.md`, `CLAUDE.md` and
  `CONTRIBUTING.md`. D3 said that the repository has no `AGENTS.md`, so this
  amends D3. `TestTheRootHoldsFourDocuments` holds it.
- `.gitignore` ignores `.claude/settings.local.json`, which holds the
  permissions of one person for Claude Code. `.gitattributes` already holds
  `* text=auto eol=lf` (D3).

The skills of D11 do not change. The standing rule that names `go-pedantry`
amends D11, which said only that the skills are committed.
