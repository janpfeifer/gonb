# GoNB Changelog

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