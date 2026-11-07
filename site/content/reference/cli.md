+++
title = "The CLI"
description = "cppgo gen, check, version and help: every flag, what goes to stdout and stderr, and the exit code each failure produces."
weight = 30
+++

```console
$ cppgo <command> [flags]
```

| command | does |
|---|---|
| `gen` | reads the package, resolves every binding, writes the generated files |
| `check` | regenerates in memory and compares with disk; writes nothing |
| `version` | prints the generator version |
| `help` | prints the top-level usage, or one command's usage |

`cppgo gen` and `cppgo check` are the same command with a different default for
`-check`: `gen` writes, `check` compares. They accept the same flags.

## `cppgo gen`

```console
$ cppgo gen -dir <pkgdir> [-out <outdir>] [-check] [-q] [-force] [-compile-commands <path>]
```

| flag | default | meaning |
|---|---|---|
| `-dir <dir>` | `.` | package directory holding the `//cppgo:` directives. Required in practice; must exist and be a directory |
| `-out <dir>` | `-dir` | where to write. When it differs from `-dir`, the rewritten source becomes `zz_cppgo_stub.go`; when it is `-dir`, the input is rewritten in place |
| `-check` | `false` | report whether the generated files are up to date instead of writing them |
| `-q` | `false` | suppress the per-binding report and the confirmation message |
| `-force` | `false` | downgrade a validation failure to a warning, for a binding you accept. The generated file records it as `NOT VALIDATED` |
| `-compile-commands <path>` | discovered | a `compile_commands.json`, or the directory holding it, to take the header's flags from instead of discovering them |

Output:

- **stdout** — the per-binding report, one line per binding, unless `-q`.
  Format: qualified C++ name, mangled name, the tier that answered, then any
  notes. See [the report]({{< ref "manual/validation.md" >}}).
- **stderr** — warnings (`cppgo: warning: ...`) and the error that stopped the
  run (`cppgo gen: ...`). Always printed, `-q` or not.

```console
$ cppgo gen -dir ./bindings/mathx
mathx::Vector::Vector       mangled _ZN5mathx6VectorC1Edd      abi from compile_commands.json; note: the object is allocated in Go storage owned by this package (16 bytes, alignment 8) and released by //cppgo:dtor
mathx::Vector::norm         mangled _ZNK5mathx6Vector4normEv    abi from compile_commands.json
```

`-dir` names the package; `-out` names the destination. Passing both the same
value is the common case, and it is what "rewrites in place" means.

## `cppgo check`

```console
$ cppgo check -dir <pkgdir> [-out <outdir>] [-q]
```

Regenerates in memory and compares byte for byte with the files on disk. It
writes nothing.

| result | output | exit |
|---|---|---|
| up to date | `<dir>: generated files are up to date` (suppressed by `-q`) | `0` |
| missing or stale | `<dir> is out of date (run "cppgo gen"): <files>` on stderr | `1` |
| generation error | `cppgo check: <error>` on stderr | `1` |
| bad command line | `cppgo check: ...` plus usage on stderr | `2` |

The comparison covers every file the run would write, including the rewritten
source. That is why editing a body in place without regenerating fails the
check: the body is not what the generator would emit.

`check` also accepts `-check`, `-force` and `-compile-commands` because it is the
same flag set; `-check` is already true.

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
| `1` | error | generation or verification failed: a bad binding, a missing header, a stale file |
| `2` | usage | the command line was wrong: missing command, unknown command, unknown flag, extra argument, empty `-dir`, `-dir` not a directory |

A missing `-dir` directory (as opposed to a path that is a file) exits 1, because
it is an I/O failure rather than a syntax error. `-dir ""` exits 2: the command
line itself asked for something impossible.

## The subcommand dispatch

`main` is one statement — `os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))` — and
`run` carries all the logic, which is what makes the CLI testable: every
subcommand, flag, message and exit code is exercised through the two writers
instead of the process streams.

`cppgo` with no arguments prints `cppgo: missing command` and the usage on
stderr, then exits 2. An unknown command prints `cppgo: unknown command "x"`,
the usage, and exits 2. An extra positional argument after the flags prints
`cppgo gen: unexpected argument "x"` (or `check`), the usage, and exits 2.

## What the generator needs on the machine

- **libclang**, for any package with `//cppgo:header`. Without it the run fails
  with `cxx: libclang not found (...)`; see [installation]({{< ref
  "manual/installation.md" >}}).
- **the header and the sources that include it**, so out-of-line definitions can
  be distinguished from inline ones.
- **no C++ compiler**. It never compiles anything; the `.so` is yours to build.

Next: back to the [manual]({{< ref "manual/_index.md" >}}) or the
[tutorials]({{< ref "tutorials/_index.md" >}}).
