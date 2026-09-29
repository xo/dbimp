# D72. The decisions are one file each

Status: Amends D3.

Ken decided on 2026-09-27 that a large project keeps each decision in a file
of its own, and dbmeta D111 names dbimp as one. `docs/PLAN.md` held every
decision and the open questions in one file, which D3 decided, so this
amends D3.

Each decision is `docs/decisions/D<nnn>-<title>.md`, with the number in three
digits, so that a listing sorts in order. The file opens with its number and
title, a blank line, and its status, such as `Status: Decided.`
[README.md](README.md) in that folder is the index. `docs/PLAN.md` keeps the
purpose of the project and the open questions.

A reference by number, such as D47, still works, because it names the number
and not a place in a file. `TestEveryDecisionReferenceExists` reads the
folder. `TestTheDecisionIndexIsComplete` checks each row of the index
against its file, the title and the status alike, and prints the row to add.
`TestAnAmendmentPointsBothWays` fails unless a decision that amends another
and the decision that it amends name each other in their status.

The move found that the headings in `docs/PLAN.md` said only `Decided` or
`Proposed`, and each amendment was written in the text of the decisions. The
status of each decision that amends another, or that another amends, now
names the other one. D53 names D70, which proposes an amendment and waits
for Ken.

A script cut `docs/PLAN.md` at each heading of a decision, moved each one to
its file, raised each heading inside it by two levels, and made each
relative link one folder deeper. The words of the old decisions and the new
files were counted, and the only words that changed are those of each
status.

Note of 2026-09-29: Ken accepted D70 on 2026-09-29.
