# Process

How the project builds, checks and publishes itself, and what a contributor has
to know before pushing a branch. Read it when you are adding a step to CI,
changing the toolchain image, touching coverage, building the documentation site,
or asking whether a release can be cut. None of it belongs in the manual: a user
of cppgo never sees a runner, an image tag or a coverage number.

The commands a change is actually held to are in
[`CONTRIBUTING.md`](../../CONTRIBUTING.md); this page is the machinery behind
them and the evidence for each claim.

## The words this page uses

| Term | What it means here |
|---|---|
| **CI** | continuous integration: the service that runs the gate on a pushed branch or a pull request |
| **the gate** | the ordered set of checks a run must pass. A red gate is a tree nobody has seen work |
| **workflow** | one YAML file that declares when CI runs and which steps it runs; its path is `.forgejo/workflows/ci.yml` here, with a copy under `.github/` for the mirror |
| **runner** | the machine the platform assigns to a job. This repository's Forgejo has two, and either may take a job |
| **image, tag, recipe** | a container image is the file system a job runs in. Its **tag** names one published build; here the tag is a **recipe** — the first 12 hexadecimal digits of `sha256(Dockerfile)` — so the same tag always means the same toolchain |
| **`CGO_ENABLED`** | the environment variable that turns cgo on (`1`) or off (`0`). cgo is Go's built-in way to call C, and it needs cgo at build time |
| **coverage, statement coverage** | the fraction of Go statements the tests executed at least once. It is measured with `-coverprofile` and reported as a percentage |
| **coverage profile** | the file `-coverprofile` writes, one line per block of statements with the count of how often it ran; `scripts/coverage-gate.sh` reads it |
| **Hugo** | the static-site generator that builds [`site/`](../../site). The *extended* edition is the one with the SCSS pipeline the site uses |

## The gate

CI runs inside a pinned image, so the toolchain is a reviewed artefact and not
the runner's mood:

```
registry.docker.manu343726.com/homelab/cppgo-ci:recipe-79f9e9ad9528
```

The tag is the recipe's address: `sha256sum Dockerfile | cut -c1-12` prints
`79f9e9ad9528` on this tree. `ghcr.io/manu343726/cppgo-ci` carries the same tag,
so both forges run the same environment. The image pins Go 1.27.3, clang and
libclang 22, the distribution's `g++`, golangci-lint v2.15.0 and Hugo 0.169.0;
[`Dockerfile`](../../Dockerfile) is the only input, and
[`.github/workflows/image.yml`](../../.github/workflows/image.yml) publishes it.

The Forgejo workflow ([`.forgejo/workflows/ci.yml`](../../.forgejo/workflows/ci.yml))
runs, in order:

1. `CGO_ENABLED=0 go build ./...` — the no-cgo contract;
2. `go vet ./...`;
3. `make fmt-check` — every Go file is `gofmt`-clean;
4. `CGO_ENABLED=0 go test -count=1 ./...` — proves the suite needs no cgo;
5. `go test -race -count=1 -covermode=atomic -coverprofile=coverage.out ./...`
   — the race detector, with statement coverage; the race detector needs cgo,
   which the image has on by default;
6. `./scripts/coverage-gate.sh coverage.out 99.0` — the coverage gate;
7. `./scripts/build-examples.sh` — builds, generates and tests the examples;
8. `golangci-lint run --timeout=8m` — the linter, last on purpose: a style
   finding must not hide whether the suite and the number are green.

The workflow also sets `CPPGO_REQUIRE_LIBCLANG=1`: on a developer machine a test
that needs libclang skips itself when libclang is absent, but in CI a skip would
silently drop most of `internal/cxx` from the coverage profile and fail the gate
for a reason nobody can see. With the variable set, the same absence is a
failure that names libclang.

A second job compiles the module with the newest released Go, to catch a
dependency on the pinned version. It compiles and vets; it does not run the
coverage gate.

