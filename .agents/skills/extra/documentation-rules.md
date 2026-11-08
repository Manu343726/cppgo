# Documentation rules

These rules extend the documentation skills in this repository —
[`documentation-writer`](../documentation-writer/SKILL.md) and
[`golang-documentation`](../golang-documentation/SKILL.md) — and **win over
their text** wherever the two disagree. Read this file before writing or
reviewing documentation.

The operator appends rules here. Number them, and keep each one short enough to
apply without interpretation. A rule that needs examples gets them under the
rule, not in a separate page.

## Rule 1 — Documentation says what a feature does, not how it does it

The documentation must not mention implementation details, except in sections
that explicitly discuss how the functionality works. Documentation is for users:
it is about what the feature does, not how it does it. This applies to tutorials
too. Explaining how it works is fine, but in a dedicated section.

Applying it:

- a user-facing page (README, tutorial, how-to guide, reference) says what the
  feature does and how to use it;
- implementation detail — internal components, the mechanism behind a call, the
  file layout of the machinery, internal types and internal paths — belongs to an
  explanation page or to a clearly named "How it works" section, and the page
  that used to carry it links there instead;
- a tutorial teaches the task and links the explanation; a how-to gives the steps
  and links the explanation; the reference documents the user-visible surface
  (the command, the generated API, the errors the user sees) and keeps only what
  a reader needs to use it;
- moving is not deleting: a passage that does not belong where it is goes to the
  page that owns it, with its evidence, rather than being dropped.
