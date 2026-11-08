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

## Rule 2 — Documentation is not reflective

Documentation must not be reflective. It does not talk about itself, its
provenance, or the project's development process. A sentence such as "Measured
2026-11-08 on the machine that built this tree" belongs in developer
documentation, never in user documentation: the user does not care when, where,
on which machine or how the documentation was produced. Documentation is about
using the library, the tool or the feature, not a developer manual. Developer
documentation lives in a dedicated folder or document.

Applying it:

- user-facing pages carry no measurement dates, no machine or clock descriptions,
  no command used to produce the page, no "this tree", no commit, pull-request or
  issue numbers, no "we measured", no build or authoring notes, no description of
  the project's own CI or deployment, and no project history;
- **benchmarks are user documentation, not a diary.** A benchmarks page is a
  legitimate, evidence-bearing part of the manual: it contrasts cppgo with cgo
  and with plain Go, and across the carriers, object lifecycle and error paths,
  and it supports the project's performance claims. It carries the comparison
  and a neutral statement of the environment it was measured on — not when, where
  or by whom the page was produced. The full methodology and the run-by-run
  provenance live in `docs/dev/benchmarks.md`;
- developer material lives in [`docs/dev/`](../../../docs/dev/README.md): the
  benchmark methodology and its provenance, the claim-to-test evidence, the
  repository layout, the project's CI and release notes, the decision records and
  the status of designed-but-unshipped work. It is linked from
  [`CONTRIBUTING.md`](../../../CONTRIBUTING.md), not from the user pages;
- a performance characteristic may stay in user documentation as a
  characteristic ("a direct call stays in the same order as a Go call; cgo is
  roughly an order of magnitude slower"), and the benchmarks page is where the
  comparison lives. A number that is published as a measurement must come from a
  real run — never an estimate, never a placeholder;
- `CONTRIBUTING.md` and `CHANGELOG.md` are developer documents and keep their
  provenance;
- moving is not deleting: the passage goes to the developer document that owns
  it.
