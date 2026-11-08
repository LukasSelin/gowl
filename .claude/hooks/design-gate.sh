#!/usr/bin/env bash
# Claude Code PreToolUse hook entry point for design-gate (see design-gate/main.go).
# Rebuilds the binary when it is missing or older than its own sources or the
# owl package it links, then hands the hook payload over to it. Exit status 2
# refuses the edit, with the binary's message as the reason.
set -u

# Every Edit and Write runs this script, so leave at once unless the payload
# names a Go file; the binary re-checks the extension properly.
input="$(cat)"
case "$input" in
*'.go"'*) ;;
*) exit 0 ;;
esac

here="$(cd "$(dirname "$0")" && pwd)"
root="$(cd "$here/../.." && pwd)"
bin="$here/bin/design-gate"
case "$(uname -s)" in MINGW* | MSYS* | CYGWIN*) bin="$bin.exe" ;; esac

stale=""
if [ -x "$bin" ]; then
	stale="$(find "$here/design-gate" "$root/owl" \
		\( -name '*.go' -o -name go.mod \) -newer "$bin" -print -quit 2>/dev/null)"
fi
if [ ! -x "$bin" ] || [ -n "$stale" ]; then
	# A failed build must not block the edit: report it and let the edit through.
	if ! (cd "$here/design-gate" && go build -o "$bin" .) >&2; then
		echo "design-gate: build failed; skipping check" >&2
		exit 0
	fi
fi
printf '%s' "$input" | "$bin"
