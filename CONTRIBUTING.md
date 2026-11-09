# Contributing to cppgo

This page takes you from a fresh clone to a change you can open a pull request
for. It is for a Go or C++ developer who has never worked on this repository: it
names every tool you need and why, gives the commands in the order that works,
and says what a successful run looks like. The design behind those commands is in
[Layout](docs/dev/layout.md) and [Process](docs/dev/process.md); this page is the
practical half.

## The words this page uses

| Term | What it means here |
|---|---|
| **cgo** | Go's built-in way to call C. It needs `CGO_ENABLED=1` and a C compiler at build time. cppgo's runtime and the packages it generates must build with `CGO_ENABLED=0`: that is the project's central promise |
| **libclang** | the C library of the Clang compiler front end, which parses C++ declarations. `cppgo gen` and `cppgo check` use it; a program built with a generated package does not |
| **shim** | the C++ library cppgo generates from the declarations a direct call cannot carry, and compiles itself: `libcppgo_<package>.so` |
| **generated package** | the Go package `cppgo gen` writes. Its files (`zz_cppgo.gen.go`, `zz_cppgo_amd64.s`, `cppgo_shim/`) are output, never edited by hand |
| **trampoline** | the generated assembly that makes one call into C++ |
| **thunk** | the generated C++ function that adapts a declaration the trampoline cannot carry |
| **race detector** | Go's `-race` mode, which reports two goroutines touching the same memory without synchronisation |
| **coverage** | the fraction of Go statements the tests executed at least once, measured with `-coverprofile` and gated at 99% |
| **golden file** | a committed expected output that a test compares byte for byte. When the emitter changes, the golden files change with it |
| **pull request (PR)** | the platform's request to merge a branch into `main`, and the place CI reports the result of that branch |
| **Forgejo** | the self-hosted git service the project lives on, at <https://git.manu343726.com>; its issue tracker is where work is recorded |
| **golangci-lint** | a linter that runs many Go analyzers in one pass and reports the file and line of each finding |

## Prerequisites

