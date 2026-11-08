# Provenance

`golang-documentation` is vendored, not written in this repository.

| | |
|---|---|
| source | <https://github.com/samber/cc-skills-golang> |
| path in the source tree | `skills/golang-documentation/` |
| pinned commit | `bb1d43d98a3f8ab639a0042f90d0ec524e39a477` (2026-11-01) |
| upstream version | 1.3.2 (`metadata.version` in the `SKILL.md` frontmatter) |
| licence | MIT, copied verbatim from the source repository root to [`LICENSE`](LICENSE) |
| vendored on | 2026-11-08 |

## What was copied

Every file the skill links to, byte for byte. `git hash-object` on each one
reproduces the blob id the upstream tree reports at the pinned commit:

| file | upstream blob |
|---|---|
| `SKILL.md` | `565413a9f17b41d8e4429c68ccd6bff31b0285e7` |
| `references/library.md` | `ccb2cee9ad74c1e2f8311fc2cd820f0d7e4ff82c` |
| `references/application.md` | `31d91e60f404f375c0092c02969794c085461e6a` |
| `references/code-comments.md` | `0fc2105d4583c34dc05daf05398943e475db0dc2` |
| `references/project-docs.md` | `69fcf6624ee07dcbd1cf3e41f18c19047967c948` |
| `assets/templates/README.md` | `996ea470cf755cc4eb913ed5a040e7af61a3708c` |
| `assets/templates/CHANGELOG.md` | `2d7d2418535209389d7fcbed5e069489a7dd239b` |
| `assets/templates/CONTRIBUTING.md` | `2af154a1bec1673a0f49bee0c6c28a99e532fc6f` |
| `assets/templates/llms.txt` | `147780ea638e1db5f834cf9bbf100d608efbce66` |

The upstream directory also holds `evals/evals.json`, a test fixture for the
skill's own evaluation harness. Nothing in the skill links to it, so it is not
vendored. The cross-references to the sibling skills `golang-naming` and
`golang-testing` are not vendored either: they are separate skills from the
same collection and are only reachable where that whole collection is
installed.

Nothing was edited. Any divergence from upstream is a defect in this copy.

## Using it

This skill is the reference for Go doc comments in this repository; it replaced
`.agents/skills/technical-prose-style/`, deleted in issue #29. The existing
comments under `internal/`, `cmd/` and the root package are rewritten in a
follow-up workstream, not here.

## Updating

Re-copy the files from a newer upstream commit, check every `git hash-object`
against the ids the upstream tree reports, and update the table. An update that
changes what a doc comment may look like belongs in its own commit, separate
from any comment rewrite it triggers.
