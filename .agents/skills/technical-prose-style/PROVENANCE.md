# Provenance

`technical-prose-style` is vendored, not written in this repository.

| | |
|---|---|
| source | <https://github.com/KensioSoftware/kensio.ai> |
| path in the source tree | `plugins/technical-prose-style/skills/technical-prose-style/` |
| pinned commit | `e86fd6a4ff9ad8fc6576348f00c2140c3536744c` |
| upstream version | 1.18.1 (`.claude-plugin/plugin.json`) |
| licence | Apache-2.0, copied verbatim to `LICENSE` |
| vendored on | 2026-11-08 |

## What was copied

- `SKILL.md`
- `references/measurements.md` (the upstream directory is `references/`; the
  skill's own link is relative to it)
- `scripts/prose-check.mjs`

Nothing was edited. The checker is run with Node, and has no npm or network
dependency: `node .agents/skills/technical-prose-style/scripts/prose-check.mjs
<path>` is the whole interface. See
[`SKILL.md`](SKILL.md) for the audit pass and the thresholds.

## Updating

Re-copy the three files from a newer upstream commit and update the table. The
project's `make prose-check` target runs the vendored copy, so an update changes
the gate and belongs in its own commit with the new findings.
