#!/bin/bash
set -e

# Note: the environment variable `REAL_GOCOVERDIR` is used by the integration
# tests (in nbtests/) to overwrite the temporary `GOCOVERDIR` created (and discarded)
# by `go test` when `-test.gocoverdir` is given.
export REAL_GOCOVERDIR="$(mktemp -d /tmp/gonb_test_coverage.XXXXXXXXXX)"
echo "REAL_GOCOVERDIR=${REAL_GOCOVERDIR}"

echo
echo "(1) Running all tests with --cover"
go test --cover --covermode=set --coverpkg=./... ./... -test.count=1 \
  -test.gocoverdir="${REAL_GOCOVERDIR}"

echo
echo "(2) Generating docs/coverage.txt"
go tool covdata func -i "${REAL_GOCOVERDIR}" > docs/coverage.txt

echo
echo "(3) Cleaning up REAL_GOCOVERDIR"
rm -rf "${REAL_GOCOVERDIR}"

echo
echo "(4) Diff of docs/coverage.txt"
git diff docs/coverage.txt