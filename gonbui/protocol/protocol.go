// Package protocol contains the definition of the objects that are serialized and communicated to the
// kernel, using the standard Go `encoding/gob` package.
package protocol

import "encoding/gob"

const (
	// GONB_PIPE_ENV is the name of the environment variable holding
	// the path to the unix named pipe to communicate rich content to the kernel.
	//
	// One doesn't need to use this directly usually, just use gonbui package instead,
	// they will use this.
	GONB_PIPE_ENV = "GONB_PIPE"

	// GONB_DIR_ENV is the name of the environment variable holding the
	// current execution directory for the Go cells, and the scripts executed
	// with `!` special command.
	//
	// This value is visible for both, Go cells, and shell script (started with the `!` or
	// `!*` special commands.
	GONB_DIR_ENV = "GONB_DIR"

	// GONB_TMP_DIR_ENV is the name of the environment variable holding the
	// temporary directory created for the compilation of the Go code.
	// This is also the directory where scripts executed with `!*` are run from.
	//
	// This value is visible for both, Go cells, and shell script (started with the `!` or
	// `!*` special commands.
	GONB_TMP_DIR_ENV = "GONB_TMP_DIR"
)

type MIMEType string

const (
	MIMETextHTML       MIMEType = "text/html"
	MIMETextJavascript          = "text/javascript"
	MIMETextMarkdown            = "text/markdown"
	MIMETextPlain               = "text/plain"
	MIMEImagePNG                = "image/png"
	MIMEImageSVG                = "image/svg+xml"

	// MIMEJupyterInput should be associated with an `*InputRequest`.
	// It's a GoNB specific mime type.
	MIMEJupyterInput = "input/jupyter"
)

// DisplayData mimics the contents of the "display_data" message used by Jupyter, see
// https://jupyter-client.readthedocs.io/en/latest/messaging.html
type DisplayData struct {
	// Data maps MIME Type to content. Content depends on the mime type. Usually either string or []byte.
	Data map[MIMEType]any

	// Metadata is a generic dictionary of Go basic data (usually strings and numbers). According to the docs,
	// the only metadata keys currently defined in IPython are the width and height of images.
	Metadata map[string]any

	// DisplayID is a "transient" (see doc) information about which id to display something. It's used to
	// overwrite some previous content. So far tested only with HTML. A program should always generate
	// unique IDs to start with, and then re-use them to update them. If set, after the first time that it's
	// used, it will trigger the use of the `update_display_data` as opposed to `display_data` message.
	DisplayID string
}

// InputRequest for the front-end.
type InputRequest struct {
	// Prompt to display to user. Can be left empty.
	Prompt string

	// Password input, in which case the contents are not displayed.
	Password bool
}

func init() {
	gob.Register(&InputRequest{})
}
