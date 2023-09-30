## Functional Tests

These are notebooks used for testing different aspects of GoNB.
The package `gonb/internal/nbtests` executes these notebooks (with `jupyter nbconvert --execute --to asciidoc ...`)
and checks for the expected results.

New tests usually include a notebook here, and a corresponding test in `gonb/internal/nbtests/nbtests_test.go`. 
When all is working, remember to run `run_coverage.sh`, it will update the file `docs/converage.txt`
to generate a report, and it will trigger an update the coverage badge in GitHub.

These trivial notebooks can be saved without any results -- they are regenerated during testing.

Remember any changes here should be matched by corresponding tests in the package `gonb/internal/nbtests`.

