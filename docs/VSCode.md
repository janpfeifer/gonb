# Visual Studio Code (VSCode) Running GoNB

VSCode can open GoNB notebooks as a normal tab. When opening it will ask the kernel to use, and if you have [GoNB
installed](https://github.com/janpfeifer/gonb?tab=readme-ov-file#linux-and-macos-installation-using-standard-go-tools)
it will offer it as an option.

There are some caveats though:

## Code Completions

VSCode doesn't talk [Jupyter's _Completion_ protocol](https://jupyter-client.readthedocs.io/en/latest/messaging.html#completion)
and hence won't make use of the auto-complete or contextual information that GoNB offers (using `gopls`).

For Python VSCode relies on IntelliSense.

## No Javascript

VSCode doesn't support notebooks that output javascript by default. So GoNB libraries like 
[Plotly](https://pkg.go.dev/github.com/janpfeifer/gonb/gonbui/plotly), or 
[widgets](https://pkg.go.dev/github.com/janpfeifer/gonb/gonbui/widgets) won't work.

## No WASM

It's an experimental feature for GoNB, but in VSCode for various reasons won't work either.


## Debugging

I'm not an expert, so I'm not sure where VSCode sends the logs (`stderr`) of GoNB kernel execution. But GoNB has an 
option to also output its logs to a specific file: `--extra_log=<output>`. You can install it with this flag, restart
VSCode, and the logs will appear on the given file.

Example: let's say you are in the directory where you cloned `gonb` repository, you can install your current version
of GoNB set up to also log to the file `/tmp/gonb.out` with:

```bash
go run . --install --logtostderr --vmodule=goexec=2,specialcmd=2,cellmagic=2,gopls=2,connection=2 --extra_log=/tmp/gonb.out
```

## Links of interest

* [VSCode Jupyter Notebooks](https://code.visualstudio.com/docs/datascience/jupyter-notebooks)