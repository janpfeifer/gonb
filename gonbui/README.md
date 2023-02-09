# GoNBUI

[**GoNB**](https://github.com/janpfeifer/gonb) is a [Jupyter notebook](https://jupyter.org/) kernel able to run Go
code.

**GoNBUI** is a library that allows any go code ran in **GoNB** to easily display various type of rich content in
the notebook. Currently supported:

* HTML: An arbitrary HTML block, and it also allows updates to a block (e.g.: updates to some ongoing processing).
* Images: Any given Go image (automatically rendered as PNG); a PNG file content; SVG.
* Javascript: To be run in the Notebook.
* Input request from the notebook.

More (sound, video, etc.) can be quite easily added as well, expect the list to grow.
