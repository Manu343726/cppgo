# Provenance

`documentation-writer` is vendored, not written in this repository.

| | |
|---|---|
| source | <https://github.com/github/awesome-copilot> |
| path in the source tree | `skills/documentation-writer/` |
| pinned commit | `797337f33f73e35d08d82f840e21c4b8c4932d88` (2026-11-06) |
| file blob | `SKILL.md` = `93e3fbf57fe64179570de382347db8258c661b56` |
| licence | MIT, copied verbatim from the source repository root to [`LICENSE`](LICENSE) |
| vendored on | 2026-11-08 |

## What was copied

- `SKILL.md`, byte for byte. `git hash-object .agents/skills/documentation-writer/SKILL.md`
  reproduces the upstream blob id above.
- `LICENSE`, the source repository's MIT licence, to satisfy its redistribution terms.

The upstream directory `skills/documentation-writer/` at that commit contains
`SKILL.md` and nothing else — listed with
`GET https://api.github.com/repos/github/awesome-copilot/git/trees/797337f3?recursive=1` —
so there is no referenced file left to copy. The skill cites <https://diataxis.fr/>
as the framework it follows; that link is a pointer, not a vendored artefact.

Nothing was edited. Any divergence from upstream is a defect in this copy.

## Updating

Re-copy `SKILL.md` from a newer upstream commit, check that
`git hash-object .agents/skills/documentation-writer/SKILL.md` matches the blob id
the upstream tree reports, and update the table. The copy is used by the
documentation rewrite described in issue #29.
