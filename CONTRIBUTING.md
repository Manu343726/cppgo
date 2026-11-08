# Contributing to cppgo

Changes are welcome. This page is what you need to build the project, run what CI
runs, and know which rules are not negotiable.

## Prerequisites

| Tool | Version | Why |
|---|---|---|
| Go | 1.25 or newer | `go.mod` declares `go 1.25.0` |
| A C++ compiler | C++17; `g++` or `clang++` | the examples, and the shim cppgo compiles |
| libclang | **20 or newer** | `cppgo gen` and `cppgo check` parse headers with it, and `clang_getOffsetOfBase` is not exported before 20 |
| `make` | any | every target below |
| Docker | optional | to build the pinned CI image, or to run the site with the same Hugo |
| Hugo extended | 0.169 | only to build `site/` outside the image |

Debian and Ubuntu need libclang from [apt.llvm.org](https://apt.llvm.org/): the
distribution packages stop at 19, which cannot bind `clang_getOffsetOfBase` and
fails at load time with an undefined symbol. Installing `clang` alone is not
enough; the project needs `libclang-<major>-dev` and the matching `clang-<major>`.

## Quick start

```console
$ git clone ssh://git@git.manu343726.com:2222/manu343726/cppgo.git
$ cd cppgo

$ make build        # CGO_ENABLED=0 go build ./..., then cmd/ binaries into bin/
$ make test         # two passes: no-cgo, then -race
$ make lint         # go vet, then golangci-lint when installed
$ make examples     # build, generate and test examples/mathx and examples/stats
$ make cover-gate   # statement coverage >= 99%
$ make site         # the documentation site (needs hugo, or use the image)
```

`make verify` runs build, lint, test, coverage and examples in one shot; it is
the closest thing to a single CI command.

`make test` runs **two passes on purpose**. The first is
`CGO_ENABLED=0 go test -count=1 ./...` and proves the no-cgo property. The second
is `CGO_ENABLED=1 go test -race -count=1 ./...`, because the race detector is
built on cgo and the toolchain rejects `-race` with cgo off. Do not "fix" this
into one command; the two properties cannot both be measured that way.

The `site` target uses the `hugo` on your `PATH`. The CI image has the pinned one:

```console
$ docker run --rm -v "$PWD/site:/src" -w /src \
    ghcr.io/manu343726/cppgo-ci:latest hugo --gc --minify --destination /tmp/out
```

## The rules that are not negotiable

- **The runtime never imports the generator.** The root package (and everything
  it imports) must stay free of `internal/cxx` and libclang; a test fails when the
  dependency appears. libclang is a generation-time dependency only.
- **The runtime builds and its tests run with `CGO_ENABLED=0`.** No change may
  introduce cgo into the runtime or into generated code.
- **A generated file is never edited by hand.** `zz_cppgo*.go`, `zz_cppgo_amd64.s`
  and `cppgo_shim/` are output. Change the emitter, regenerate, and commit the
  result. The examples commit their generated sources, so `cppgo check` catches a
  hand edit.
- **The user's build never compiles a generated file.** The shim is cppgo's, and
  it links *against* the user's library. The shim tests build the two as
  separate objects and assert which one exports what: an inline body is absent
  from the user's library and present in the shim.
- **A construct is either emitted or refused.** There is no pass-through guess.
  `rejectUnimplemented` in `internal/gen/reject.go` is the single list of what
  the build does not emit, and each entry names the declaration and the fix.

## Where things live

| Area | Path |
|---|---|
| runtime the generated code imports | repository root (`cppgo.go`, `loadfs.go`, `object.go`, `reentrant.go`, `errors.go`, `callerror.go`) |
| call trampolines and the stack probe | `internal/asm` |
| C++→Go callbacks | `internal/callback` |
| libclang model | `internal/cxx` |
| selection, carriers, emitters, shim build | `internal/gen` |
| error projection (not consumed by the generator yet) | `internal/errormap` |
| construct matrix against a real compiler | `internal/shim` |
| command line | `cmd/cppgo` |
| executable examples | `examples/` |
| design contract and decisions | `DESIGN.md`, `docs/adr/` |
| documentation site sources | `site/` |

## Tests

- Table-driven, with `t.Run` per case; `testify`'s `require` and `assert`.
- `t.Parallel()` where the cases are independent.
- Concurrency tests run under `-race` and `GOMAXPROCS=1` with at least 32
  goroutines.
- A behaviour that the documentation claims needs a test. If you cannot test it,
  say so in the pull request instead of claiming it.
- When you add a construct to the emitter, add both: a golden file under
  `internal/gen/testdata/golden/` and an end-to-end test that compiles the shim
  with a real compiler and calls the result. The shim's compilation is the gate
  that a model alone cannot give you.

The coverage gate is **≥ 99%** on `./...`, measured by
`./scripts/coverage-gate.sh`. Its exclusions are the three it prints, each with
its reason: generated output (`zz_cppgo*.go`), the generated fixtures under
`internal/gen/testdata/`, and `internal/testlib/`. No hand-written production
package is excluded.

## Branches, commits and pull requests

- Branch from `main` and name the branch after the issue: `dev/<issue>`.
- One deliverable per pull request. A service-like change does not split across
  two PRs; a refactor with no behaviour change does not ride along with a fix.
- Use Conventional Commits (`feat(gen): …`, `fix(runtime): …`, `docs(site): …`).
  The body says what was measured, with the command and the result.
- Never merge your own pull request, and never push to `main`.

## Documentation changes

- The site lives in `site/` and follows four sections: tutorials, how-to guides,
  reference and explanation. A new page belongs to exactly one of them.
- Every claim about behaviour is backed by a test name, a command and its
  output, or a file and its line. A number carries how it was obtained.
- A page that describes an API is written after the change that alters it, in
  the same pull request.
- `docs/adr/` is append-only: an accepted record is not edited. A decision that
  changes is a new record that supersedes the old one.
- Build the site with the pinned image and check that it reports no errors and no
  warnings before you open the pull request.

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
