# GoNB Changelog

## v0.10.7, 2025/01/20

* Improved `autostart` logic: it now requires being mounted as "readonly" under `/root/autostart`.
* Added `%version`, and environment variables `GONB_VERSION`, `GONB_GIT_COMMIT`.
* Added `%help` info on missing environment variables.
* Added stack traces to protocol parsing errors.
* Include function comments in the generated code. This makes tags like `//go:noinline` work. See #150

## v0.10.6, 2024/10/16, Improved Docker, added `%capture`

* Feature request #138
  * Added openssh-client, rsync and curl, to allow users to install other dependencies.
  * Added sudo for apt install and apt update.
  * Added support for `autostart.sh` that if present in the mounted container `/notebooks` directory, and if root owned
    and set as executable.
* Updated Dockerfile to latest version to JupyterLab -- now the base docker is served `quay.io/jupyter/base-notebook`
* Added `%capture [-a] <file>` to capture the output of a cell (#142)
* Fixed `nbexec`: added `--disable-gpu` and `--disable-software-rasterizer` when executing "headless" chrome for tests.

## v0.10.5, Added SendAsDownload

* Added `dom.SendAsDownload` to send data from cells to the client by triggering a browser download. #134

## v0.10.4, Fixed #131

* Issue #131: proper handling of tuple variable declarations like `var contents, _ = os.ReadFile(...)`

## 0.10.3, Simple go1.23 update.

* go1.23, and update dependencies.
* Fixed GitHub's go.yaml actions, test shows green now.

## 0.10.2, 2024/07/10 Added Jupytext support and `ndlv` script for debugging cells.

* [Jupytext](https://jupytext.readthedocs.io/en/latest/) integration [#120](https://github.com/janpfeifer/gonb/discussions/120):
  * Many thanks for [Marc Wouts](github.com/mwouts) for [adding support in Jupytext](https://github.com/mwouts/jupytext/releases/tag/v1.16.3), 
    and @HaveF for the help and starting the topic.
  * Handle special commands to be prefixed with `//gonb:` -- this allows special commands to be parseable Go code, and makes it easier for IDEs.
  * Ignore `package` tag -- as opposed to raising an error: also to make easy on IDEs that may require a `package` tag.
  * Added special variation: `%exec <function_name> <args...>` that creates a main function that calls `<function_name>`
    and sets the program arguments (flags) to the given values.
* Added `ndlv` wrapper script for starting [gdlv](https://github.com/aarzilli/gdlv) on cell binary.
  * Many thanks for @HaveF for the help -- see [#122](https://github.com/janpfeifer/gonb/discussions/122)
* Notebook testing: changed `nbexec` to use system's google-chrome if available (with sandbox), or let Rod download 
  chromium, but then use with --no-sandbox (since there is no SUID on the binaries).
* Fixed tracking to simply ignore loops, but not interrupt traversal during tracking.

## 0.10.1, 2024/04/14 Added support for Apache ECharts

* Interrupt and Shutdown:
  * [`interrupt_mode`] set to `message`, as opposed to having a `SIGINT`. Works both in JupyterLab and VSCode.
  * Interrupt all cell executions at `shutdown_request`.
* New `github.com/janpfeifer/gonb-echarts` to add support to [Apache ECharts](https://echarts.apache.org/en/index.html)
  using [github.com/go-echarts/go-echarts](https://github.com/go-echarts/go-echarts).
  * Added `gonb_echarts.Display` and `gonb_echarts.DisplayContent`.
* Updated documentation on VSCode limitation for Javascript.
* Fixed bug in `dom.LoadScriptOrRequireJSModuleAndRun` where plotly source was hardcoded by mistake.

## 0.10.0, 2024/04/07 Improvements on Plotly, VSCode support, interrupt handling and several minor fixes. 
  
* Added special cell commands ("magic"):
  * `%%writefile` to write contents of cell to file. See #103. Thanks @potoo0!
  * `%%script`, `%%bash` and `%%sh` to execute the full cell contents with the given command (none of the extra flags are supported yet though).
* Added `dom.LoadScriptOrRequireJSModuleAndRun` and `dom.LoadScriptOrRequireJSModuleAndRunTransient` that dynamically decides
  if to include script using `<script src=...>` or use RequireJS.
* Plotly library uses `dom.LoadScriptOrRequireJSModuleAndRun` now, allowing result to show up in the HTML export of
  the notebook.
* Added `plotly.AppendFig` that allows plotting to a transient area, or anywhere in the page.
* Several minor fixes, see #106
* Added handling of SIGHUP and SIGTERM to handle a clean exit: and avoid leaking `gopls` daemons.
* Make sure SIGINT triggers an equivalent SIGINT on the child processes (it was not happening in VSCode).
* Added `docs/VSCode.md` with notes/info on running GoNB with Visual Studio Code.

## 0.9.6, 2024/02/18

* Fixed some typos in klog formatting.
* Updated dependencies.
* Updated gopls dependencies: new jsonrpc2 API.
* Added `LoadScriptModuleAndRun`.
* Added [Plotly Javascript](https://plotly.com/javascript/) support, in `gonbui.plotly` package. Also added example in tutorial.
* During installation, consider `/var/folders` also a temporary directory.

## 0.9.5, 2024/01/10

* Added instrumentation to Jupyter input boxes in `nbexec`.
* Added functional tests for input boxes created with `%with_inputs`, `%with_password` or `gonbui.RequestInput`.
* Added logo to installation.

## 0.9.4, 2023/12/13

* Cache is by-passed if cache key is set to empty ("").
* New widgets demo, using [GoMLX](https://github.com/gomlx/gomlx)'s [Flowers Diffusion demo](https://github.com/gomlx/gomlx/blob/main/examples/oxfordflowers102/OxfordFlowers102_Diffusion.ipynb)
* Updated Dockerfile to start from `/notebooks` directory, and with instructions to mount the
  host current directory in the `host/` subdirectory. More in #78
* Fixed `gonbui.RequestInput`.

## 0.9.3, 2023/10/09

* Widgets:
  * Normalized API across widgets, so they all look the same -- breaks the API, apologies.
  * Added `widget.Select` for a drop-down menu.
  * Improved integration tests.
* nbexec: changed javascript code to use the public API `jupyterapp.commands.execute` instead.

## 0.9.2, 2023/10/07

* Fixed Darwin (and other non-linux, that are unix-like) build -- see #74.
  * Added cross-compilation to darwin/arm64 test in `run-coverage.sh`

## 0.9.1, 2023/10/04

* Removed left-over debugging messages.
* Removed left-over replace rule from tutorial.

## 0.9.0 -- Widgets, DOM, Wasm, Front-end communication, `nbexec`, 2023/10/04

* Added **widgets** support (experimental): 
  * a websocket opened from the front-end that communicates
    to the kernel, and through it to the users cells.
  * API to use it in `gonb/gonbui/widgets`.
  * API to communicate with front-end in `gonb/gonbui/comms`.
    * `Listen[T](address)` function added to create a channel listening
      to front-end updates.
    * Added `--comms_log` flag to add verbose logging to the Javascript console.
  * API to manipulate the DOM in `gonb/gonbui/dom`. 
    * `Persist()` added to persist transient changes to the DOM -- meaning
      dynamically generated HTML and widgets will show up when exporting
      to HTML or when running `nbconvert`.
* Added `gonb/cmd/nbexec` to execute notebooks for integration testing --
  `nbconvert --execute` was not working.
* Added "%wasm" support (experimental): 
  * Allows compiling cell to WASM and running that in the notebook. One
    can write widgets like this. **Experimental**: there are some use cases are not 100% clear. See
    "%help" for details on how this works.
  * Added also `github.com/janpfeifer/gonb/gonbui/wasm` library with some basic helpers to write
    WASM widgets.
  * Added `gonb/examples/wasm_demo.ipynb` with a couple of examples of Wasm.
* Improved logging of errors; pre-checking for duplicate `package`, with improved error message.
* Handle incoming messages in a separated goroutine, so asynchronous/concurrent messages can be
  handled. Specially important when executing cells that take a long time.
  * Serialize "execute_request" and other "busy" type of requests, so they are executed in the order
    they are received.
* Added support for `%env VAR=VALUE` syntax as well (like ipython uses).
* Refactored internal packages to `internal/` subdirectory.

## 0.8.0 -- Tests and Benchmarks, 2023/08/24

* Added support for tests and benchmarks (`go test`) with `%test`, see #58
* Added `%gcflags` to allow arbitrary compilation flags to be set for GoNB, see #58

## 0.7.8 -- 2023/08/17

* Added support for --raw_errors, where errors are not reported using HTML. Useful for
  running tests, for instance with `nbmake` (see #48, by @bagel897).
* Added functional/integration tests by instrumenting `nbconvert` (bringing coverage from ~25% to ~55%).
* Added `run_coverage.sh` to include integration tests in coverage report.
* Coverage configured to be generated manually (and not automatically in GitHub actions) -- coverage
  badge still generated in GitHub actions.
* Installation uses `$JUPYTER_DATA_DIR`, if it is set.
* Fixed proper shutdown.
* Fixed gopls dying when a cell is interrupted.
* Updated `go.mod` parser dependency for go 1.21 -- since the format changes slightly (see #53).

## 0.7.7 -- 2023/08/08

* Added `DisplayMarkdown` and `UpdateMarkdown`.
* Changed `%help` to use markdown.
* `init_*` functions: 
  * Fixed duplicate rendering.
  * Added section about it in `tutorial.ipynb`
* Updated tutorial to use `%rm` to remove no longer wanted definitions.

## 0.7.6 -- 2023/07/28

* Issue #43:
  * %reset now also resets `go.mod`.
  * Added `%reset go.mod` which only resets `go.mod` but not the Go definitions memorized.

## 0.7.5 -- 2023/07/28

* Issue #30 (cont):
  * Added GONB_DIR and GONB_TMP_DIR even with the directories being used by GONB.

## 0.7.4 -- 2023/07/20

* Issue #38:
  * `%with_inputs` and `%with_password` now wait 200 milliseconds each time (a constant), before 
    prompting user with an input in the Jupyter Notebook.
  * Added `gonbui.RequestInput`, that will prompt the user with a text field in the notebook.

## 0.7.3 - 2023/07/14

* Issue #35: Fixed installation (--install): it now uses the absolute path to the gonb binary
  (as opposed to simply `os.Args[0]`).
  Also added check that it can find the "go" binary.
* Workaround for `go get` not working with `go.work`: parse `go get` errors, and if it's complaining about
  a missing package that is defined in one of the `go.work` "use" paths, it will add a suggestion the user
  add a `go.mod` replace rule.
* Added `%goworkfix` to add `use` clauses as `replace` clauses in `go.mod`.

## 0.7.2 - 2023/07/08

* Fixed bug crashing command "%cd" with no argument.
* Fixed error parsing: matching line number mix up with lines starting with 1 (instead of 0).
* Cleaned up logs: moved more logging to `klog`: most is disabled by default, but can be enabled
  for debugging passing the flags `--logtostderr --vmodule=...` (they work with `--install`).
* Fixed bug where #bytes written of parsed stderr was reported wrong, which lead to truncated errors.

## 0.7.1 - 2023/07/03

* Added support for tracking `go.work`, which allows auto-complete and contextual help
  to work with the local modules configured. It also requires `gopls` **v0.12.4** or newer to work.
* Fixed auto-complete bug when no `main` function (or no `%%`) was present in cell.
* Added special command `%cd` to chance current directory.
* Commands `%cd` and `%env` prints results of its execution.

## v0.7.0 - 2023/05/29

* Added "%ls" and "%rm" to manage memorized definitions directly.
* More contextual help and auto-complete improvements:
  * Tracking of files follows through symbolic links.

## v0.6.5 - 2023/05/23

* More contextual help and auto-complete improvements:
  * Added tracking of files in development (`%track`, `%untrack`), for usage with `gopls`.
  * Auto-track `replace` directives in `go.mod` pointing to local filesystem.

## v0.6.4 - 2023/05/22

* More InspectRequest improvements:
  * Search for identifier preceding the cursor if cursor is under a non-identifier.
  * If cursor under a ",", search for preceding function name identifier.
  * Handle case where cell is not parseable: like with auto-complete before.
* Fixed a bug where updates to `go.mod` and `go.sum` were not being notified to `gopls`.

## v0.6.3 - 2023/05/18

* Handle auto-complete case where cell is not parseable: now `gopls` is also called, and memorized
  definitions are instead saved on a second `other.go` file, for `gopls` to pick content from.
  (Issues #21 and #23).

## v0.6.2 - 2023/05/17

* Issue #23: Fixed support for generic types.

## v0.6.1

* Issue #21: Added call to `goimports` and `go get` before trying to get contextual information or auto-complete, 
  fixing many of the issues with those.

## v0.6.0

* Issue #16: Added package `cache`: implements a convenient cache of values for things that
  are expensive or slow to regenerate at each execution.
* Issue #13 and #16: Dockerfile and updated documentation.

## v0.5.1

* Fixed specialcmd_test.

## v0.5

* Improved error reporting, including indication of line number in cell.
* Parse error output of the execution of a cell, and if it contains a stack-trace, add a reference to the cell
  code (cell id and line number).
* Cleaned up, improved code documentation and testing for `goexec` package.

## v0.4.1

* Added support for Mac installation.

## v0.4.0

* "%%" or "%main" now set the program arguments as well. This may reset previously configured parameters
  given by "%args", which breaks compatibility is some cases, hence the version number bump.
* Added "UpdateHTML" and "UniqueID", to allow dynamically updated HTML content on the page.
* Fixed crash when auto-complete returns a nil structure.

## v0.3.9

* Small Go Report Card fixes (https://goreportcard.com/report/github.com/janpfeifer/gonb)

## v0.3.8

* Fixed CSS for VSCode/Github Codespaces -- it doesn't play very well with Jupyter CSS.

## v0.3.7

* Use standard Jupyter CSS color coding for error context -- should work on different themes (See #3).

## v0.3.6

* Better handling of gopls dying.
* Cleaned up and improved cursor mapping to generated Go file.
* Better handling of "didOpen" and "didChange" language server protocol with gopls.
* Monitor changes in files contents (for files being edited locally in parallel) 
  for gopls contextual help.
* Started instrumenting logs using `github.com/golang/glog`

## v0.3.5

* Display parsing errors that were disabled accidentally displaying.

## v0.3.4

* Added auto-complete.

## v0.3.3

* Fixed support of variables declared only with type but no value.
* Invoke `gopls` as a service, and talk LanguageServiceProtocol with it, to get inspection
  of symbol -- and upcoming auto-complete.
* Improved handling of cursor position: Jupyter sends UTF16 based positions (as opposed to bytes 
  or unicode runes). Still not perfect: regeneration of the Go code may get the cursor shifted.

## v0.3.2

* Added mybinder.org configuration

## v0.3.1

* Improved error message (in contextual help side-bar) if `gopls` is not installed.
* Added `--force` flag to allow installation even if `goimports` or `gopls` 
  are missing.

## v0.3.0

* Added support for **Contextual Help** (`control+I` in Jupyter), by servicing message `inpect_request`.
* Fixed keys for function receivers: when redefining a receiver as a pointer (from by value)
  they wouldn't be overwritten, and the presence of both would conflict. Special case of #1.

## v0.2.0, v0.2.1

* Added support for pointer receivers when defining methods of a type.
* Added `%env` to set environment variables from the kernel.
