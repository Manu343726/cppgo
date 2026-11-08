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

node "$root/scripts/prose-check-godoc.mjs" "$root" >"$tmp/godoc.md"

inputs=(
	"$root/README.md"
	"$root/DESIGN.md"
	"$root/docs"
	"$root/site/content"
)
while IFS= read -r readme; do
	inputs+=("$readme")
done < <(find "$root/examples" -name README.md -not -path '*/testdata/*' | sort)

exec node "$checker" "${inputs[@]}" "$tmp/godoc.md" "$@"
