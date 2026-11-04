#!/usr/bin/env bash
# Claude Code Stop hook: before the agent finishes a turn that changed Go code,
# compare the module's exported types with the base commit using "gowl sprawl",
# and hand anything it finds back to the agent - once. Every finding reported
# is recorded for the session, so none is repeated and the hook cannot keep the
# agent going round in a loop.
#
# The base is HEAD, so the check covers uncommitted work. Set GOWL_SPRAWL_BASE
# to another commit, such as origin/main, to cover a whole branch.
set -u

input="$(cat)"
here="$(cd "$(dirname "$0")" && pwd)"
root="$(cd "$here/../.." && pwd)"
base="${GOWL_SPRAWL_BASE:-HEAD}"

if ! git -C "$root" rev-parse --verify --quiet "$base^{commit}" >/dev/null 2>&1; then
	echo "sprawl-check: $base is not a commit; skipping" >&2
	exit 0
fi

session="$(printf '%s' "$input" | tr -d '\r\n' |
	sed -n 's/.*"session_id"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' | tr -cd 'A-Za-z0-9._-')"
state="${TMPDIR:-/tmp}/gowl-sprawl"
mkdir -p "$state"
seen="$state/${session:-nosession}.seen"
last="$state/${session:-nosession}.tree"

# Fingerprint the Go changes since the base: tracked diffs and untracked files.
# Tests and .claude/ are not part of what gocode describes.
paths=('*.go' 'go.mod' ':(exclude)*_test.go' ':(exclude).claude')
fingerprint() {
	git -C "$root" diff "$base" -- "${paths[@]}"
	git -C "$root" ls-files -z --others --exclude-standard -- "${paths[@]}" |
		while IFS= read -r -d '' f; do
			printf '%s\n' "$f"
			cat "$root/$f"
		done
}
sum="$(fingerprint | git hash-object --stdin)"
# Leave at once when no Go code changed (git's id for empty content), or when
# these exact changes were checked already.
if [ "$sum" = e69de29bb2d1d6434b8b29ae775ad8c2e48c5391 ] ||
	{ [ -f "$last" ] && [ "$(cat "$last")" = "$sum" ]; }; then
	exit 0
fi

bin="$here/bin/gowl"
case "$(uname -s)" in MINGW* | MSYS* | CYGWIN*) bin="$bin.exe" ;; esac
# The go command's cache makes this cheap when nothing changed. A tree that
# does not build cannot be described either, so a failed build skips the check.
if ! (cd "$root" && go build -o "$bin" ./cmd/gowl) >/dev/null 2>&1; then
	echo "sprawl-check: the module does not build; skipping" >&2
	exit 0
fi

report="$("$bin" sprawl -base "$base" -fail-on info -seen "$seen" "$root" 2>&1)"
status=$?
if [ "$status" -gt 1 ]; then
	echo "sprawl-check: skipping: $(printf '%s' "$report" | head -n 3)" >&2
	exit 0
fi
printf '%s\n' "$sum" >"$last"
[ "$status" -eq 0 ] && exit 0

# Exit status 2 keeps the agent working, with this as its next instruction.
cat >&2 <<EOF
Before you finish: gowl sprawl compared the module's exported types with $base,
and this change grows the module beside its existing types rather than
through them.

$report

For each finding, either rework the change to use what is already there
(reuse the existing type, embed it, or implement an existing interface), or
tell the user why a new type is the right call. These findings will not be
repeated in this session.
EOF
exit 2
