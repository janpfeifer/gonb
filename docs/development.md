# Development of GoNB

## Jupyter Kernel Protocol

Related documentation:

* [Jupyter Kernal Architecture](https://www.romaglushko.com/blog/jupyter-kernel-architecture/) blog post by Roman Glushko
* [Messaging Protocol](https://jupyter-client.readthedocs.io/en/latest/messaging.html)
* [Jupyter Server](): backend of Jupyter applications, the one that talks to the kernels.

Tips:

* When running JupyterLab use `--Session.debug=true` to see all messages back-and-forth exchanged 
  between JupyterLab and **GoNB** (the kernel). 

## Integration tests in `/nbtests`

They require the following to run: `jupyter-lab`, `nbconvert`, `pandoc`.

In my setup I was Conda and install `pandoc` and `pip` in conda, and then `jupyter-lab` and `nbconvert`
with pip. I know it's painful :( ... another reason I keep to Go as much as I can.

**New test notebooks** can be created in `examples/tests`, and there should be a counter-part entry
in `nbtests/nbtests_test.go`, in the function `TestNotebooks()`, with a new function describing the
expected output of the execution of the new notebook.

## Generating Coverage Report

Since the integration tests have lots of dependencies, and I'm no expert in GitHub actions 
(it's like a docker but different?), coverage is for now being generated manually.

To generate it, run the script `run_coverage.sh` from the module (GoNB) root directory.
In the end of it, you will be presented with a `git diff docs/coverage.txt`, meaning
what changed in the coverage.

Note: the environment variable `REAL_GOCOVERDIR` is used by the integration tests to overwrite the
temporary `GOCOVERDIR` created (and discarded) by `go test` in this case.
