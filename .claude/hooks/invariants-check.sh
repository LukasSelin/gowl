#!/usr/bin/env bash
# Claude Code Stop hook: before the agent finishes a turn that changed Go code
# or the invariants themselves, check the code against design/invariants.ofn
# with "gowl invariants", and hand anything it finds back to the agent - once.
# Every finding reported is recorded for the session, so none is repeated and
# the hook cannot keep the agent going round in a loop.
#
# Unlike a plan, the invariants are never retired, so a change to them alone is
# checked too: change the rule first, and see at once what the code says.
#
# The base is where this branch left the default branch, as for conform-check.
# Set GOWL_DESIGN_BASE to compare with another commit.
set -u

input="$(cat)"
here="$(cd "$(dirname "$0")" && pwd)"
root="$(cd "$here/../.." && pwd)"

[ -f "$root/design/invariants.ofn" ] || exit 0

base="${GOWL_DESIGN_BASE:-}"
if [ -z "$base" ]; then
	for ref in origin/HEAD origin/main origin/master main master; do
		base="$(git -C "$root" merge-base HEAD "$ref" 2>/dev/null)" && [ -n "$base" ] && break
		base=""
	done
fi
[ -n "$base" ] || base=HEAD
if ! git -C "$root" rev-parse --verify --quiet "$base^{commit}" >/dev/null 2>&1; then
	echo "invariants-check: $base is not a commit; skipping" >&2
	exit 0
fi

session="$(printf '%s' "$input" | tr -d '\r\n' |
	sed -n 's/.*"session_id"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' | tr -cd 'A-Za-z0-9._-')"
state="${TMPDIR:-/tmp}/gowl-invariants"
mkdir -p "$state"
seen="$state/${session:-nosession}.seen"
last="$state/${session:-nosession}.tree"

# The code gocode describes, and the invariants. Tests and .claude/ are not
# part of what gocode describes.
paths=('*.go' 'go.mod' 'design/invariants.ofn' ':(exclude)*_test.go' ':(exclude).claude')
fingerprint() {
	git -C "$root" diff "$base" -- "${paths[@]}"
	git -C "$root" ls-files -z --others --exclude-standard -- "${paths[@]}" |
		while IFS= read -r -d '' f; do
			printf '%s\n' "$f"
			cat "$root/$f"
		done
}
sum="$(fingerprint | git hash-object --stdin)"
# Leave at once when neither changed (git's id for empty content), or when
# these exact changes were checked already.
if [ "$sum" = e69de29bb2d1d6434b8b29ae775ad8c2e48c5391 ] ||
	{ [ -f "$last" ] && [ "$(cat "$last")" = "$sum" ]; }; then
	exit 0
fi

# Its own binary: the other Stop hooks build theirs at the same moment.
bin="$here/bin/gowl-invariants"
case "$(uname -s)" in MINGW* | MSYS* | CYGWIN*) bin="$bin.exe" ;; esac
# A tree that does not build cannot be described, so a failed build skips the
# check.
if ! (cd "$root" && go build -o "$bin" ./cmd/gowl) >/dev/null 2>&1; then
	echo "invariants-check: the module does not build; skipping" >&2
	exit 0
fi

report="$(cd "$root" && "$bin" invariants -fail-on warning -seen "$seen" 2>&1)"
status=$?
if [ "$status" -gt 1 ]; then
	echo "invariants-check: skipping: $(printf '%s' "$report" | head -n 3)" >&2
	exit 0
fi
printf '%s\n' "$sum" >"$last"
[ "$status" -eq 0 ] && exit 0

# Exit status 2 keeps the agent working, with this as its next instruction.
cat >&2 <<EOF
Before you finish: gowl invariants checked the code against
design/invariants.ofn, the rules it keeps on every change, and some do not hold.

$report

For each finding, change the code so the rule holds. If the rule itself is
wrong, change design/invariants.ofn and tell the user which rule changed and
why: do not weaken a rule just to silence a finding. These findings will not be
repeated in this session.
EOF
exit 2
