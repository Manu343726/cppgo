---
title: "Command line"
description: "cppgo gen, build, check, version and help: every flag and exit code."
weight: 10
---

The `cppgo` command has five subcommands. Three of them (`gen`, `build`,
`check`) share one flag set; they differ in what they do with the result.

```console
$ cppgo gen   -header <path> -package <name> [-library <file>] [-out <dir>]
$ cppgo build -header <path> -package <name> -scan <go-package> [-out <dir>]
$ cppgo check -header <path> -package <name> [-library <file>] [-out <dir>]
$ cppgo version
$ cppgo help [command]
```

## Exit codes

| Code | Constant in `cmd/cppgo/cli.go` | Meaning |
|---|---|---|
| `0` | `exitOK` | the command succeeded |
| `1` | `exitError` | generation, or verification, failed |
| `2` | `exitUsage` | the command line itself was wrong |

A wrong command line prints the usage to stderr and exits `2`. A generation
error prints one line naming the file, the line, the declaration and the fix,
and writes no output.

## `cppgo gen`

Writes the Go package, the assembly and the shim, and compiles the shim.

```console
$ cppgo gen -header <path> [-header <path>]... -package <name>
            [-library <file>] [-out <dir>] [-compile-commands <path>]
            [-bind <qualified>]... [-exclude <qualified>]...
            [-instantiate '<specialization>']...
            [-rename '<signature>=<GoName>']... [-status <qualified>]...
            [-cxx <compiler>]
            [-check] [-q]
```

| Flag | Default | Meaning |
|---|---|---|
| `-header <path>` | *required* | a C++ header to bind; repeatable. Each header is a unit of selection |
| `-package <name>` | none | the Go package name. There is no default; pass it on every run |
| `-library <file>` | none | your shared object. The shim is linked against it, and when it is a readable file its dynamic symbol table cross-checks every direct binding. Omit it for a header-only library, whose shim is the whole binary |
| `-out <dir>` | `.` | the output directory |
| `-compile-commands <path>` | discovered | `compile_commands.json`, or its directory, to take the header's flags from |
| `-bind <qualified>` | everything in the header | add a root: a namespace, class, member or function; repeatable |
| `-exclude <qualified>` | none | remove a root; repeatable |
| `-instantiate '<specialization>'` | none | emit a concrete template specialisation, e.g. `mathx::Box<int>`; repeatable. The header must make the instantiation visible, e.g. `template struct Box<int>;` |
| `-rename '<signature>=<GoName>'` | none | resolve a Go name collision; repeatable |
| `-status <qualified>` | none | a declaration whose 32-bit return is a status code: the Go signature returns only an error, and a non-zero value is the failure; repeatable |
| `-cxx <compiler>` | `g++`, then `clang++` | the C++ compiler for the shim |
| `-check` | off | report whether the generated files are up to date instead of writing them |
| `-q` | off | suppress the per-binding report and the confirmation message |
| `-scan`, `-scan-dir` | — | accepted and ignored: they are phase two's input, and `gen` knows no concrete type |

**Output**, under `-out`:

| File | Contents |
|---|---|
| `zz_cppgo.gen.go` | the Go API, the generated `Open` and one declaration per trampoline |
| `zz_cppgo_amd64.s` | one NOSPLIT trampoline per binding |
| `cppgo_shim/zz_cppgo_shim.h` | the `extern "C"` thunk declarations |
| `cppgo_shim/zz_cppgo_shim.cpp` | the thunks |
| `libcppgo_<package>.so` | the shim cppgo compiled and linked against the user's library |

**Report.** On stdout, one line per binding, then the compiler line:

```
mathx::Vector::scale(double) noexcept        direct _ZN5mathx6Vector5scaleEd     noexcept, not virtual, register class; the library exports the symbol
mathx::Vector::norm() const                  thunk  cppgo_thunk_49993cda6560     not noexcept: an exception would unwind into Go
cppgo: built libcppgo_mathx.so with /usr/bin/g++
```

The generated `zz_cppgo.gen.go` carries the exact invocation that produced it in
its banner.

## `cppgo build`

Phase two of the template flow: collect a program's template instantiations and
emit their shim thunks, then compile the shim.

```console
$ cppgo build -header <path> [-header <path>]... -package <name>
              -scan <go-package> [-scan <go-package>]...
              [-scan-dir <dir>] [-library <file>] [-out <dir>]
              [-compile-commands <path>] [-instantiate '<specialization>']...
              [-rename '<signature>=<GoName>']... [-status <qualified>]...
              [-cxx <compiler>] [-q]
```

| Flag | Default | Meaning |
|---|---|---|
| `-scan <package>` | *required* | a Go package pattern whose instantiations to collect; repeatable |
| `-scan-dir <dir>` | `.` | the directory the patterns resolve against |
| the rest | as `gen` | the same manifest flags |

A Go instantiation the shim has no C++ counterpart for fails when it is used,
naming the exact `template ...;` line to add. It never reaches a nil call.

## `cppgo check`

Regenerates the sources in memory and compares them with the files on disk. It
writes nothing and builds nothing.

```console
$ cppgo check -header <path> [-header <path>]... -package <name>
              [-library <file>] [-out <dir>] [-q]
```

It accepts the same manifest flags as `gen`, plus `-scan`/`-scan-dir` when the
package has templates: the shim's source depends on the program's
instantiations, so check compares the shim built for the same scan.

| Exit | Output |
|---|---|
| `0` | `<out>: generated files are up to date` |
| `1` | `cppgo check: <out> is out of date (run "cppgo gen"): <file>` |

## `cppgo version`

Prints `cppgo <version>` and a newline on stdout. The version is set at build
time:

```console
$ go build -ldflags "-X main.version=v1.2.3" ./cmd/cppgo
```

and is `dev` for an unmodified local build.

## `cppgo help`

`cppgo help` prints the top-level usage. `cppgo help <command>` prints that
command's usage. `-h`, `-help` and `--help` do the same wherever they appear in
the arguments, and always exit `0`.

## Related

- What the flags mean in practice: [Generate a binding for your own library]({{< ref "how-to/generate.md" >}}).
- The generated files' contents: [Generated API]({{< ref "reference/generated-api.md" >}}).
