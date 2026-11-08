+++
title = "The CLI"
description = "cppgo gen, check, version and help: every flag the v4 generator accepts, what goes to stdout and stderr, and the exit code each failure produces."
weight = 30
+++

```console
$ cppgo <command> [flags]
```

| command | does |
|---|---|
| `gen` | reads the headers, writes the Go package and the shim sources, compiles the shim |
| `check` | regenerates in memory and compares with disk; writes nothing, builds nothing |
| `version` | prints the generator version |
| `help` | prints the top-level usage, or one command's usage |

`cppgo gen` and `cppgo check` are the same command with a different default for
`-check`: `gen` writes, `check` compares. They accept the same flags.

## `cppgo gen`

```console
$ cppgo gen -header <path> [-header <path>]... -package <name> \
      [-library <file>] [-out <dir>] [-compile-commands <path>] \
      [-bind <qualified>]... [-exclude <qualified>]... \
      [-instantiate '<specialization>']... \
      [-rename '<signature>=<GoName>']... [-status <qualified>]... \
      [-cxx <compiler>] [-check] [-q]
```

| flag | default | meaning |
|---|---|---|
| `-header <path>` | required, repeatable | the C++ header to bind |
| `-package <name>` | | the Go package name |
| `-library <file>` | | your shared object: the shim is linked against it, and a readable file is cross-checked against every direct binding. Omit it for a header-only library |
| `-out <dir>` | `.` | where the Go, assembly and shim sources are written, and where the shim is compiled |
| `-compile-commands <path>` | discovered | a `compile_commands.json` (or its directory) for the header's flags |
| `-bind <qualified>` | every declaration | a root to keep; repeatable |
| `-exclude <qualified>` | | a root to drop; repeatable |
| `-instantiate '<spec>'` | | a concrete template specialisation to emit; repeatable |
| `-rename '<sig>=<GoName>'` | | resolve a Go name collision; repeatable |
| `-status <qualified>` | | a declaration whose 32-bit return is a status code; repeatable |
| `-cxx <compiler>` | `g++`, then `clang++` | the compiler that builds the shim |
| `-check` | `false` | report whether the files are up to date instead of writing |
| `-q` | `false` | suppress the per-binding report and the confirmation message |

Output:

- **stdout** — the per-binding report, one line per binding, then the shim
  build line, unless `-q`. Format. Signature, carrier, symbol, reason. See
  [the report]({{< ref "manual/validation.md" >}}).
- **stderr** — warnings (`cppgo: warning: ...`) and the error that stopped the
  run (`cppgo gen: ...`). Always printed, `-q` or not.

```console
$ cppgo gen -header include/mathx/vector.hpp -package mathx \
      -library build/libmathx.so -out ./mathx
mathx::Vector::scale(double) noexcept        direct _ZN5mathx6Vector5scaleEd     noexcept, out of line, not virtual, register class
mathx::Vector::norm() const                  thunk  cppgo_thunk_49993cda6560     not noexcept: an exception would unwind into Go
cppgo: built mathx/libcppgo_mathx.so with /usr/bin/g++
```

## `cppgo check`

```console
$ cppgo check -header <path> [-header <path>]... -package <name> \
      [-library <file>] [-out <dir>] [-q]
```

Regenerates the sources in memory and compares them byte for byte with the files
on disk. It writes and builds no artefacts.

| result | output | exit |
|---|---|---|
| up to date | `<dir>: generated files are up to date` (suppressed by `-q`) | `0` |
| missing or stale | `<dir> is out of date (run "cppgo gen"): <files>` on stderr | `1` |
| generation error | `cppgo check: <error>` on stderr | `1` |
| bad command line | `cppgo check: ...` plus usage on stderr | `2` |

The comparison covers the generated sources and leaves the compiled
`libcppgo_<package>.so`: the shim is a build artefact, verified by compiling it,
never by comparing bytes.

`check` also accepts `-check`, `-q` and the same manifest flags because it is
the same flag set. `-check` is already true.

## `cppgo version`

```console
$ cppgo version
cppgo dev
```

`-v` and `--version` are aliases. The value is set at build time:

```console
$ go build -ldflags "-X main.version=v1.2.3" ./cmd/cppgo
```

`dev` is what an unmodified local build reports.

## `cppgo help`

```console
$ cppgo help            # top-level usage
$ cppgo help gen        # gen's usage
$ cppgo help check      # check's usage
$ cppgo help version    # version's usage
$ cppgo help help       # top-level usage
```

`-h`, `-help` and `--help` are accepted anywhere in the arguments of `gen` and
`check` (and as the top-level command) and are checked **before** flag parsing,
so help always goes to stdout and exits 0, while a malformed command goes to
stderr and exits 2.

## Exit codes

| code | name | when |
|---|---|---|
| `0` | ok | the command succeeded; for `check`, everything is up to date |
| `1` | error | generation or verification failed: a refused construct, a missing header, a shim that does not compile, a stale file |
| `2` | usage | the command line was wrong: missing command, unknown command, unknown flag, extra argument, empty `-out`, missing `-header` |

`cppgo` with no arguments prints `cppgo: missing command` and the usage on
stderr, then exits 2. An unknown command prints `cppgo: unknown command "x"`,
the usage, and exits 2. An extra positional argument after the flags prints
`cppgo gen: unexpected argument "x"` (or `check`), the usage, and exits 2.

## What the generator needs on the machine

- **libclang**, always. Without it the run fails with
  `cxx: libclang not found (...)`. See [installation]({{< ref
  "manual/installation.md" >}}).
- **a C++ compiler** for `gen`, because `gen` compiles the shim. `check` does
    not need one. It writes and builds no artefacts.
- **the headers**, and a compilation database when they have includes of their
  own.

Next. Back to the [manual]({{< ref "manual/_index.md" >}}) or the
[tutorials]({{< ref "tutorials/_index.md" >}}).
