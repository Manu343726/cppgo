---
name: documentation-manual
description: "The cppgo documentation manual, written in this repository: who the documentation is for, the six rules on what a page may say and in which order it shows its code, the operator's own anti-patterns, the evidence every claim carries, the Diátaxis map of the site, and the checklist a writer runs before opening a docs pull request. Read it before writing, rewriting or reviewing any cppgo page: README, tutorial, how-to, reference, explanation, benchmark page or the site under site/."
---

# The cppgo documentation manual

This manual is written in this repository, not vendored. It is the long form of the
numbered rule list in [`../extra/documentation-rules.md`](../extra/documentation-rules.md),
and it is the page both vendored documentation skills send you to first:
[`../documentation-writer/SKILL.md`](../documentation-writer/SKILL.md) and
[`../golang-documentation/SKILL.md`](../golang-documentation/SKILL.md).

Precedence, when two of these disagree:

1. [`../extra/documentation-rules.md`](../extra/documentation-rules.md) — the short numbered list.
   It is the operator's own text and it wins.
2. **this manual** — the long form of those rules, with the examples that make them
   checkable.
3. the vendored skills (`documentation-writer`, `golang-documentation`), which describe
   Diátaxis and Go doc comments in general and know nothing about this project.

The manual adds what the rule list is deliberately too short to carry: the audience, the
concrete anti-patterns, the evidence each claim owes, the map of which page is which
quadrant, and the pre-pull-request checklist. Where the rule list is silent, this manual
applies a rule; where it speaks, quote it.

`golang-documentation` still owns Go **doc comments** (`//` comments on exported
identifiers) and the README/CONTRIBUTING/CHANGELOG structure. This manual governs the
**prose documentation**: every page under
[`site/content/`](../../../site/content/), [`README.md`](../../../README.md),
[`CONTRIBUTING.md`](../../../CONTRIBUTING.md) and [`docs/`](../../../docs/).

## Who the documentation is for

The reader is **a competent Go developer who has never built an FFI**. They write Go every
day. They can read a Go interface, run `go test`, and read a stack trace. They have never
declared an `extern "C"` function, never linked a shared object by hand, and never thought
about which registers carry a struct.

What follows from that, on every page:

- **Every C++ and FFI term is defined the first time the page uses it.** Not on a glossary
  page the reader has to find: in the sentence that needs the word, or in a short list of
  terms just above the first step. "Thunk", "carrier", "shim", "soname", "POD", "mangled
  name", "compilation database" are all opaque to a Go developer.
