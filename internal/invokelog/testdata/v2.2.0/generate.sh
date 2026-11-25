#!/bin/sh
# Writes the day files in this directory with the code of release v2.2.0: generate_test.go runs inside that
# release's invokelog package. Run it from anywhere inside the repository; the output is the same every run.
set -eu
out=$(cd "$(dirname "$0")" && pwd)
root=$(git -C "$out" rev-parse --show-toplevel)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
git -C "$root" archive v2.2.0 go.mod go.sum internal/invokelog internal/filelock | tar -x -C "$work"
rm -f "$work"/internal/invokelog/*_test.go
cp "$out/generate_test.go" "$work/internal/invokelog/"
rm -f "$out"/*.jsonl
(cd "$work" && FIXTURE_OUT="$out" go test -count=1 -run '^TestGenerateFixture$' ./internal/invokelog/)
