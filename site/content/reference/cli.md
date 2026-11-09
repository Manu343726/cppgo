---
title: "Command line"
description: "cppgo gen, build, check, version and help: every flag and exit code."
weight: 10
---

The `cppgo` command has five subcommands. Three of them (`gen`, `build`,
`check`) share one flag set; they differ in what they do with the result.

`cppgo` reads a C++ **header** — the `.hpp` file that declares a library's
classes and functions — and writes a Go package that calls the compiled library.
The compiled library is a **shared object**: a `.so` file on Linux, which the
system loader maps into the process at run time. The C++ that a direct call
cannot carry is compiled into a second shared object, the **shim**, which
`cppgo` generates and builds. A **binding** is one C++ declaration exposed as a
Go identifier.

The block below is the usage `cppgo help` prints. An angle-bracketed word stands
for a value you supply; each subcommand's own heading has a command you can run
as written.

```console
$ cppgo gen   -header <path> -package <name> [-library <file>] [-out <dir>]
$ cppgo build -header <path> -package <name> -scan <go-package> [-out <dir>]
$ cppgo check -header <path> -package <name> [-library <file>] [-out <dir>]
$ cppgo version
$ cppgo help [command]
```

## Exit codes

| Code | Meaning |
|---|---|
| `0` | the command succeeded |
| `1` | generation, verification, or the build of the shim failed |
| `2` | the command line itself was wrong |

A wrong command line prints the usage to stderr and exits `2`. An error prints
one line naming the problem and the fix — for a refused declaration, the file,
the line and the declaration — and a failed generation writes no output. The
per-binding report and the success text go to stdout; every diagnostic goes to
stderr.

## `cppgo gen`

Reads the headers, writes the Go package and the C++ side, and builds the shim.

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
| `-header <path>` | *required* | a C++ header to read; repeatable. Every public declaration in the header is bound unless `-bind` or `-exclude` narrows the set |
| `-package <name>` | none | the Go package name. There is no default: a missing or invalid name is an error |
| `-library <file>` | none | your shared object. The shim is linked against it, and when it is a readable file cppgo checks that every function it binds directly is exported by it — that is, listed in its dynamic symbol table, the names the loader can resolve. Omit it for a header-only library |
| `-out <dir>` | `.` | the output directory |
| `-compile-commands <path>` | discovered | `compile_commands.json`, or the directory that holds it, to take the header's flags from. That file is the **compilation database**: it records the compiler flags each source file of the project is built with |
| `-bind <qualified>` | everything in the header | add a root: the namespace, class, member or function to start selecting from; repeatable. A root is one declaration and everything below it |
| `-exclude <qualified>` | none | remove a root; repeatable. An entry that matches nothing is an error |
| `-instantiate '<specialization>'` | none | emit a concrete template specialization: a template with its type argument supplied, e.g. `metrics::Accumulator<double>`; repeatable. The header must make the specialization visible, e.g. `template class ::metrics::Accumulator<double>;` |
| `-rename '<signature>=<GoName>'` | none | resolve a Go name collision; repeatable |
| `-status <qualified>` | none | a declaration whose 32-bit return is a status code — `0` means success and any other value a failure: the Go signature returns only an error, and a non-zero value becomes it; repeatable |
| `-cxx <compiler>` | `g++`, then `clang++` | the C++ compiler for the generated side |
| `-check` | off | report whether the generated files are up to date instead of writing them |
| `-q` | off | suppress the per-binding report and the confirmation message |
| `-scan`, `-scan-dir` | — | accepted and ignored: they are `build`'s input, and `gen` writes no concrete template code |

**Output**, under `-out`:

| File | What it is |
|---|---|
| `zz_cppgo.gen.go` | the Go API and the code that loads the C++ side |
| `zz_cppgo_amd64.s` | the assembly cppgo generated for the calls: one trampoline per direct binding, the function that moves the arguments into the C registers and calls the resolved address |
| `cppgo_shim/zz_cppgo_shim.h` | declarations for the C++ file |
| `cppgo_shim/zz_cppgo_shim.cpp` | the C++ source cppgo writes |
| `libcppgo_<package>.so` | the shim cppgo builds and links against yours |