[`.github/workflows/ci.yml`](../../.github/workflows/ci.yml) runs the same gate on
the GitHub mirror, and [`.github/workflows/image.yml`](../../.github/workflows/image.yml)
publishes the image from the checked-out `Dockerfile`.

## Why two test passes

The runtime must build and test with `CGO_ENABLED=0`; the race detector is built
on cgo and the toolchain rejects `-race` with cgo off. The two properties cannot
be measured by one command, so `make test` runs:

```console
CGO_ENABLED=0 go test -count=1 ./...
CGO_ENABLED=1 go test -race -count=1 ./...
```

Do not "fix" this into a single command. `-count=1` disables Go's test cache, so
a second run actually runs; `-race` is the detector that reports two goroutines
touching the same memory without synchronisation.

## Coverage

The gate is **99%** on `./...`, enforced by
[`scripts/coverage-gate.sh`](../../scripts/coverage-gate.sh) with an
`-covermode=atomic` profile from the cgo-enabled pass. The three exclusions, each
printed by the script with its reason:

- `zz_cppgo*.go` — generator output;
- `internal/gen/testdata/` — generated fixtures;
- `internal/testlib/` — test scaffolding.

`cmd/` is measured like everything else: the CLI is tested through
`run(args, stdout, stderr)`.

The threshold is per module, not per package: the script's `PASS` line prints
the total, and a package below 99% is flagged in its table without failing the
run on its own. A package can therefore sit at 98.6% while the total passes.

## The documentation site

`site/` is a Hugo site built with `hugo --gc --minify` and Hugo 0.169 extended.
`make site` uses the `hugo` on `PATH`; CI and the deploy use the pinned one.

[`.github/workflows/pages.yml`](../../.github/workflows/pages.yml) installs
`hugo_extended_0.169.0_linux-amd64.deb`, builds `site/` and deploys it to GitHub
Pages. The Pages site has to be enabled once by hand (Settings → Pages → Source:
GitHub Actions); the workflow builds either way and skips only the deploy.

The site's source must build with zero errors and zero warnings, and every
internal link — including in-page anchors — must resolve. Check both before a
documentation pull request:

```console
$ docker run --rm --user "$(id -u):$(id -g)" -e HOME=/tmp \
    -v "$PWD/site:/src" -v /tmp/site-out:/out -w /src \
    ghcr.io/manu343726/cppgo-ci:recipe-79f9e9ad9528 hugo --gc --minify --destination /out
```

`--user` and `HOME` keep Hugo's cache out of the container's root-owned home, so
the source tree is not written to. The image tag is the recipe above, not
`latest`: `latest` is for humans reading the registry, and no workflow runs in
it.

## Releases

There are **no tagged releases**: `git tag` prints nothing, and no readiness
question or version criterion is documented yet. `CHANGELOG.md` keeps one
`Unreleased` section; it gains a version on the day the project answers what
makes a trunk releasable and at which level. Until then, a change is consumed at
`main`.

[`.github/workflows/release.yml`](../../.github/workflows/release.yml) is the
mechanism that exists for the day a tag is pushed: a `v*` tag re-runs the tests on
`ubuntu-latest`, cross-builds the CLI for linux and darwin on amd64 and arm64,
writes `SHA256SUMS`, and publishes a GitHub release with generated notes. It
runs on GitHub, not in the pinned image, so its test job installs `g++` and
`libclang-dev` itself.

The command's version is fixed at build time:

```console
$ go build -ldflags "-X main.version=v1.2.3" ./cmd/cppgo
```

## Documentation tooling

The documentation skills under [`.agents/skills/`](../../.agents/skills) are
vendored from upstream, except
[`documentation-manual`](../../.agents/skills/documentation-manual/SKILL.md),
which is written in this repository. The vendored two — `documentation-writer`
and `golang-documentation` — each carry a `PROVENANCE.md` recording the one local
line added to the skill's `SKILL.md`. The local rules file
[`.agents/skills/extra/documentation-rules.md`](../../.agents/skills/extra/documentation-rules.md)
extends them and wins over their text.
