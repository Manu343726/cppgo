# mathx, the direct ABI mode on one small class

`mathx::Vector` is a C++17 class with a constructor, a destructor, two const
accessors, a const method that takes a slice, a mutator and a static member. All
of them are called from Go through generated NOSPLIT trampolines, with no
`extern "C"` layer in between and no cgo at build time.

One command builds and proves it:

```console
$ cd examples/mathx
$ make
```

That compiles `build/libmathx.so`, regenerates the bindings with `cppgo gen`,
runs `go test` and then `go vet`. The interesting part of the output is the
generator's report and the test lines:

```
mathx::Vector::Vector              mangled _ZN5mathx6VectorC1Edd                          abi from compile_commands.json; note: the object is allocated in Go storage owned by this package (16 bytes, alignment 8) and released by //cppgo:dtor
mathx::Vector::dims                mangled _ZN5mathx6Vector4dimsEv                        abi from compile_commands.json
mathx::Vector::dot                 mangled _ZNK5mathx6Vector3dotEPKdm                     abi from compile_commands.json
mathx::Vector::norm                mangled _ZNK5mathx6Vector4normEv                       abi from compile_commands.json
mathx::Vector::scale               mangled _ZN5mathx6Vector5scaleEd                       abi from compile_commands.json
mathx::Vector::x                   mangled _ZNK5mathx6Vector1xEv                          abi from compile_commands.json
mathx::Vector::y                   mangled _ZNK5mathx6Vector1yEv                          abi from compile_commands.json
mathx::Vector::~Vector             mangled _ZN5mathx6VectorD1Ev                           abi from compile_commands.json
...
--- PASS: TestConstructorAndAccessors (0.00s)
--- PASS: TestConstMethod (0.00s)
--- PASS: TestMethodTakingSlice (0.00s)
--- PASS: TestMethodMutatesObject (0.00s)
--- PASS: TestObjectsAreIndependent (0.00s)
--- PASS: TestDestroyReleasesStorage (0.00s)
PASS
ok  	github.com/Manu343726/cppgo/examples/mathx	0.015s
```

No line in that report is hand-written: `cppgo gen` read the header through
libclang, resolved each declaration to its mangled name, and refused to emit a
trampoline it could not prove would link. `abi from compile_commands.json` is the
primary tier. The flags came from this project's own compilation database, and
the parse used the same `-std=c++17 -fPIC -I…` a real build uses.

## What this example teaches

| what | where |
|---|---|
| the whole binding is one Go file with `//cppgo:` directives | [`mathx.go`](mathx.go) |
| C++ members must be defined **out of line** to have a symbol | [`src/vector.cpp`](src/vector.cpp) |
| an inline member is refused at build time, not at run time | `make inline-demo` |
| the method trampoline: arguments in registers, result copied back | [`zz_cppgo_amd64.s`](zz_cppgo_amd64.s) |
| the object is Go-allocated, sized by libclang, freed by the dtor | [`zz_cppgo.gen.go`](zz_cppgo.gen.go) |
| generated files are checked in and verifiable | `make check` |

## Layout

```
examples/mathx/
├── include/mathx/vector.hpp   the class: declarations only
├── src/vector.cpp             every member defined out of line
├── mathx.go                   the //cppgo: directives (rewritten in place by gen)
├── mathx_test.go              the assertions, including the lifetime check
├── zz_cppgo.gen.go            generated: wrappers, lazy binding, Go side of the ABI
├── zz_cppgo_amd64.s           generated: one trampoline per symbol
├── Makefile                   builds the .so, runs gen, test and vet
├── go.mod                     its own module, replace ../..
└── build/                     ignored: libmathx.so, the cppgo binary, the CDB
```

The two `zz_cppgo*` files are generated, and they are committed so the example
builds without running the generator first. `make` regenerates them anyway (they
come out byte-identical), and `make check` is the way to prove they are current:

```console
$ make check
build/cppgo check -dir . -compile-commands /…/build/compile_commands.json
.: generated files are up to date
```

## The binding, in full

`mathx.go` is the entire interface. There is no `import "C"`, no unsafe, no
assembly to write and no C++ glue file:

```go
//cppgo:library libmathx
//cppgo:header include/mathx/vector.hpp

//cppgo:class mathx::Vector
type Vector cppgo.Handle

//cppgo:ctor
func New(x, y float64) (Vector, error)

//cppgo:method norm
func (v Vector) Norm() float64

//cppgo:method dot
func (v Vector) Dot(ys []float64) float64

//cppgo:dtor
func (v Vector) Destroy()

//cppgo:function mathx::Vector::dims
func Dims() int32
```

