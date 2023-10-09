#!/bin/bash

# run_coverage.sh runs longer tests, in particular the integration tests using
# a headless chrome browser. These can take several minutes, so it's not connected
# to a Github action. The recommendation is to run before every release.
#
# It builds everything with `--cover` so to generate a final test coverage report,
# a metric we want to increase whenever possible.
#
# It also does a cross-compilation build for darwin/arch64: I don't have the hardware
# to actually execute/test it, but at least it checks that it compiles.
set -e

# Note: the environment variable `REAL_GOCOVERDIR` is used by the integration
# tests (in nbtests/) to overwrite the temporary `GOCOVERDIR` created (and discarded)
# by `go test` when `-test.gocoverdir` is given.
REAL_GOCOVERDIR="$(mktemp -d /tmp/gonb_test_coverage.XXXXXXXXXX)"
export REAL_GOCOVERDIR
echo "REAL_GOCOVERDIR=${REAL_GOCOVERDIR}"

echo
echo "(1) Cross-compilation for darwin/arm64"
xbuild="$(mktemp /tmp/gonb_darwin_arm64_XXXXXXXX)"
env GOOS=darwin GOARCH=arm64 go build -o "${xbuild}" .
rm -f "${xbuild}"

echo
echo "(2) Running all tests with --cover"
go test --cover --covermode=set --coverpkg=./... ./... -test.count=1 \
  -test.gocoverdir="${REAL_GOCOVERDIR}"

echo
echo "(3) Generating docs/coverage.txt"
go tool covdata func -i "${REAL_GOCOVERDIR}" > docs/coverage_raw.txt

# Filter out spurious coverage on cell code and remove line number
# (which won't match after changes)
cat docs/coverage_raw.txt | \
  egrep '^(github.com/janpfeifer/gonb|total)' | \
  sed 's/:[0-9]*://g' \
  > docs/coverage.txt
rm docs/coverage_raw.txt

echo
echo "(4) Cleaning up REAL_GOCOVERDIR"
rm -rf "${REAL_GOCOVERDIR}"

echo
echo "(5) Diff of docs/coverage.txt"
git diff docs/coverage.txt