- **Each page stands alone on terminology.** A tutorial and a how-to guide may both spell
  out what the shim is; the reader arrives at one of them, not at a sequence. Repeating a
  definition is not the "repeating the same lecture" the operator rejects. That rejection
  (epic [#1](https://git.manu343726.com/manu343726/cppgo/issues/1)) is about repeating an
  explanation of the whole mechanism on every page, and is rule 1's subject: the deeper
  explanation has one home, and the other pages link it.
- **No step is skipped because it is obvious to someone who has done it before.** "Run
  `cppgo gen`" is a step only if the page first showed the command, the header it reads and
  where the output lands.
- **Praise, cleverness and marketing are not content.** A page states what exists, what it
  costs and what it cannot do. It does not call anything "seamless", "robust" or
  "enterprise-grade".

### The terms a page owes its reader

These are the words the project uses as if they were obvious. Each is defined properly
elsewhere in the tree; a page that uses one before its first definition is a defect.

| Term | What the reader needs, in one line | Defined properly in |
|---|---|---|
| FFI | foreign function interface: Go calling code that was not compiled as Go, through the platform's C calling convention | [`site/content/tutorials/first-binding.md`](../../../site/content/tutorials/first-binding.md) |
| ABI | application binary interface: how a compiled call is laid out — which registers and which memory carry the arguments and the result | [`DESIGN.md`](../../../DESIGN.md) § *The ABI the direct path calls* |
| cgo | Go's built-in way to call C: it needs `CGO_ENABLED=1` and a C toolchain at build time | [`site/content/explanation/benchmarks.md`](../../../site/content/explanation/benchmarks.md) § *cgo* |
| libclang | the C library of the Clang compiler front end, which cppgo uses at generation time to read a header | [`README.md`](../../../README.md) § *Quick start* (the tool table) and [`CONTRIBUTING.md`](../../../CONTRIBUTING.md) § *Prerequisites* |
| shared object, `soname` | the `.so` file the loader opens at run time, and the name recorded inside it that the loader resolves | [`site/content/explanation/architecture.md`](../../../site/content/explanation/architecture.md) § *Loading: the two objects* |
| carrier | the mechanism that carries one call across the boundary: a direct trampoline, a thunk compiled into the shim, or the callback-capable reentrant call | [`DESIGN.md`](../../../DESIGN.md) § *Carriers* |
| thunk, shim | generated C++ that adapts a declaration the direct call cannot carry, compiled by cppgo into `libcppgo_<package>.so` | [`docs/dev/layout.md`](../../../docs/dev/layout.md) § *The generated files* |
| POD | plain old data: a struct with no constructors, no virtual functions and a standard layout, such as `metrics::Point` | [`examples/full/include/metrics/metrics.hpp`](../../../examples/full/include/metrics/metrics.hpp) |
| two-phase flow | templates only exist where they are instantiated, so `cppgo gen` writes the generic API and `cppgo build -scan` adds the instantiations the Go program uses | [`site/content/explanation/architecture.md`](../../../site/content/explanation/architecture.md) § *Templates: why two phases* |

## Rule 1 — User documentation says what the feature does, not how it does it

The documentation is for users. It says what a feature does, how to use it, and what it
costs. It does not say how the machinery works, except inside a section that is explicitly
about the machinery. This holds for tutorials too.

Applying it:

- A user-facing page (README, tutorial, how-to, reference) says what the feature is, what
  the user writes, what happens, and what to do when it fails.
- Implementation detail — internal packages, the mechanism behind a call, the file layout
  of the machinery, internal types and internal paths — belongs to an explanation page or
  to a section literally named *How it works*, and the page that used to carry it links
  there instead.
- A tutorial teaches the task and links the explanation. A how-to gives the steps and links
  the explanation. The reference documents the user-visible surface: the command, the
  generated API, the errors the user sees.
- **Moving is not deleting.** A passage that does not belong where it is moves to the page
  that owns it, with its evidence, rather than being dropped.
- The one home of the deep explanation in this project is
  [`site/content/explanation/architecture.md`](../../../site/content/explanation/architecture.md)
  (and, per topic, [`carriers.md`](../../../site/content/explanation/carriers.md),
  [`go-runtime.md`](../../../site/content/explanation/go-runtime.md) and
  [`limits.md`](../../../site/content/explanation/limits.md)). A fifth page that explains
  the wire a fourth time is the defect this rule exists for.
- Design decisions are the ADRs' business:
  [`docs/adr/0001-header-first-bindings.md`](../../../docs/adr/0001-header-first-bindings.md)
  and its neighbours, plus [`DESIGN.md`](../../../DESIGN.md). No user page restates them.

The anti-pattern: a how-to guide that stops in the middle of the steps to explain how the
trampoline aligns the stack. The fix is one sentence of what the user gets plus a link, and
the explanation grows where it belongs.

## Rule 2 — Documentation is not reflective

Documentation does not talk about itself, about when or where it was produced, or about the
project's own process. It is about the library, the tool and the feature.

Applying it:

- A user-facing page carries **no measurement dates, no machine or clock descriptions, no
  command used to produce the page, no "this tree", no commit, branch, pull-request or issue
  numbers, no "we measured", no build or authoring notes, no description of the project's
  CI or deployment, and no project history.**
- A statement like "measured on the machine that built this tree" is a defect on a user
  page, even when the number is right. A number is published as a characteristic of the
  implementation, with the environment stated neutrally and the run it came from linked.
- **Benchmarks are user documentation, not a diary.** A benchmarks page is a legitimate,
  evidence-bearing part of the manual: it contrasts this project with cgo and with plain Go,
  across carriers, object lifecycle and error paths, and it supports the performance claims
  the project makes. It carries the comparison, the code and a neutral statement of the
  environment the numbers were measured in. It does not carry when, where or by whom the
  page was written.
- Developer material lives in [`docs/dev/`](../../../docs/dev/README.md): the benchmark
  methodology and its provenance, the claim-to-test evidence
  ([`evidence.md`](../../../docs/dev/evidence.md)), the repository layout
  ([`layout.md`](../../../docs/dev/layout.md)), the project's CI and releases
  ([`process.md`](../../../docs/dev/process.md)) and the decision history
  ([`decisions.md`](../../../docs/dev/decisions.md)). It is reached from
  [`CONTRIBUTING.md`](../../../CONTRIBUTING.md), not from the user pages.
- **A number published as a measurement comes from a real run.** Never an estimate, never a
  placeholder, never a number remembered from another page. The run it came from is linked;
  the method that produced it stays in
  [`docs/dev/benchmarks.md`](../../../docs/dev/benchmarks.md) and
  [`benchmarks/README.md`](../../../benchmarks/README.md).
- [`CONTRIBUTING.md`](../../../CONTRIBUTING.md) and [`CHANGELOG.md`](../../../CHANGELOG.md)
  are developer documents and keep their provenance.

The anti-pattern, verbatim: *"Measured 2026-11-08 on the machine that built this tree."* It
fails three ways at once: it dates a page that will outlive the date, it names the author's
machine, and it tells the reader about the project's process instead of the project. What
the sentence wanted to say is *"on a Ryzen 7 6800HS, `-count=6`, median of six runs"*, and
even that belongs on the benchmarks page rather than on the page introducing the feature.

## Rule 3 — Binding and code-generation examples start from the C++ API

Every example, how-to and tutorial that shows generated Go starts from the C++ declaration
it came from. The Go is the product of the generation, never the starting point.

The order of a binding example is the order of the work:

1. the C++ declaration, copied from the real header, with a link to that file;
2. the command that reads it (`cppgo gen`, then `cppgo build -scan` where templates are
   involved);
3. the generated Go, as `cppgo gen` wrote it;
4. the Go call the user writes in their program.

Applying it:

- The C++ is **taken from the tree**, never invented, and the Go beside it corresponds to
  that exact excerpt.
- A page that currently opens with a Go type or a Go function moves the C++ declaration to
  the top, and the Go becomes what generation produced.
- The header is shown with its namespace and its `#include`s intact enough that a reader can
  recognise it as C++ written by someone else. A tutorial binds a header from
  [`examples/mathx`](../../../examples/mathx) or [`examples/full`](../../../examples/full),
  not a fragment.
- A page whose subject is not binding or code generation keeps its own order; if it shows
  generated Go at all, it brings the C++ declaration along.
- Correct examples to imitate:
  [`site/content/tutorials/first-binding.md`](../../../site/content/tutorials/first-binding.md)
  (Step 1 is the header of `mathx::Vector`, Step 4 is `cppgo gen`) and
  [`site/content/how-to/generate.md`](../../../site/content/how-to/generate.md).

The anti-pattern: a Go snippet in a tutorial with no C++ above it. The reader cannot tell
whether that Go was generated or hand-written, nor which declaration produced it, so the
page teaches nothing about the thing it claims to teach.

## Rule 4 — Show the code before talking about it

No identifier, arm, carrier, helper or helper type is mentioned before its definition is
shown or linked. Show the code first, then discuss it.

Applying it:

- A measured row, an arm, a carrier, a helper or a generated type is introduced by its
  definition — the C++ declaration or the Go function, copied from the tree and linked —
  before the text explains or measures it.
- A benchmarks page puts the header and the call of each arm above the tables, so a table is
  a summary of code the reader has already seen, never the first appearance of a name.
- A page whose subject is not code (a conceptual explanation, a comparison, a decision
  record) may link the definition instead of inlining it, and is not reordered to satisfy
  this rule.
- A name with no definition and no link — `cppgo_direct`, a `cppgo_thunk_…` symbol, a helper
  such as `cppgo_last_error`, an arm named only in a table cell — is a defect to fix, not a
  shorthand to allow.
- A bare path is the same defect in another form. A path is a link to the file it names
  (`[`vector.hpp`](../../../examples/mathx/include/mathx/vector.hpp)`), never a string in
  running text.

The anti-pattern: a benchmark table whose rows are `go_direct`, `cgo_thunk` and
`cppgo_direct`, appearing before any of them is defined, on a page that has not yet said
what `bench::add` is. The reader reads a table of names and cannot check a single cell
against anything.

## Rule 5 — Benchmarks are documented per scenario, in a fixed order

A benchmark page is per scenario, and every scenario has four parts, in this order:

1. **The scenario.** What is measured, what C++ work the call does, and which variants are
   compared (this project's generated binding, cgo, plain Go). Why the scenario matters for
   a user.
2. **The scenario's code.** The C++ declaration being measured, and for every arm the Go
   call that reaches it (rules 3 and 4). A reader must be able to read the table from the
   code above it.
3. **The results table.** One row per arm, including:
   - the **iteration count** the row was measured over — the `N` the benchmark runner
     reports for that row (the count `b.N` or `b.Loop()` ran);
   - the **run count** (`-count=N`: how many separate runs the row is summarised from);
   - ns/op, B/op and allocs/op;
   - the median and the range, not a lone average.
4. **The conclusions.** What the numbers say for this scenario and why, in terms the reader
   already has from parts 1 and 2.

Applying it:

- **No table without the scenario and the code above it.** A table that appears before its
  scenario is the rule-4 defect and the rule-5 defect at the same time.
- The iteration count is not decoration: it tells the reader whether a 6 ns row is a
  measurement of a billion tiny calls or of a handful of expensive ones, and it is the
  first thing a reader uses to judge whether the ns/op figure is stable.
- The scenarios the performance page covers, as the operator listed them
  ([#68](https://git.manu343726.com/manu343726/cppgo/issues/68)): the crossing `add`; a 16 B
  POD by value; the result above 16 B; `std::string`; `std::vector`, both sizes; a throwing
  failure; the `-status` path, ok and failure; the C++-to-Go callback; and the object
  lifecycle. The measured declarations live in
  [`benchmarks/include/bench/bench.hpp`](../../../benchmarks/include/bench/bench.hpp).
- The same table on a developer page follows the same order; the difference is that
  [`benchmarks/README.md`](../../../benchmarks/README.md) may carry the methodology and the
  raw runs ([`benchmarks/results/`](../../../benchmarks/results/)), and the user page
  ([`site/content/explanation/benchmarks.md`](../../../site/content/explanation/benchmarks.md))
  carries the comparison and the neutral environment statement.
- Ranges are shown, never averaged away. A row whose six runs span 91–99 ns says that; one
  that reports "94.2" alone hides it.

## Rule 6 — A tutorial has a shape

Every tutorial has these parts, in this order:

> An **introduction** (what the reader will build and why), an **objective** (the concrete
> result at the end), the **example scenario** it uses — a real, user-facing project, not an
> internal test fixture — and then a walk **step by step**, each step explaining what it
> does and why, and a **closing** (what the reader now has, and where to go next). Write
> prose for a human who is learning, not feature announcements.

That wording is the operator's, from [#70](https://git.manu343726.com/manu343726/cppgo/issues/70).

A **how-to guide** is task-focused, but it still opens with the task, when to use it, and
the scenario it serves.

Applying it:

- **A real scenario, never an internal test fixture.** `internal/gen/testdata/box/box.hpp`
  is a test fixture for the generator: it exists to exercise a construct, it is not a
  library anyone uses, and a tutorial that binds it teaches the reader a fixture. Bind
  [`examples/mathx/include/mathx/vector.hpp`](../../../examples/mathx/include/mathx/vector.hpp)
  or [`examples/full/include/metrics/metrics.hpp`](../../../examples/full/include/metrics/metrics.hpp),
  which are real projects with their own builds, and link the files.
- **Steps are numbered and named for what the reader does** ("Step 2: Build the library the
  way you always would"), not for the feature being announced.
- **Each step says why before how.** "The generator reads the header, not the compiled
  library, so this step is only about the header" is the explanation a step owes.
- The closing points at the next page: a how-to for the variations, an explanation for the
  mechanism, a reference for the full surface.
- Tutorials in the tree to imitate, and against which to review a new one:
  [`site/content/tutorials/first-binding.md`](../../../site/content/tutorials/first-binding.md)
  and [`site/content/tutorials/full-project.md`](../../../site/content/tutorials/full-project.md).

The anti-pattern, abridged from
[`site/content/how-to/templates.md`](../../../site/content/how-to/templates.md) as the page
opened before rule 6 applied (its link markup is elided here). The page now opens with
prose, an objective, and the real `metrics` scenario of
[`examples/full`](../../../examples/full):

> ## The header
>
> The template is declared in a header, and that header is what `cppgo gen` reads.
> This is `box::Box`, the fixture the two-phase flow is measured on, in
> `internal/gen/testdata/box/box.hpp`.

Three defects in three lines: it opens on a section title that assumes the reader knows why
the header matters; it introduces `box::Box` with an internal test fixture as the reason the
type exists ("the fixture the two-phase flow is measured on" is about the project's tests,
not about the reader's task); and it sends the reader to a path in
`internal/gen/testdata`, where no library they can bind lives. A learned
reader gets through it; a reader learning the two-phase flow cannot tell whether `Box` is a
library they can use.

## The rule list carries rules 1–6

[`../extra/documentation-rules.md`](../extra/documentation-rules.md) carries all six rules —
rule 1 through rule 6, each with its own `Applying it` list — and it is the canonical wording.
The manual's § *Rule 5* and § *Rule 6* above are its long form: the same requirement, plus the
examples and the closing a tutorial owes, which the rule list leaves to this page. Where a
sentence of rules 5 or 6 in this manual differs from the file, the file wins, as the
precedence at the top of this page says.

Rule 5, *A benchmarks page is read one scenario at a time*, opens:

> The benchmarks page is organised per scenario, in the order the scenarios are measured, and
> every scenario has the same four parts: **the scenario** — what is measured, the C++ work,
> and the variants compared; **the code** — the C++ declaration and the call each arm makes
> to reach it; **the results** — one row per arm with the numbers; **the conclusions** — what
> this scenario's numbers say and why, written for a reader who has not seen the other
> scenarios.

Rule 6, *A tutorial teaches a person, a how-to answers a task*, opens:

> Every tutorial opens with an introduction (what the reader will build and why), states its
> objective (the concrete result at the end), introduces the example scenario it uses — a
> real, user-facing project, not an internal test fixture — and then walks step by step,
> explaining what each step does and why.

The first is the four parts the manual's § *Rule 5* numbers, the second the opening of the
shape its § *Rule 6* lists; the manual adds the examples that make each one checkable, and the
closing section the rule list leaves to the long form.

Do not edit the rule list for documentation reasons. It is the operator's file, and a writer
reports a disagreement instead of resolving it alone.

## The anti-patterns, named

The operator named these four, and they are the shortest statement of what this manual is
for. Each is quoted as written, and each one is a defect wherever it appears.

| The anti-pattern | Why it fails | What replaces it |
|---|---|---|
| *"Measured 2026-11-08 on the machine that built this tree"* | A date, a machine and a description of the authoring process on a user page (rule 2). The user cannot use any of the three, and the page will be wrong as soon as the tree moves. | The characteristic and its neutral environment: *"median of six runs on a Ryzen 7 6800HS; the run is linked"*, on the benchmarks page, with the method in the developer docs. |
| *"The header. The template is declared in a header… This is `box::Box`, the fixture the two-phase flow is measured on, in `internal/gen/testdata`"* | A terse feature-announcement opening (rule 6), an internal test fixture used as the example (rule 6), and a path into `internal/` that a user cannot follow (rule 4). | An introduction and objective, a real library from [`examples/`](../../../examples), the header in code with a link, then the numbered steps — what [`site/content/how-to/templates.md`](../../../site/content/how-to/templates.md) now does. |
| A Go snippet with no C++ above it (rule 3) | The reader cannot tell what was generated, from which declaration, or whether the snippet is the generator's output or something they must write. | The C++ declaration, the `cppgo gen` command, the generated Go, then the Go call. |
| A benchmark table with no scenario and no code above it (rule 4, rule 5) | The table is a wall of names the reader has never seen, with no way to judge a row. | Scenario, scenario code (every arm's C++ and Go), table with iterations and runs, conclusions. |

Three more that the rules imply, and that review must catch:

- **A bare path in running text.** Every path is a link (rule 4). `internal/gen/testdata` and
  `docs/dev/benchmarks.md` in a sentence are defects; the link is the reference.
- **A number with no real run behind it.** Rule 2: an estimate, a placeholder, or a figure
  copied from a page whose run was on different hardware is not evidence.
- **The same lecture on every page.** Epic [#1](https://git.manu343726.com/manu343726/cppgo/issues/1)
  rejects a documentation set in which the ABI is re-explained page after page. One home,
  rule 1, links from everywhere else.

## Evidence

Every claim carries the way it was obtained.

- **A number** comes from a real run, and the run is reachable: the command in
  [`benchmarks/README.md`](../../../benchmarks/README.md), the committed output in
  [`benchmarks/results/`](../../../benchmarks/results/), the method in
  [`docs/dev/benchmarks.md`](../../../docs/dev/benchmarks.md). "Good coverage" is not a
  claim; `make cover-gate` enforcing a threshold is.
- **A behaviour** carries the test that asserts it, or the fixture that demonstrates it. The
  mapping from claim to test already exists in
  [`docs/dev/evidence.md`](../../../docs/dev/evidence.md); a user page states the behaviour
  and links there rather than inventing a measurement.
- **A version** carries where the floor is recorded: `go.mod`'s `go` line, libclang 20 in
  [`CONTRIBUTING.md`](../../../CONTRIBUTING.md) § *Prerequisites* (the distribution packages
  stop at 19, and `clang_getOffsetOfBase` is not exported before 20).
- **Code that is quoted is real.** Open the file, copy the lines, link the file. A
  paraphrased API is a defect even when it would compile.
- **Every entity and file is a link.** A directory, the board, a service, a component, a
  page: linked, never named. The board is
  <https://git.manu343726.com/manu343726/cppgo/issues>; the site is
  <https://manu343726.github.io/cppgo/>.
- **English only**, in every file and every example. Typographic characters are fine;
  another language is not.
- **A document that no longer describes the code is a bug**, filed with the evidence: the
  doc line and the source line that contradict it.

## Diátaxis: which page is which

The site under [`site/`](../../../site) is laid out along the four quadrants, and a page
that confuses two of them is rewritten rather than patched.

| Quadrant | Serves | Shape | Pages |
|---|---|---|---|
| Tutorial | the reader learning by doing | introduction, objective, scenario, numbered steps, closing (rule 6) | [`tutorials/first-binding.md`](../../../site/content/tutorials/first-binding.md), [`tutorials/full-project.md`](../../../site/content/tutorials/full-project.md) |
| How-to | the reader with a task | the task, when to use it, the scenario, the steps, the explanation linked (rule 1) | [`how-to/generate.md`](../../../site/content/how-to/generate.md), [`how-to/templates.md`](../../../site/content/how-to/templates.md), [`how-to/errors.md`](../../../site/content/how-to/errors.md), [`how-to/embed.md`](../../../site/content/how-to/embed.md), [`how-to/ci.md`](../../../site/content/how-to/ci.md), [`how-to/deploy.md`](../../../site/content/how-to/deploy.md) |
| Reference | the reader looking something up | the user-visible surface, exactly: the command, the generated API, the errors | [`reference/cli.md`](../../../site/content/reference/cli.md), [`reference/generated-api.md`](../../../site/content/reference/generated-api.md), [`reference/runtime.md`](../../../site/content/reference/runtime.md) |
| Explanation | the reader who wants to understand | the mechanism, in one place, linked from everywhere else | [`explanation/architecture.md`](../../../site/content/explanation/architecture.md), [`explanation/carriers.md`](../../../site/content/explanation/carriers.md), [`explanation/go-runtime.md`](../../../site/content/explanation/go-runtime.md), [`explanation/limits.md`](../../../site/content/explanation/limits.md), [`explanation/why.md`](../../../site/content/explanation/why.md), [`explanation/benchmarks.md`](../../../site/content/explanation/benchmarks.md) |

A page in the wrong quadrant is the root cause of most rule violations: a reference page that
starts teaching becomes a tutorial, a tutorial that explains the ABI becomes an explanation
and stops teaching. Move the page, do not bend the rule.

## Where each kind of material lives

| Material | Where | Rule |
|---|---|---|
| what the feature does, how to use it | [`README.md`](../../../README.md) and [`site/content/`](../../../site/content/) | 1 |
| the mechanism behind a call | [`site/content/explanation/`](../../../site/content/explanation/) | 1 |
| the design decision and its alternatives | [`docs/adr/`](../../../docs/adr/) and [`DESIGN.md`](../../../DESIGN.md) | 1 |
| the benchmark methodology and provenance | [`docs/dev/benchmarks.md`](../../../docs/dev/benchmarks.md), [`benchmarks/README.md`](../../../benchmarks/README.md) | 2, 5 |
| which test pins which claim | [`docs/dev/evidence.md`](../../../docs/dev/evidence.md) | 2 |
| the repository layout and generated files | [`docs/dev/layout.md`](../../../docs/dev/layout.md) | 1, 2 |
| CI, coverage, the site build and releases | [`docs/dev/process.md`](../../../docs/dev/process.md), [`CONTRIBUTING.md`](../../../CONTRIBUTING.md) | 2 |
| the project's history and designed-but-unshipped work | [`docs/dev/decisions.md`](../../../docs/dev/decisions.md) | 2 |
| what changed and when | [`CHANGELOG.md`](../../../CHANGELOG.md) | 2 |

## Before you open a docs pull request

Run this list against the diff, in order. A "no" is not a note for later; it is the change.

1. **Audience.** Does every C++ or FFI term appear with its definition, in the page that
   first needs it, the way a Go developer who has never built an FFI would need it?
2. **Rule 1.** Does the page say what the feature does and how to use it, with the mechanism
   either absent or inside a *How it works* section or an explanation page it links?
3. **Rule 2.** Search the diff for a date, a machine name, "this tree", a commit or issue
   number, "we measured", a CI or deployment note. None belongs on a user page. Developer
   material goes to [`docs/dev/`](../../../docs/dev/README.md).
4. **Rule 3.** Does every generated-Go snippet have its C++ declaration above it, copied
   from a real header in the tree and linked?
5. **Rule 4.** Is every identifier, arm, carrier and helper either shown before it is
   discussed, or linked to its definition? Is every path a link?
6. **Rule 5.** Does every benchmark scenario have its explanation, its code, its table
   (iterations, run count, ns/op, B/op, allocs/op, median and range) and its conclusions, in
   that order?
7. **Rule 6.** Does every tutorial have an introduction, an objective, a real user-facing
   scenario (not a fixture under `internal/`), numbered steps with the reason for each, and a
   closing? Does every how-to open with its task, when and scenario?
8. **Evidence.** Is every number traceable to a run, every behaviour to a test, every
   version to a file? Does anything claim more than the tree proves?
9. **The build.** `make site` succeeds — Hugo fails the build on a broken `ref`/`relref`, so
   a green build is the link check for the site. The pages the change links still exist.
10. **The international check.** The diff is English. Re-read the two sentences you are least
    sure about; if they are written for someone who already knows, rewrite them.

A docs change is reviewed against this list, not against taste. When a page and an issue
disagree about a measurement, the issue with the run behind it wins and the page is
corrected; when a rule here and the rule list disagree, the rule list wins and this manual
is the thing to fix.
