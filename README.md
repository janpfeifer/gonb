# GoNB - A Go Notebook Kernel for Jupyter

To quick start, see the very simple [tutorial](examples/tutorial.ipynb)!

Go is a compiled language, but with very fast compilation, that allows one to use
it in a REPL (Read-Eval-Print-Loop) fashion, by inserting a "Compile" step in the middle
of the loop -- so it's a Read-Compile-Run-Print-Loop -- while still feeling very interactive. 

**gonb** leverages that compilation speed to implement a full-featured (at least it's getting there)
[Jupyter notebook](https://jupyter.org/) kernel.

It's still **experimental**. This is very fresh from the oven, and likely there are many nuanced
(or not so nuanced) situations where it may not work as expected. Reports of issues and even better
fixes are very welcome.

# Rich display: HTML, Images, SVG, Videos, manipulating javascript, etc.

**gonb** opens a named pipe (set in environment variable gonb_PIPE) that a program can use to directly
display any type of HTML content. 

For the most cases, one can simply import [`github.com/janpfeifer/gonb/gonbui`](https://github.com/janpfeifer/gonb/gonbui):
the library offers and convenient API to everything available.

If implementing some new mime type (or some other form of interaction), see `kernel/display.go` for the protocol
details.

# TODOs

Many! Contributions are welcome. Some from the top of my head:

* Mac and Windows: 
  * Installation.
  * Named-pipe implementation in `kernel/pipeexec.go`.
* Example in [colab.research.google.com](http://colab.research.google.com)
* Tracking of lines on generated Go files back to cell, so reported errors are easy to follow.


# Implementation

The Jupyter kernel started from [gophernotes](https://github.com/gopherdata/gophernotes)
implementation, but was heavily modified. Also, the execution loop and mechanisms are completely
different and new.

