# GoNB Changelog

## Next


* More contextual help and auto-complete improvements:
  * Tracking of files follow through symbolic links.

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