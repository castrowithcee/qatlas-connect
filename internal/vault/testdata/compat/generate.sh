#!/bin/sh
# Writes the vaults in <release>/ beside this script with the code of each release named, v2.2.0 and v3.3.0
# by default: generate_test.go runs inside that release's vault package. Run it from anywhere inside the
# repository; the output is the same every run.
set -eu
out=$(cd "$(dirname "$0")" && pwd)
root=$(git -C "$out" rev-parse --show-toplevel)
[ "$#" -gt 0 ] || set -- v2.2.0 v3.3.0
for release in "$@"; do
	work=$(mktemp -d)
	git -C "$root" archive "$release" go.mod go.sum internal | tar -x -C "$work"
	rm -f "$work"/internal/vault/*_test.go
	cp "$out/generate_test.go" "$work/internal/vault/"
	rm -rf "${out:?}/$release"
	mkdir -p "$out/$release"
	(cd "$work" && FIXTURE_OUT="$out/$release" go test -count=1 -run '^TestGenerateFixture$' ./internal/vault/)
	rm -rf "$work"
done
