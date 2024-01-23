# `nbexec` Executing a Jupyter Notebook with Chromium

`nbexec` executes a Jupyter Notebook and then saves it.
The saved notebook can then be used by `nbconvert` and converted to various formats (html, text, pdf, etc.).

It requires Jupyter Notebook and optionally `nbconvert` and `pandoc` installed.

Typically, in Ubuntu, with `sudo apt install pandoc` and `pip install jupyterlab notebook nbconvert`.
But it may vary on different systems.

## How to use it?

Assuming you already installed it (see next question), you can do something like:

```bash
nbexec --jupyter_dir=${MY_PROJECT_DIR} -n=notebooks/integration_test.ipynb
```

After that the notebook (`${MY_PROJECT_DIR}/notebooks/integration_test.ipynb` in the example) will
have been executed and saved with the output cells set.
One can then do something like this, to check the results in text format -- text output will be
in `integration_test.asciidoc`:

```bash
jupyter nbconvert --to=asciidoc "${MY_PROJECT_DIR}/notebooks/integration_test.ipynb"
```

Example with verbose output, to allow one to debug what is going on, including a screenshot of the
rendered notebook after execution:

```bash
nbexec --jupyter_log --console_log  --vmodule=nbexec=1 \
   --screenshot="${MY_PROJECT_DIR}/notebooks/integration_test.png" \
   --jupyter_dir=${MY_PROJECT_DIR} -n=notebooks/integration_test.ipynb
```

Inputs to input boxes (created by the `%with_inputs` special command or with `gonbui.RequestInput()`)
can also be instrumented with `--input_boxes`.  

[GoNB](https://github.com/janpfeifer/gonb) does this all in tests for integration tests. 
See [`internal/nbtests` package](https://github.com/janpfeifer/gonb).
It also compiles everything with `--coverage` to get full coverage report in the end (see [`run_coverage.sh`](https://github.com/janpfeifer/gonb/blob/main/run_coverage.sh))

## How to install it?

Using Go package manager `go install github.com/janpfeifer/gonb/cmd/nbexec@latest`.
It will install in the directory `GOBIN` (try `go env GOBIN` to find it if not set), that should be in your `PATH`.

It requires Jupyter Notebook and optionally `nbconvert` and `pandoc` installed.

Typically, in Ubuntu, with `sudo apt install pandoc` and `pip install notebook nbconvert`.
But it may vary on different systems.


You will need the following 
`sudo apt install pandoc` and `pip install notebook nbconvert`.

```bash
go install github.com/janpfeifer/gonb/cmd/nbexec
```

## Why not use `nbconvert --execute ...` to execute the notebook?

Because it doesn't properly run some javascript -- I'm not sure what engine it uses
behind the scenes to execute javascript, but I didn't get the same results as
running on the browser. 

See [discussion here](https://discourse.jupyter.org/t/how-does-nbconvert-executes-javacript-can-i-see-the-js-console-output/21700)
in the Jupyter Community Forum.

## How does it work?

Short version:

1. It executes `jupyter notebook --expose-app-in-browser --no-browser`.
   - It captures the generated token.
2. It uses [go-rod](https://github.com/go-rod/rod)(see also docs in [go-rod.github.io](https://go-rod.github.io/#/get-started/README))
   to instrument a headless chromium browser to open the selected notebook.
   - It passes the captured token.
3. It then sends the following javascript to be executed in the virtual chromium browser:

```js
jupyterapp.commandLinker._commands.execute("editmenu:clear-all", null);
jupyterapp.commandLinker._commands.execute("runmenu:run-all", null);
jupyterapp.commandLinker._commands.execute("docmanager:save", null);
```

After that, it closes everything, clean up and exit.

## Thank You

To the [github.com/go-rod/rod](https://github.com/go-rod/rod) project, that made it so easy to implement this.

And to [Jupyter.org](https://jupyter.org/) of course!
