# Development of GoNB

## Integration tests in nbtests

They require the following to run: `jupyter-lab`, `nbconvert`, `pandoc`.

In my setup I was Conda and install `pandoc` and `pip` in conda, and then `jupyter-lab` and `nbconvert`
with pip. I know it's painful :( ... another reason I keep to Go as much as I can.

## Generate Coverage

Since it has lots of dependencies, and GitHub actions is painful to develop (and add dependencies),
coverage is for now being generated manually.

The integration requires the following to run the following, from the module (GoNB) root directory:

```bash
go test --work --cover --covermode=set --coverpkg=./... --coverprofile=/tmp/cov_profile.out ./nbtests/... -test.count=1 -test.v \
  >& /tmp/tests_output.txt \
  && go tool cover -func /tmp/cov_profile.out > /tmp/cov_func.out \
  && cover_dir=$(grep GOCOVERDIR /tmp/tests_output.txt | head -n 1 | cut -f2 -d'=') \
  && go tool covdata func -i "${cover_dir}" > docs/coverage.txt
```

The static file with coverage will be in `docs/coverage.out` and if this file is submitted the 
coverage badge is updated.

Hopefully this will get better. For now see more in [this thread in GoNuts](https://groups.google.com/g/golang-nuts/c/tg0ZrfpRMSg)
