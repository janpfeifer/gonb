## GoNB Help Page

**GoNB** is a Go kernel that compiles and executes on-the-fly Go code.

When executing a cell, **GoNB** will save the cell contents (except non-Go commands see
below) into a `main.go` file, compile and execute it.

It also saves any global declarations (imports, functions, types, variables, constants)
and reuse them at the next cell execution -- so you can define a function in one
cell, and reuse in the next one. Just the `func main()` is not reused.

A `hello world` example would look like:

```go
func main() {
    fmt.Printf(`Hello world!\n`);
}

```

But to avoid having to type `func main()` all the time, you can use `%%` and everything
after is wrapped inside a `func main() { ... }`. 
So our revised `hello world` looks like:

```go
%%
fmt.Printf(`Hello world!\n`)

```


### Init Functions -- `func init()`

Since there is always only one definition per function name, it's not possible for
each cell to have its own init() function. 
Instead, **GoNB** converts any function named `init_something()` to `init()` before 
compiling and executing. 
This way each cell can create its own `init_...()` and have it called at every cell execution.


### Special non-Go Commands

- `%%` or `%main`: Marks the lines as follows to be wrapped in a `func main() {...}` during
  execution. A shortcut to quickly execute code. It also automatically includes `flag.Parse()`
  as the very first statement. Anything `%%` or `%main` are taken as arguments
  to be passed to the program -- it resets previous values given by `%args`.
- `%args`: Sets arguments to be passed when executing the Go code. This allows one to
  use flags as a normal program. Notice that if a value after `%%` or `%main` is given, it will
  overwrite the values here.
- `%autoget` and `%noautoget`: Default is `%autoget`, which automatically does `go get` for
  packages not yet available.
- `%cd [<directory>]`: Change current directory of the Go kernel, and the directory from where
  the cells are executed. If no directory is given it reports the current directory.
- `%env VAR value`: Sets the environment variable VAR to the given value. These variables
  will be available both for Go code and for shell scripts.