The first four are sources to commit; the `.so` is a build output. What each one
contains is in [Architecture]({{< ref "explanation/architecture.md" >}}#the-generated-files).

**Report.** On stdout, one line per binding, then the compiler line:

```
mathx::Vector::norm() const                  thunk  cppgo_thunk_49993cda6560     not noexcept: an exception would unwind into Go
mathx::Vector::scale(double) noexcept        direct _ZN5mathx6Vector5scaleEd     noexcept, not virtual, register class; the library exports the symbol
cppgo: built libcppgo_mathx.so with /usr/bin/g++
```

The four columns are the C++ declaration, its **carrier**, the symbol the call
resolves, and why that carrier was chosen. A carrier is the path one call takes
into C++: `direct` calls the **mangled symbol** — the name the C++ compiler
stores for a declaration, encoding its namespace, class and parameter types —
through the generated assembly; `thunk` calls a generated `extern "C"` wrapper
compiled into the shim; `reentrant` is the path that lets C++ call a Go function
back, and a call that passes no Go function never needs it. The last column is
the reason: `not noexcept` means the declaration may throw a C++ exception,
which cannot unwind through a Go frame, so the shim catches it and returns a
status instead. Check the compiler line against the compiler that built your
library.

The generated `zz_cppgo.gen.go` carries the exact invocation that produced it in
its banner.

Run from [`examples/mathx`](https://github.com/Manu343726/cppgo/tree/main/examples/mathx)
after `make lib` builds its shared object, with `cppgo` on `PATH`:

```console
$ cppgo gen -header include/mathx/vector.hpp -package mathx \
            -library build/libmathx.so -out .
```

The three lines above are the report of that command.

## `cppgo build`

Phase two of the template flow. A C++ **template** is a recipe for a family of
classes or functions; the C++ compiler writes the real one only where a program
supplies the type argument, and that written-out class or function is a
**specialization**. `cppgo gen` writes the Go API once, as a **generic** one: Go
declarations that take the type argument as a Go type parameter. `cppgo build`
reads the Go program, collects the type arguments it uses, and adds the matching
C++ specializations before it compiles the shim.

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
| `-scan <package>` | none | a Go package pattern — a path with `...` wildcards, as the `go` command accepts — whose uses of the generated generic API are collected; repeatable. The source is read, not run. Without it, `build` writes the generic API and adds no specialization |
| `-scan-dir <dir>` | `.` | the directory the patterns resolve against |
| the rest | as `gen` | the same manifest flags |

`cppgo help build` prints `-scan` without brackets, but the command does not
refuse a run without it: it writes the generic API and collects no type
argument. Pass the program's packages so the shim gets its specializations.

If the program uses an instantiation the shim has no specialization for, the
call returns an error naming the exact `template ...;` line to add; it never
reaches a nil call.

Run from [`examples/full`](https://github.com/Manu343726/cppgo/tree/main/examples/full)
after `make lib`, with `cppgo` on `PATH`:

```console
$ cppgo build -header include/metrics/metrics.hpp -package metrics \
              -library build/libmetrics.so \
              -compile-commands build/compile_commands.json \
              -out ./gen -status metrics::validate_name \
              -scan ./cmd/... -scan-dir .
```

The report gains one line per collected specialization, with the C++ line cppgo
derives for it; the walk through it is in
[Bind a C++ template library]({{< ref "how-to/templates.md" >}}).

## `cppgo check`

Regenerates the sources in memory and compares them with the files on disk. It
writes nothing and builds nothing.

```console
$ cppgo check -header <path> [-header <path>]... -package <name>
              [-library <file>] [-out <dir>]
```

It accepts the same manifest flags as `gen`, plus `-scan`/`-scan-dir` when the
package has templates: the generated C++ depends on the program's
instantiations, so check compares like with like.

| Exit | Output |
|---|---|
| `0` | `<out>: generated files are up to date` |
| `1` | `cppgo check: <out> is out of date (run "cppgo gen"): <file>` |

The same command as the `build` example, with `check` in place of `build`, makes
a CI gate:

```console
$ cppgo check -header include/metrics/metrics.hpp -package metrics \
              -library build/libmetrics.so \
              -compile-commands build/compile_commands.json \
              -out ./gen -status metrics::validate_name \
              -scan ./cmd/... -scan-dir .
./gen: generated files are up to date
```

## `cppgo version`

Prints `cppgo <version>` and a newline on stdout. `cppgo -v` and `cppgo
--version` are the same command. The version is fixed when the command is built,
with `-ldflags "-X main.version=<version>"`, and is `dev` for an unmodified
local build.

## `cppgo help`

`cppgo help` prints the top-level usage. `cppgo help <command>` prints that
command's usage; `<command>` is one of `gen`, `build`, `check`, `version` and
`help`. `-h`, `-help` and `--help` do the same after a subcommand, and always
exit `0`. An unknown topic prints `cppgo: unknown command "<topic>"` and the
top-level usage to stderr, and exits `2`.

## Related

- What the flags mean in practice: [Generate a binding for your own library]({{< ref "how-to/generate.md" >}}).
- The generated package's shape: [Generated API]({{< ref "reference/generated-api.md" >}}).
- How a call reaches C++, and what each carrier costs: [Carriers and the construct table]({{< ref "explanation/carriers.md" >}}).
