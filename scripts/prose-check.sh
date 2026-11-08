#!/usr/bin/env bash
# Score this repository's prose with the vendored technical-prose-style checker.
#
#   ./scripts/prose-check.sh [checker flags]
#
# Inputs:
#   README.md, DESIGN.md
#   docs/**/*.md
#   site/content/**/*.md
#   examples/**/README.md
#   godoc: package comments and exported declarations extracted from every
#          non-test .go file into markdown, with path:line markers
#
# Excluded, each for a stated reason:
#   .agents/            vendored upstream skill; not this project's prose
#   **/testdata/        generated fixtures, not hand-written prose
#   all *_test.go       godoc publishes no test files
#   Hugo frontmatter    the +++ block is page metadata and listing copy, not
#                       page prose; the checker only knows YAML frontmatter, so
#                       this wrapper strips the TOML block before scoring
#
# Node is the only requirement. The checker exits non-zero on a fail threshold.

set -euo pipefail

root=$(cd "$(dirname "$0")/.." && pwd)
checker="$root/.agents/skills/technical-prose-style/scripts/prose-check.mjs"

if ! command -v node >/dev/null 2>&1; then
	printf 'prose-check: node is required (the checker is a Node script)\n' >&2
	exit 2
fi
if [ ! -f "$checker" ]; then
	printf 'prose-check: %s is missing; the vendored skill is not in this tree\n' "$checker" >&2
	exit 2
fi

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

# Copy every markdown input into the mirror with its Hugo frontmatter removed,
# preserving the relative path so a finding still names the real page.
mirror="$tmp/tree"
mkdir -p "$mirror"
stage() {
	local src=$1 rel=${1#"$root"/}
	mkdir -p "$mirror/$(dirname "$rel")"
	perl -0777 -pe 's/\A\+\+\+\n.*?\n\+\+\+\n//s' "$src" >"$mirror/$rel"
}

stage "$root/README.md"
stage "$root/DESIGN.md"
while IFS= read -r file; do stage "$file"; done < <(find "$root/docs" "$root/site/content" -name '*.md' | sort)
while IFS= read -r readme; do stage "$readme"; done < <(find "$root/examples" -name README.md -not -path '*/testdata/*' | sort)

node "$root/scripts/prose-check-godoc.mjs" "$root" >"$tmp/godoc.md"

set +e
node "$checker" "$mirror" "$tmp/godoc.md" "$@" >"$tmp/out" 2>&1
status=$?
set -e
sed "s|$mirror|$root|g; s|$tmp/godoc.md|(godoc extracted from .go files)|g" "$tmp/out"
exit "$status"