- `%goflags <values...>`: Configures list of extra arguments to pass to `go build` when compiling the
  code for execution of a cell.
  If no values are given, it simply shows the current setting.
  To reset its value, use `%goflags """`.
  See example on how to use this in the [tutorial](https://github.com/janpfeifer/gonb/blob/main/examples/tutorial.ipynb). 
- `%with_inputs`: will prompt for inputs for the next shell command. Use this if
  the next shell command (`!`) you execute reads the stdin. Jupyter will require
  you to enter one last value after the shell script executes.
- `%with_password`: will prompt for a password passed to the next shell command.
  Do this is if your next shell command requires a password.

Notice all these commands are executed **before** any Go code in the same cell.

### Managing Memorized Definitions

- `%list` (or `%ls`): Lists all memorized definitions (imports, constants, types, variables and
  functions) that are carried from one cell to another.
- `%remove <definitions>` (or `%rm <definitions>`): Removes (forgets) given definition(s). Use as key the
  value(s) listed with `%ls`.
- `%reset [go.mod]` clears all memorized definitions (imports, constants, types, functions, etc.)
  as well as re-initializes the `go.mod` file. 
  If the optional `go.mod` parameter is given, it will re-initialize only the `go.mod` file -- 
  useful when testing different set up of versions of libraries.


### Executing Shell Commands

- `!<shell_cmd>`: executes the given command on a new shell. It makes it easy to run
  commands on the kernels box, for instance to install requirements, or quickly
  check contents of directories or files. Lines ending in `\` are continued on
  the next line -- so multi-line commands can be entered. But each command is
  executed in its own shell, that is, variables and state is not carried over.
- `!*<shell_cmd>`: same as `!<shell_cmd>` except it first changes directory to
  the temporary directory used to compile the go code -- the latest execution
  is always saved in the file `main.go`. It's also where the `go.mod` file for
  the notebook is created and maintained. Useful for manipulating `go.mod`,
  for instance to get a package from some specific version, something
  like `!*go get github.com/my/package@v3`.

Notice that when the cell is executed, first all shell commands are executed, and only after that, if there is
any Go code in the cell, it is executed.

### Tracking of Go Files In Development:

A convenient way to develop programs or libraries in **GoNB** is to use replace
rules in **GoNB**'s `go.mod` to your program or library being developed and test
your program from **GoNB** -- see the 
[Tutorial]((https://github.com/janpfeifer/gonb/blob/main/examples/tutorial.ipynb))'s
section "Developing Go libraries with a notebook" for different ways of achieving this.

To manipulate the list of files tracked for changes:

- `%track [file_or_directory]`: add file or directory to list of tracked files,
  which are monitored by **GoNB** (and 'gopls') for auto-complete or contextual help.
  If no file is given, it lists the currently tracked files.
- `%untrack [file_or_directory][...]`: remove file or directory from list of tracked files.
  If suffixed with `...` it will remove all files prefixed with the string given (without the
  `...`). If no file is given, it lists the currently tracked files.


### Environment Variables

For convenience, **GoNB** defines the following environment variables -- available for the shell
scripts (`!` and `!*`) and for the Go cells:

- `GONB_DIR`: the directory where commands are executed from. This can be changed with `%cd`.
- `GONB_TMP_DIR`: the directory where the temporary Go code, with the cell code, is stored
  and compiled. This is the directory where `!*` scripts are executed. It only changes when a kernel
  is restarted, and a new temporary directory is created.
- `GONB_PIPE`: is the _named pipe_ directory used to communicate rich content (HTML, images)
  to the kernel. Only available for _Go_ cells, and a new one is created at every execution.
  This is used by the `**GoNB**ui`` functions described above, and doesn't need to be accessed directly.

### Widgets

The package `gonbui/widgets` offers widgets that can be used to interact in a more
dynamic way, using the HTML element in the browser. E.g.: buttons, sliders.

It's not necessary to do anything, but, to help debug the communication system
with the front-end, **GoNB** offers a couple of special commands:

- `%widgets` - install the javascript needed to communicate with the frontend.
  This is usually not needed, since it happens automatically when using Widgets.
- `%widgets_hb` - send a _heartbeat_ signal to the front-end and wait for the
  reply.
  Used for debugging only.

### Writing for WASM (WebAssembly) (Experimental)

**GoNB** can also compile to WASM and run in the notebook. This is experimental, and likely to change
(feedback is very welcome), and can be used to write interactive widgets in Go, in the notebook.

When a cell with `%wasm` is executed, a temporary directory is created under the Jupyter root directory
called `jupyter_files/<kernel unique id>/` and the cell is compiled to a wasm file and put in that 
directory.

Then **GONB** outputs the javascript needed to run the compiled wam.

In the Go code, the following extra constants/variables are created in the global namespace, and can be used
in your Go code:

- `GonbWasmDir`, `GonbWasmUrl`: the directory and url (served by Jupyter) where the generated `.wasm` files are read.
  Potentially, the user can use it to serve other files.
  These are unique for the kernel, but shared among cells.
- `GonbWasmDivId`: When a `%wasm` cell is executed, an empty `<div id="<unique_id>"></div>`
  is created with a unique id -- every cell will have a different one.
  This is where the Wasm code can dynamically create content.

The following environment variables are set when `%wasm` is created:

- `GONB_WASM_SUBDIR`, `GONB_WASM_URL`: the directory and url (served by Jupyter) where the generated `.wasm` files are read.
  Potentially, the user can use it to serve other files.
  These environment variables are available for shell scripts (`!...` and `!*...` special commands) and non-wasm 
  programs if they want to serve different files from there.


### Writing Tests and Benchmarks

If a cell includes the `%test` command (anywhere in cell), it is compiled with `go test`
(as opposed to `go build`).
This can be very useful both to demonstrate tests, or simply help develop/debug them in a notebook.

If `%test` is given without any flags, it uses by default the flags `-test.v` (verbose) and `-test.run` defined
with the list of the tests defined in the current cell. 
That is, it will run only the tests in the current cell. 
Also, if there are any benchmarks in the current cell, it appends the flag `-test.bench=.` and runs the benchmarks
defined in the current cell.

Alternatively one can use `%test <flags>`, and the `flags` are passed to the binary compiled with `go test`. 
Remember that test flags require to be prefixed with `test.`. 
So for a verbose output, use `%test -test.v`. 
For benchmarks, run `%test -test.bench=. -test.run=Benchmark`. 

See examples in the [`gotest.ipynb` notebook here](https://github.com/janpfeifer/gonb/blob/main/examples/tests/gotest.ipynb).


### Cell Magic

The following are special commands that change how the cell is interpreted, so they are prefixed with `%%` (two '%'
symbols). They try to follow [IPython's Cell Magic](https://ipython.readthedocs.io/en/stable/interactive/magics.html#cell-magics).

They must always appear as the first line of the cell.

The contents in the cells are not assumed to be Go, so auto-complete and contextual help are disabled in those cells.

#### `%%writefile`

```
%%writefile [-a] <filePath>
```

Write contents of the cell (except the first line with the '%%writefile') to the given `<filePath>`. If `-a` is given
it will append the cell contents to the file.

This can be handy if for instance the notebook needs to write a configuration file, or simply to dump the code inside
the cell into some file.

File path passes through a tilde (`~`) expansion to the user's home directory, as well as environment variable substitution (e.g.: `${HOME}` or `$MY_DIR/a/b`). 

### `%%script`, `%%bash` and `%%sh`

```
%%script <command>
```

Execute `<command>` and feed it (`STDIN`) with the contents of the cell. The `%%bash` and `%%sh` magic is an alias to `%%script bash` and `%%script sh` respectively.

Generally, a convenient way to run larger scripts.


### Other

- `%goworkfix`: work around 'go get' inability to handle 'go.work' files. If you are
  using 'go.work' file to point to locally modified modules, consider using this. It creates
  'go mod edit --replace' rules to point to the modules pointed to the 'use' rules in 'go.work'
  file.
  It overwrites/updates 'replace' rules for those modules, if they already exist. See 
  [tutorial](https://github.com/janpfeifer/gonb/blob/main/examples/tutorial.ipynb) for an example.

### Links

- [github.com/janpfeifer/gonb](https://github.com/janpfeifer/gonb) - GitHub page.
- [Tutorial](https://github.com/janpfeifer/gonb/blob/main/examples/tutorial.ipynb).
- [go.dev](https://pkg.go.dev/github.com/janpfeifer/gonb) package reference.