`//cppgo:class` on a `cppgo.Handle` makes the receiver *be* the object
pointer: `Vector` is a `uintptr`. There is no struct, no field to remember and
nothing for the collector to trace. `//cppgo:method` resolves against that class,
`//cppgo:ctor` builds one, `//cppgo:dtor` destroys it, and `//cppgo:function`
takes a qualified name for everything with no `this`, a free function or, as
here, a static member.

## What the generator produced

One `NOSPLIT` trampoline per bound symbol: `this` in `RDI`, a slice as
`RSI`/`RDX`, the `double` back in `XMM0`. The file is checked in at
[`zz_cppgo_amd64.s`](zz_cppgo_amd64.s), and the [generated-files
reference](https://manu343726.github.io/cppgo/reference/generated-code/)
documents it line by line.

The wrapper around it is ordinary Go. A `sync.Once` for the `dlopen`, a
`ProbeStack()` for the stack headroom the foreign frame may need, the call, then
`cppgo.KeepAlive(ys)` so the backing array is still there while C++ is reading
it.

The constructor is the one place with real work on the Go side. libclang was
asked for `sizeof`/`alignof` (16/8 for two doubles), the wrapper allocates that
much Go memory, records the handle in a package-level registry so the collector
cannot reclaim it, and passes the address as the first integer argument. The
destructor calls `~Vector` and drops the registry entry, which is why
`TestDestroyReleasesStorage` can assert the count goes back down.

## The limitation, demonstrated

`norm_squared()` is defined *inside* the class body in the header. It is
implicitly inline and the shared object exports no symbol for it. A reader need not take this on faith. The example ships a target that binds it
and shows the refusal:

```console
$ make inline-demo
cppgo gen refused it, as it must:
  cppgo gen: build/inline-demo/mathx.go:82: mathx::Vector::norm_squared: it is defined inside its class definition or has internal linkage, so it is implicitly inline and the library exports no symbol for it: move the definition out into a source file and rebuild, or bind it through the shim mode
```

That is the whole value proposition of the ABI mode in one line. The alternative
would be a trampoline that compiles, links, and dies on the first call when
`dlsym` answers nothing. The scratch copy lives under `build/inline-demo/`. The
real package is untouched. The long form of this failure, and its three
fixes, is in the site's [when the generator
refuses](https://manu343726.github.io/cppgo/tutorials/when-generation-refuses/)
page.

The same rule rejects the other shapes the direct ABI mode cannot express. A
virtual function, a template without an explicit instantiation, a `std::` type
by value, a struct parameter passed by value, and more than 6 integer or 8 float
argument registers.

## How the generator finds the definitions

**`src/vector.cpp` includes `"vector.hpp"`, not `<mathx/vector.hpp>`.** cppgo
discovers the translation units that hold the bodies by looking for a source
that includes the header *by file name*. That is why `include/mathx` is on the
include path in the Makefile (`-Iinclude -Iinclude/mathx`) and why the include is
spelled the short way. A `.cpp` that only says `#include <mathx/vector.hpp>`
makes every member look inline, and the failure is the message above.

Which sources are parsed, and in what order, is in [validation and the
report](https://manu343726.github.io/cppgo/manual/validation/).

## Requirements

- Go 1.25 or newer (`go.mod` pins it. This repo is developed on 1.27).
- A C++17 compiler (`g++` or `clang++`, override with `make CXX=clang++`).
- libclang, because `cppgo gen` parses the header with it. On Debian/Ubuntu:
  `sudo apt install clang libclang-dev`. On Arch: `sudo pacman -S clang`.

The example builds its own `cppgo` binary from the repository root into
`build/cppgo`, the same code as `go install …/cmd/cppgo@latest` would, without
requiring the install and without any chance of testing a different version.

## Where the shared library comes from at run time

`//cppgo:library libmathx` names the library, and the generated bindings call
`dlopen("libmathx")`, which searches the loader's paths. **not** the current
directory. A program that uses this package therefore has to tell it where the
file is, in one of two ways:

```go
// before the first call: the generated variable exists for exactly this
mathx.CppgoLibraryPath = "/opt/mathx/lib/libmathx.so"
```

or by leaving the name alone and putting the file where the loader looks:

```console
$ LD_LIBRARY_PATH=build go run .
```

The test does the first (see `init` in [`mathx_test.go`](mathx_test.go)), which
is also why the library can be rebuilt and swapped without recompiling Go.

## Other targets

```console
$ make test        # go test only (builds the library and regenerates first)
$ make vet         # go vet, which also checks the generated assembly declarations
$ make check       # cppgo check: are the committed generated files up to date?
$ make inline-demo # the refusal above
$ make clean
```