| Tool | Version | Why you need it |
|---|---|---|
| [Go](https://go.dev/dl/) | 1.25 or newer | [`go.mod`](go.mod) declares `go 1.25.0`; the generated code uses generics |
| A C++ compiler | C++17; `g++` or `clang++` | `cppgo gen` compiles the shim, and the examples are C++17 projects. Use the same compiler family and standard library as the library you bind |
| [libclang](https://clang.llvm.org/docs/Tooling.html) | **20 or newer** | `cppgo gen` and `cppgo check` parse headers with it. `clang_getOffsetOfBase` is not exported before 20, so an older library fails at load time with an undefined symbol |
| [`make`](https://www.gnu.org/software/make/) | any | every target on this page |
| [CMake](https://cmake.org/) | 3.16 or newer, the minimum [`examples/full/CMakeLists.txt`](examples/full/CMakeLists.txt) declares | [`examples/full`](examples/full) is a CMake project; `make examples` drives it |
| `nm` (binutils) | any | two tests inspect a shared object's exported symbols. They skip when it is missing, so it is optional |
| [Docker](https://docs.docker.com/get-docker/) | optional | to build the pinned CI image, or to run the documentation site with the same Hugo |
| [Hugo extended](https://gohugo.io/installation/) | 0.169 | only to build [`site/`](site) outside the pinned image |

On Debian and Ubuntu, take libclang from [apt.llvm.org](https://apt.llvm.org/):
the distribution packages stop at 19, which cannot provide
`clang_getOffsetOfBase`. Installing `clang` alone is not enough; you need
`libclang-<major>-dev` and the matching `clang-<major>`. On Arch and Manjaro the
`clang` package is current. Check what you have:

```console
$ go version
go version go1.27.0-X:nodwarf5 linux/amd64

$ g++ --version | head -n 1
g++ (GCC) 16.2.1 20260810

$ clang --version | head -n 1
clang version 22.1.8

$ ldconfig -p | grep libclang | head -n 2
	libclang.so.22.1 (libc6,x86-64) => /usr/lib/libclang.so.22.1
	libclang.so.20.1 (libc6,x86-64) => /usr/lib/libclang.so.20.1
```

A missing libclang is not silent: `cppgo gen` reports it and names the package to
install.

## Your first change, step by step

**Step 1: clone the repository.** The canonical remote is the project's Forgejo;
the GitHub repository is a mirror of `main` only.

```console
$ git clone ssh://git@git.manu343726.com:2222/manu343726/cppgo.git
$ cd cppgo
```

**Step 2: build the command.** This compiles every package with
`CGO_ENABLED=0`, which is the no-cgo contract, and writes `bin/cppgo`:

```console
$ make build
CGO_ENABLED=0 go build ./...
  build cppgo -> bin/cppgo

$ bin/cppgo version
cppgo dev
```

`dev` is the version a local build carries; a release build sets it with
`-ldflags "-X main.version=…"`.

**Step 3: run the tests.** `make test` runs two passes on purpose: the first
proves the suite needs no cgo, and the second runs the same tests under the race
detector, which is built on cgo and is rejected with `CGO_ENABLED=0`:

```console
$ make test
== pass 1/2: CGO_ENABLED=0 go test -count=1 ./... ==
…
ok  	github.com/Manu343726/cppgo/internal/shim	5.177s
== pass 2/2: CGO_ENABLED=1 go test -race -count=1 ./... ==
…
ok  	github.com/Manu343726/cppgo/internal/shim	6.926s
```

Every line must say `ok`. A `FAIL` line names the package, and
`go test ./internal/shim -run '^TestStdStringAndVector$' -v` runs one test with
its output; the mapping from behaviour to test is
[Evidence](docs/dev/evidence.md). Expect
several minutes: `internal/gen` compiles C++ fixtures and is the slow package.
Do not "fix" the two passes into one command; the two properties cannot both be
measured by one.

**Step 4: run the coverage gate.**

```console
$ make cover-gate
…
PASS total coverage 99.1% >= 99.0%
```

The gate is 99% on `./...`. If `coverage.out` does not exist, the target runs
`make cover` first, which is another test pass with a coverage profile; that is
why `make verify` takes longer than the sum of its parts. The exclusions (the
generated files, the generated fixtures and the test scaffolding) are printed by
the script with their reason.

**Step 5: build the examples.** This is the only step that needs a C++ compiler
and CMake, and it is the closest thing to a user's own project:

```console
$ make examples
== mathx: check
== mathx: make
…
== stats: check
== stats: make
…
== full: check
== full: make
…
examples: 3 built, 0 skipped
```

**Step 6: lint.**

```console
$ make lint
CGO_ENABLED=0 go vet ./...
… (golangci-lint, when installed)
```

`make lint` always runs `go vet`. It then runs `golangci-lint` when that command
is on your `PATH`; without it, the target prints a warning and `go vet` is the
only check. A finding names the file and the line; fix or explain it in the pull
request.

**Step 7: one command for all of it.**

```console
$ make verify
```

`make verify` is `build lint test cover-gate examples`, in that order: the
closest thing to a single CI command. Its steps are the ones above; if one of
them failed on its own, it will fail here too.

**Step 8: pick an issue and branch.** The tracker is the project's Forgejo:
<https://git.manu343726.com/manu343726/cppgo/issues>. Comment on the issue that
you are taking it, then branch from `main` and name the branch after the issue:

```console
$ git switch -c dev/123
```

**Step 9: make the change, test it, commit it.** One logical change per commit,
Conventional Commits (`feat(gen): …`, `fix(runtime): …`, `docs(site): …`), and a
body that says what you measured, with the command and its output. Push the
branch as you go:

```console
$ git push -u origin dev/123
```

**Step 10: open the pull request.** Open it against `main`, name every issue it
serves (`Refs #123`), and include what changed, what you measured and how to
undo it. CI runs on the pull request and its result is the gate. Never merge your
own pull request, and never push to `main`.

## The rules that are not negotiable

- **The runtime never imports the generator.** The root package (and everything
  it imports) must stay free of [`internal/cxx`](internal/cxx) and libclang; a
  test fails when the dependency appears. libclang is a generation-time
  dependency only.
- **The runtime builds and its tests run with `CGO_ENABLED=0`.** No change may
  introduce cgo into the runtime or into generated code.
- **A generated file is never edited by hand.** `zz_cppgo*.go`,
  `zz_cppgo_amd64.s` and `cppgo_shim/` are output. Change the emitter,
  regenerate, and commit the result. The examples commit their generated sources,
  so `cppgo check` catches a hand edit.
- **The user's build never compiles a generated file.** The shim is cppgo's, and
  it links *against* the user's library. The shim tests build the two as
  separate objects and assert which one exports what: an inline body is absent
  from the user's library and present in the shim.
- **A construct is either emitted or refused.** There is no pass-through guess.
  `rejectUnimplemented` in [`internal/gen/reject.go`](internal/gen/reject.go) is
  the single list of what the build does not emit, and each entry names the
  declaration and the fix.

## Where things live

| Area | Path |
|---|---|
| runtime the generated code imports | [repository root](cppgo.go): [`cppgo.go`](cppgo.go), [`loadfs.go`](loadfs.go), [`object.go`](object.go), [`reentrant.go`](reentrant.go), [`errors.go`](errors.go), [`callerror.go`](callerror.go), [`preload.go`](preload.go) |
| call trampolines and the stack probe | [`internal/asm`](internal/asm) |
| C++→Go callbacks | [`internal/callback`](internal/callback) |
| dynamic-loader wrapper | [`internal/rtl`](internal/rtl) |
| libclang model | [`internal/cxx`](internal/cxx) |
| selection, carriers, emitters, shim build | [`internal/gen`](internal/gen) |
| error projection (not consumed by the generator yet) | [`internal/errormap`](internal/errormap) |
| construct matrix against a real compiler | [`internal/shim`](internal/shim) |
| command line | [`cmd/cppgo`](cmd/cppgo) |
| executable examples | [`examples/`](examples) |
| benchmark suite | [`benchmarks/`](benchmarks) |
| design contract and decisions | [`DESIGN.md`](DESIGN.md), [`docs/adr/`](docs/adr) |
| documentation site sources | [`site/`](site) |
| developer documentation | [`docs/dev/`](docs/dev/README.md) |

## Tests

- Table-driven, with `t.Run` per case; [testify](https://github.com/stretchr/testify)'s
  `require` and `assert`. A table-driven test is a slice of cases and one loop;
  `t.Run` gives each case its own name in the output.
- `t.Parallel()` where the cases are independent.
- Concurrency tests run under `-race` and `GOMAXPROCS=1` with at least 32
  goroutines.
- A behaviour that the documentation claims needs a test. If you cannot test it,
  say so in the pull request instead of claiming it.
- When you add a construct to the emitter, add both: a golden file under
  [`internal/gen/testdata/golden/`](internal/gen/testdata/golden) and an
  end-to-end test that compiles the shim with a real compiler and calls the
  result. The shim's compilation is the gate that a model alone cannot give you.

The coverage gate is **≥ 99%** on `./...`, measured by
[`scripts/coverage-gate.sh`](scripts/coverage-gate.sh). Its exclusions are the
three it prints, each with its reason: generated output (`zz_cppgo*.go`), the
generated fixtures under [`internal/gen/testdata/`](internal/gen/testdata), and
[`internal/testlib/`](internal/testlib). No hand-written production package is
excluded.

## Branches, commits and pull requests

- Branch from `main` and name the branch after the issue: `dev/<issue>`.
- One deliverable per pull request. A service-like change does not split across
  two PRs; a refactor with no behaviour change does not ride along with a fix.
- Use Conventional Commits (`feat(gen): …`, `fix(runtime): …`, `docs(site): …`).
  The body says what was measured, with the command and the result.
- Never merge your own pull request, and never push to `main`.

## Documentation changes

- The site lives in [`site/`](site) and follows four sections: tutorials, how-to
  guides, reference and explanation. A new page belongs to exactly one of them.
- The project's rules for documentation live in
  [`.agents/skills/extra/documentation-rules.md`](.agents/skills/extra/documentation-rules.md).
  They extend the vendored documentation skills and win over their text: user
  documentation says what a feature does, and it is not reflective.
- Every claim about behaviour is backed by a test name, a command and its
  output, or a file and its line. A number carries how it was obtained.
- A page that describes an API is written after the change that alters it, in the
  same pull request.
- [`docs/adr/`](docs/adr) is append-only: an accepted record is not edited. A
  decision that changes is a new record that supersedes the old one.
- Build the site with the pinned image and check that it reports no errors and no
  warnings before you open the pull request. [Process](docs/dev/process.md) has
  the command and the image tag.

## Developer documentation

[`docs/dev/`](docs/dev/README.md) holds what the manual deliberately does not:

| Document | Contents |
|---|---|
| [`benchmarks.md`](docs/dev/benchmarks.md) | the measurement methodology and every number the manual's Performance page quotes |
| [`evidence.md`](docs/dev/evidence.md) | which test pins which claim, the fixtures, the known defects |
| [`layout.md`](docs/dev/layout.md) | the repository layout, the runtime/generator split, the generated files |
| [`process.md`](docs/dev/process.md) | the project's own CI, the pinned image, coverage, the site build, releases |
| [`decisions.md`](docs/dev/decisions.md) | the history, the decision records, the rejected alternatives, the status of designed work |

[`DESIGN.md`](DESIGN.md) is the design contract the code is held to, and
[`docs/adr/`](docs/adr) holds one decision record per decision, never edited after
acceptance. Where the contract and the manual disagree about what exists, the
code and its tests decide.

## Reporting issues

The tracker is the project's Forgejo:
<https://git.manu343726.com/manu343726/cppgo/issues>.

Include:

- the output of `cppgo version` (or the commit you built from);
- `go version`, the C++ compiler's `--version`, and the libclang major;
- the smallest header that reproduces it, and the exact `cppgo gen` command;
- what you expected and what happened, with the full error text.

For a crash or a fatal error, say whether the process died and what the last
line was: `fatal error: morestack on g0` and a `terminate called` message point
at different mechanisms.
