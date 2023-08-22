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