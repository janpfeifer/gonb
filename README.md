# GoNB - A Go Notebook Kernel for Jupyter

[![Binder](https://mybinder.org/badge_logo.svg)](https://mybinder.org/v2/gh/janpfeifer/gonb/HEAD?labpath=examples%2Ftutorial.ipynb)

To quick start, see the very simple [**tutorial**](examples/tutorial.ipynb)! And
[here live version in Google's Colab](https://colab.research.google.com/drive/1vUd3SSoOm2K6UQLnkJQursZZx4CaIT_1?usp=sharing)
that one can interact with (make a copy first) -- if link doesn't work (Google Drive sharing publicly
is odd), [download it from github](examples/google_colab_demo.ipynb) and upload it to Google's Colab.

Go is a compiled language, but with very fast compilation, that allows one to use
it in a REPL (Read-Eval-Print-Loop) fashion, by inserting a "Compile" step in the middle
of the loop -- so it's a Read-Compile-Run-Print-Loop -- while still feeling very interactive. 

**GoNB** leverages that compilation speed to implement a full-featured (at least it's getting there)
[Jupyter notebook](https://jupyter.org/) kernel.

It's still **experimental**. This is very fresh from the oven, and likely there are many nuanced
(or not so nuanced) situations where it may not work as expected. Reports of issues and even better
fixes are very welcome.

# Installation

The [**tutorial**](examples/tutorial.ipynb) explains, but in short:

```
$ go install github.com/janpfeifer/gonb@latest
$ go install golang.org/x/tools/cmd/goimports@latest
$ go install golang.org/x/tools/gopls@latest
$ gonb --install
```

And then (re-)start Jupyter.

# Rich display: HTML, Images, SVG, Videos, manipulating javascript, etc.

**GoNB** opens a named pipe (set in environment variable `GONB_PIPE`) that a program can use to directly
display any type of HTML content. 

For the most cases, one can simply import 
[`github.com/janpfeifer/gonb/gonbui`](https://pkg.go.dev/github.com/janpfeifer/gonb/gonbui):
the library offers and convenient API to everything available. Examples of use in the
[tutorial](examples/tutorial.ipynb). 

If implementing some new mime type (or some other form of interaction), see `kernel/display.go` for the protocol
details.

# TODOs

Many! Contributions are welcome. Some from the top of my head:

* Mac and Windows: 
  * Installation.
  * Named-pipe implementation in `kernel/pipeexec.go`.
* Tracking of lines on generated Go files back to cell, so reported errors are easy to
  follow. In the meantime the errors can be moused over and will display the lines
  surrounding them.
* Run [`gopls`](https://github.com/golang/tools/tree/master/gopls) as a service (as opposed
  to invoking it every time -- super slow).
* Add auto-complete with [`gopls`](https://github.com/golang/tools/tree/master/gopls).
* Library to easily store/retrieve calculated content. When doing data analysis so 
  one doesn't need to re-generate some result at a next cell execution. Something
  like `func CacheResult[T any](id string, fn func() (T, error)) T, error` that will
  either load `T` from some storage, and if not stored, call `fn()` to generate the
  result and save it again. This way results can easily be reused on different cells. 

# Implementation

The Jupyter kernel started from [gophernotes](https://github.com/gopherdata/gophernotes)
implementation, but was heavily modified. Also, the execution loop and mechanisms are completely
different and new.

