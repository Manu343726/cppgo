# Changelog

Notable changes, in the format of [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

The project has **no tagged release yet** (`git tag` is empty at this commit) and
no documented release procedure, so everything below is unreleased. Pull-request
numbers refer to the project's
[tracker](https://git.manu343726.com/manu343726/cppgo/pulls).

## Unreleased

### Added

- **Header-first generation.** `cppgo gen` reads C++ headers through libclang,
  selects every public declaration, and writes the Go API, one assembly
  trampoline per binding that takes no callback and the C++ shim. The Go
  declaration file is no longer the input (#10, #12).
- **A generator-built shim.** C++ constructs the direct ABI cannot express —
  inline bodies, virtual dispatch, `std::` types, by-value records, templates,
  exception translation — are carried by `libcppgo_<package>.so`, which cppgo
  compiles itself and links against the user's library (#12).
- **`cppgo check`.** Regenerates in memory and exits non-zero when a generated
  file is stale, without writing or compiling (#12).
- **`cppgo build` and the two-phase template flow.** `cppgo gen` writes the
  generic API; `cppgo build -scan <go-package>` collects a program's
  instantiations, emits the C++ specialisations and their thunks, and compiles
  the shim (#27).
- **C++→Go callbacks.** A Go function passed as a function pointer or wrapped in
  a `std::function` is called by C++ through the reentrant carrier, with panics
  captured and re-raised on the Go side (#12).
- **The `-status` convention.** A declaration whose 32-bit return is a status
  code gets a Go signature that returns only an error (#12).
- **`cppgo.OpenFS`.** A shared object embedded with `//go:embed` is extracted to
  a private, content-addressed cache and loaded from there (ADR-0003).
- **The `mathx` and `stats` examples**, each with its own build, generated
  package and tests (#11).
- **The documentation site**, rebuilt along the four Diátaxis sections:
  tutorials, how-to guides, reference and explanation (#31).

### Changed

- **Symbol resolution is cached per `Library`.** A name is resolved through the
  loader once — the address it returned, or the error it reported — so
  `cppgo.LastError` and a generated object's `Close`, which read
  `cppgo_last_error` on every failure, no longer pay a `dlsym` per call. The
  cache is per library and the closed flag is what invalidates it: a lookup
  after `Close` still fails with `cppgo.ErrClosed` and hands out no address
  (#51).
- **The shim belongs to cppgo.** It is generated from the same parse as the Go
  API and compiled by cppgo, so the user's build gains no step and no generated
  file is compiled into the user's library. ADR-0002 supersedes ADR-0001 (#12).
- **The user's library is named by its soname**, so a build directory never
  reaches the shim and an object placed beside it is found through `$ORIGIN`
  (#12).
- **A generated package is movable**: the generator records the library's soname
  and the compiled shim is resolved relative to the package, not to the machine
  that generated it (#12).
- **`DESIGN.md` states the v4 contract**, with each mechanism either naming the
  test that measures it or marked as designed.

### Removed

- **The v3 Go directive DSL as the primary input.** `//cppgo:library`,
  `//cppgo:class`, `//cppgo:method` and friends are no longer how a binding is
  declared (#12).
- **The published v3 documentation.** It described an API that changed under it;
  the site was replaced with a notice (#30) and then rewritten (#31).

### Fixed

- A cache path that is not a private directory is reported as
  `cppgo.ErrInsecureCache` instead of a generic extraction error.
- The error-configuration loader rejects unknown keys and refuses a fallible
  declaration whose convention is neither inferred nor configured.
