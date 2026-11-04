#!/usr/bin/env bash
# Claude Code hook entry point for ontology-check (see ontology-check/main.go).
# Rebuilds the binary when it is missing or older than its own sources or the
# gowl packages it links, then hands the hook payload over to it.
set -u

# As a hook (pre/post), every Edit and Write runs this script, so leave at once
# unless the payload names a .ofn file; the binary re-checks the extension
# properly. "compare" is run by hand or by the new-term skill and reads no stdin.
input=""
if [ "${1:-}" != compare ]; then
	input="$(cat)"
	case "$input" in
	*'.ofn"'* | *'.OFN"'*) ;;
	*) exit 0 ;;
	esac
fi

here="$(cd "$(dirname "$0")" && pwd)"
root="$(cd "$here/../.." && pwd)"
bin="$here/bin/ontology-check"
case "$(uname -s)" in MINGW* | MSYS* | CYGWIN*) bin="$bin.exe" ;; esac

stale=""
if [ -x "$bin" ]; then
	stale="$(find "$here/ontology-check" "$root/owl" "$root/el" "$root/lint" "$root/vocab" \
		\( -name '*.go' -o -name '*.ofn' -o -name go.mod \) -newer "$bin" -print -quit 2>/dev/null)"
fi
if [ ! -x "$bin" ] || [ -n "$stale" ]; then
	# A failed build must not block the edit: report it and let the edit through.
	if ! (cd "$here/ontology-check" && go build -o "$bin" .) >&2; then
		echo "ontology-check: build failed; skipping check" >&2
		exit 0
	fi
fi
if [ "${1:-}" = compare ]; then
	exec "$bin" "$@"
fi
printf '%s' "$input" | "$bin" "$@"
