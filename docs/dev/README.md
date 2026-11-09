# Developer documentation

This folder is for a contributor who works **on** cppgo: a Go and C++ developer who
has cloned the repository and needs to build it, measure it, or change it. It is
not the manual. The manual — what cppgo does and how to use it — is
[`README.md`](../../README.md) and the site under [`site/`](../../site). The
manual's reader is a Go developer who has never built a foreign function
interface (FFI: Go calling code that was not compiled as Go); this folder's
reader already knows both languages and needs to know this repository.

Start at [`CONTRIBUTING.md`](../../CONTRIBUTING.md). It has the prerequisites,
the commands that build and test the tree, and the rules a change is held to.
This folder is what a change needs next: how the project measures itself, where
each behavioural claim is pinned by a test, what each package owns, how
continuous integration (CI: the service that runs the checks on a branch) and
the site are built, and why the design is what it is.

The rules both the manual and this folder follow are in
[`.agents/skills/extra/documentation-rules.md`](../../.agents/skills/extra/documentation-rules.md).
They agree on what documentation is; they differ on where it lives. The manual
describes the library and the tool. A measurement date, a machine name, a commit
or an issue number, and the project's own process belong here, on a developer
page, and nowhere in the manual.

## What each page answers

| Document | Answers |
|---|---|
| [Benchmarks](benchmarks.md) | how the figures the manual publishes were measured, what every arm and flag means, and which numbered defect the suite found |
| [Evidence](evidence.md) | which test pins which behavioural claim, what each test asserts, and how to run one test on its own |
| [Layout](layout.md) | what each package owns and why, how the runtime and the generator are kept apart, and what generation writes |
| [Process](process.md) | how CI runs, what the pinned image holds, how coverage and the site are built, and where releases stand |
| [Decisions](decisions.md) | the design decisions in record form, the alternatives they rejected, and the work that is designed but not shipped |
| [Stack allocation](stack-allocation.md) | the research behind three questions — a C++ object in a Go frame, a zero-copy parameter, a manual declaration — with its verdicts and measurements |

[`CONTRIBUTING.md`](../../CONTRIBUTING.md) is the how-to for a first change;
[`CHANGELOG.md`](../../CHANGELOG.md) records what changed, in the user's terms;
[`DESIGN.md`](../../DESIGN.md) is the design contract, and [`docs/adr/`](../adr)
holds the architecture decision records (ADRs), one per decision, never edited
after acceptance.
