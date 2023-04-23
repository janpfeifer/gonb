## Package `goexec`

The package `goexec` is responsible for executing the notebook cells using Go, as well as keeping the
state of all current declarations (functions, variables, types, constants, imports) that once
declared, stay alive for subsequent cell executions.

The code is organized in the following files:

* `goexec.go`: definition of the main `State` object and the various Go structs for the various declarations
  (functions, variables, types, constants, imports), including the concept of cursor.
* `execcode.go`: implements `State.ExecuteCell()`, the main functionality offered by the package.
* `composer.go`: generate dynamically a `main.go` file from pre-parsed declarations. It includes some
  the code that renders teh various types of declarations, and a writer that keep tabs on the cursor position.
* `parser.go`: methods and objects used to parse the Go code from the cell, and again after `goimports` is run.
