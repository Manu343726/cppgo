# Process

How the project builds, checks and publishes itself. Nothing here is part of the
manual.

## The gate

CI runs inside a pinned image, so the toolchain is a reviewed artefact and not
the runner's mood:

```
registry.docker.manu343726.com/homelab/cppgo-ci:recipe-e1efb00b847f
```

The tag is the recipe's address: the first 12 hex digits of `sha256(Dockerfile)`.
`ghcr.io/manu343726/cppgo-ci` carries the same tag, so both forges run the same
environment. The image pins Go 1.27.3, clang/libclang 22 (already at the `FROM`
stage), g++ from the base distribution, golangci-lint v2.15.0 and Hugo 0.169.0.

The Forgejo workflow (`.forgejo/workflows/ci.yml`) runs, in order:

1. `CGO_ENABLED=0 go build ./...` — the no-cgo contract;
2. `go vet ./...` and `make fmt-check`;
3. `CGO_ENABLED=0 go test -count=1 ./...` — proves the suite needs no cgo;
4. `CGO_ENABLED=1 go test -race -count=1 -covermode=atomic ./...` with the
   coverage profile, then `scripts/coverage-gate.sh` at 99%;
5. `make examples` — builds, generates and tests both examples;
6. `golangci-lint run`.

A second job compiles the module with the newest released Go, to catch a
dependency on the pinned version.

`.github/workflows/ci.yml` runs the same gate on the GitHub mirror, and
`image.yml` publishes the image from the checked-out `Dockerfile`.

## Why two test passes

The runtime must build and test with `CGO_ENABLED=0`; the race detector is built
on cgo and the toolchain rejects `-race` with cgo off. The two properties cannot
be measured by one command, so `make test` runs:

```console
CGO_ENABLED=0 go test -count=1 ./...
CGO_ENABLED=1 go test -race -count=1 ./...
```

Do not "fix" this into a single command.

## Coverage

The gate is **99%** on `./...`, enforced by `scripts/coverage-gate.sh` with a
`-covermode=atomic` profile from the cgo-enabled pass. The three exclusions, each
printed by the script with its reason:

- `zz_cppgo*.go` — generator output;
- `internal/gen/testdata/` — generated fixtures;
- `internal/testlib/` — test scaffolding.

`cmd/` is measured like everything else: the CLI is tested through
`run(args, stdout, stderr)`.

## The documentation site

`site/` is a Hugo site built with `hugo --gc --minify` and Hugo 0.169 extended.
`make site` uses the `hugo` on `PATH`; CI and the deploy use the pinned one.

`.github/workflows/pages.yml` installs `hugo_extended_0.169.0_linux-amd64.deb`,
builds `site/` and deploys it to GitHub Pages. The Pages site has to be enabled
once by hand (Settings → Pages → Source: GitHub Actions); the workflow builds
either way and skips only the deploy.

The site's source must build with zero errors and zero warnings, and every
internal link — including in-page anchors — must resolve. Check both before a
documentation pull request:

```console
$ docker run --rm --user "$(id -u):$(id -g)" -e HOME=/tmp \
    -v "$PWD/site:/src" -v /tmp/site-out:/out -w /src \
    ghcr.io/manu343726/cppgo-ci:latest hugo --gc --minify --destination /out
```

## Releases

There are **no tagged releases** (`git tag` is empty) and no release procedure is
documented yet. `CHANGELOG.md` keeps one `Unreleased` section; it gains a version
on the day the project decides what its readiness question and version criterion
are. Until then, a change is consumed at `main`.

The command's version is fixed at build time:

```console
$ go build -ldflags "-X main.version=v1.2.3" ./cmd/cppgo
```

## Documentation tooling

The documentation skills under `.agents/skills/` are vendored from upstream. The
local rules file `.agents/skills/extra/documentation-rules.md` extends them and
wins over their text; each `PROVENANCE.md` records the one local line added to
the skill's `SKILL.md`.
