#!/usr/bin/env bash
# Claude Code Stop hook: before the agent finishes a turn that changed Go code,
# compare the code with the plan for the change using "gowl conform", and hand
# anything it finds back to the agent - once. Every finding reported is recorded
# for the session, so none is repeated and the hook cannot keep the agent going
# round in a loop.
#
# The base is where this branch left the default branch, so a plan and code
# committed partway through a branch still count. Set GOWL_DESIGN_BASE to compare
# with another commit.
set -u

input="$(cat)"
here="$(cd "$(dirname "$0")" && pwd)"
root="$(cd "$here/../.." && pwd)"

base="${GOWL_DESIGN_BASE:-}"
if [ -z "$base" ]; then
	for ref in origin/HEAD origin/main origin/master main master; do
		base="$(git -C "$root" merge-base HEAD "$ref" 2>/dev/null)" && [ -n "$base" ] && break
		base=""
	done
fi
[ -n "$base" ] || base=HEAD
if ! git -C "$root" rev-parse --verify --quiet "$base^{commit}" >/dev/null 2>&1; then
	echo "conform-check: $base is not a commit; skipping" >&2
	exit 0
fi

session="$(printf '%s' "$input" | tr -d '\r\n' |
	sed -n 's/.*"session_id"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' | tr -cd 'A-Za-z0-9._-')"
state="${TMPDIR:-/tmp}/gowl-conform"
mkdir -p "$state"
seen="$state/${session:-nosession}.seen"
last="$state/${session:-nosession}.tree"

# Tests and .claude/ are not part of what gocode describes.
code=('*.go' 'go.mod' ':(exclude)*_test.go' ':(exclude).claude')
fingerprint() {
	git -C "$root" diff "$base" -- "$@"
	git -C "$root" ls-files -z --others --exclude-standard -- "$@" |
		while IFS= read -r -d '' f; do
			printf '%s\n' "$f"
			cat "$root/$f"
		done
}
# A turn that only wrote a plan has nothing to compare yet.
if [ "$(fingerprint "${code[@]}" | git hash-object --stdin)" = e69de29bb2d1d6434b8b29ae775ad8c2e48c5391 ]; then
	exit 0
fi
# Leave at once when this code and these plans were checked already.
sum="$(fingerprint "${code[@]}" design | git hash-object --stdin)"
if [ -f "$last" ] && [ "$(cat "$last")" = "$sum" ]; then
	exit 0
fi

# Its own binary: sprawl-check runs at the same moment and builds bin/gowl.
bin="$here/bin/gowl-conform"
case "$(uname -s)" in MINGW* | MSYS* | CYGWIN*) bin="$bin.exe" ;; esac
# The go command's cache makes this cheap when nothing changed. A tree that
# does not build cannot be described either, so a failed build skips the check.
if ! (cd "$root" && go build -o "$bin" ./cmd/gowl) >/dev/null 2>&1; then
	echo "conform-check: the module does not build; skipping" >&2
	exit 0
fi

# From the module root and with no -base unless one was asked for, so that the
# report names the plans and the base the way a person would.
args=()
[ -n "${GOWL_DESIGN_BASE:-}" ] && args=(-base "$GOWL_DESIGN_BASE")
report="$(cd "$root" && "$bin" conform "${args[@]}" -fail-on warning -seen "$seen" 2>&1)"
status=$?
if [ "$status" -gt 1 ]; then
	echo "conform-check: skipping: $(printf '%s' "$report" | head -n 3)" >&2
	exit 0
fi
printf '%s\n' "$sum" >"$last"
[ "$status" -eq 0 ] && exit 0

# Exit status 2 keeps the agent working, with this as its next instruction.
cat >&2 <<EOF
Before you finish: gowl conform compared the code with the plan for this
change, and the two differ.

$report

For each finding, change the code to match the plan. If the plan turned out to
be wrong, change design/*.ofn instead and tell the user what changed and why:
do not edit a plan just to silence a finding. If you stopped partway on purpose,
tell the user what is left. These findings will not be repeated in this session.
EOF
exit 2
