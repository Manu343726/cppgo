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

## Rule 3 — Binding examples start from the C++ API

Binding and code-generation examples and tutorials start from the C++ API. The
page opens with the C++ declaration/header and the library it comes from, then
the generation command, then the generated Go, then the Go call. The Go code is
never the starting point of a binding example.

Applying it:

- a page that shows generated Go shows the C++ declaration it comes from —
  copied from the real header in the repository, with a link to that file —
  next to or before the Go. Generated Go never appears in isolation in a
  tutorial, a how-to guide or a reference page;
- the order is the order of the work: the C++ header, `cppgo gen`, the generated
  Go, the call from Go. A page that currently opens with a Go type or function
  moves the C++ header or signature to the top, and the Go becomes the product
  of the generation;
- a page whose subject is not binding or code generation keeps its own order,
  and any generated Go it shows still brings its C++ declaration along;
- the C++ is taken from the tree, never invented, and the Go snippet corresponds
  to the C++ excerpt beside it. A target library whose headers are not in the
  repository shows its real header when the slice lands; until then the page
  says the slice is designed.

## Rule 4 — Show the code before talking about it

Documentation does not mention a thing assuming the reader knows what it is.
Show the code first — the C++ declaration and the Go binding/call, or the
relevant snippet — then discuss it. A page that talks about `cppgo_direct`, a
carrier, a helper or a function without showing or linking its definition fails
this rule. Benchmarks are the clear case: every arm and every measured function
shows the C++ it calls and the Go call that reaches it (and the cgo/Go
equivalent where compared) before the table; a reader must be able to read the
table from the code above it.

Applying it:

- a measured row, an arm, a carrier, a helper or a generated type is introduced
  by its definition — the C++ declaration or the Go function, copied from the
  tree and linked — before the text explains or measures it;
- a benchmarks page puts the header and the call of each arm above the tables,
  so the table is a summary of code the reader has already seen, never the first
  appearance of a name;
- a page whose subject is not code (a conceptual explanation, a comparison, a
  decision record) may link the definition instead of inlining it, and such a
  page is not reordered to satisfy this rule;
- a name with no definition and no link — `cppgo_direct`, a `cppgo_thunk_…`
  symbol, a helper such as `cppgo_last_error`, an arm named only in a table
  cell — is a defect to fix, not a shorthand to allow.

## Rule 5 — A benchmarks page is read one scenario at a time

The benchmarks page is organised per scenario, in the order the scenarios are
measured, and every scenario has the same four parts: **the scenario** — what is
measured, the C++ work, and the variants compared; **the code** — the C++
declaration and the call each arm makes to reach it; **the results** — one row
per arm with the numbers; **the conclusions** — what this scenario's numbers say
and why, written for a reader who has not seen the other scenarios. A table
never appears without the scenario and the code above it.

Applying it:

- the results table has one row per arm, and the row carries the iterations the
  benchmark executed (the `b.N` of its runs) and the number of runs (`-count`)
  beside the median ns/op, the min–max range of those runs, B/op and allocs/op:
  a reader must be able to tell a 5 ns figure from a 8-element loop and a 1 µs
  figure from a 1024-element one;
- the column of numbers is a measurement, so the scenario states the fixture the
  numbers were taken with — the sizes, the arguments, the failing input;
- the analysis sits under the table it explains, and it explains that table
  rather than repeating the page's thesis: why this row is where it is, what the
  allocation count costs, and where two arms are within each other's range and
  therefore not distinguishable;
- a fixture measured at two sizes is two scenarios with two tables, not one
  table with two kinds of row;
- an arm named in a table is identified by its definition above the table, and
  the page's preamble defines the arm names once so the first table does not
  introduce one.

## Rule 6 — A tutorial teaches a person, a how-to answers a task

Every tutorial opens with an introduction (what the reader will build and why),
states its objective (the concrete result at the end), introduces the example
scenario it uses — a real, user-facing project, not an internal test fixture —
and then walks step by step, explaining what each step does and why. Write prose
for a human who is learning, not feature announcements. A how-to guide is
task-focused, but it still opens with what the task is, when to use it, and the
scenario.

Applying it:

- the introduction says what the reader ends up with and what it is good for,
  before the first command;
- the objective is a result, not a topic: a program that runs, a file that
  exists, an output the reader can check — the text names it;
- the example scenario is introduced as the reader would meet it: a project with
  a purpose, its C++ header, and the goal the steps pursue. A synthetic fixture
  from the test suite is not a scenario;
- every step says what it does and why it is needed, in prose, in the reader's
  words; a bare sequence of commands with no explanation is a script, not a
  tutorial;
- a how-to guide opens with the task, when the reader needs it, and the scenario
  it assumes, then gives the steps.
