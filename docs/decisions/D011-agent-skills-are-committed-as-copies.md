# D11. Agent skills are committed as copies

Status: Amended by D71.

The repository carries two agent skills, `simple-english` and `go-pedantry`,
copied from `dbmeta`. Each is an ordinary folder under `.agents/skills` and
under `.claude/skills`, and `skills-lock.json` names its source.

A symbolic link is refused, because a Windows checkout writes a link as a text
file, and Claude Code then loads no skill and reports nothing.
`TestSkillsAreCopies` fails on a link, and it fails when the two folders
differ. This follows dbmeta D89 and `cql` D15.

The first copies in this repository were symbolic links, made before this
file existed. They were replaced with copies on 2026-09-27.
