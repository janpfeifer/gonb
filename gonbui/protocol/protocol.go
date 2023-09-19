// Package protocol contains the definition of the objects that are serialized and communicated to the
// kernel, using the standard Go `encoding/gob` package.
package protocol

import "encoding/gob"

const (
	// GONB_PIPE_ENV is the name of the environment variable holding
	// the path to the unix named pipe to communicate from the kernel to
	// the Go program.
	//
	// It is used to display rich content in Jupyter, and to update widgets.
	//
	// One doesn't need to use this directly usually, just use gonbui package instead.
	GONB_PIPE_ENV = "GONB_PIPE"

	// GONB_PIPE_BACK_ENV is the name of the environment variable holding
	// the path to the unix named pipe to communicate from the Go program to the kernel.
	//
	// It is used to receive updates from widgets displayed in the front-end (Jupyter notebook).
	GONB_PIPE_BACK_ENV = "GONB_PIPE_BACK"

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

	// GONB_JUPYTER_ROOT_ENV is the path to the Jupyter root directory, if GONB managed
	// to read it (depends on the architecture).
	//
	// This can be used to construct URLs to static file contents (images, javascript, etc.)
	// served by Jupyter: one can use `src="/file/...<path under GONB_JUPYTER_ROOT>..."`.
	GONB_JUPYTER_ROOT_ENV = "GONB_JUPYTER_ROOT"

	// GONB_JUPYTER_KERNEL_ID_ENV is the environment variable with the unique id assigned
	// by Jupyter to this kernel.
	// It's used to build some of the API paths to the JupyterServer.
	// If it's not set, GoNB was not able to parse it from the kernel file path.
	GONB_JUPYTER_KERNEL_ID_ENV = "GONB_JUPYTER_KERNEL_ID"

	// GONB_WASM_DIR_ENV is the temporary directory created in "${GONB_JUPYTER_ROOT}/.jupyter_files/<session_id>/wasm/"
	// where the generated `.wasm` file is stored when using `%wasm`.
	// It is set/updated everytime `%wasm` is first used.
	// It can be used to store/serve other static files if needed.
	// See GONB_WASM_URL_ENV.
	//
	// Notice that the Wasm program gets this value from a global variable automatically introduced in the Go code,
	// see `%help`.
	GONB_WASM_DIR_ENV = "GONB_WASM_DIR"

	// GONB_WASM_URL_ENV is the name of the environment variable defined in the `%wasm` environment
	// that holds the url used to fetch the generated `.wasm` file.
	// It is only set when `%wasm` is used.
	// It can also be fetch other static files if needed.
	// See GONB_WASM_DIR_ENV.
	//
	// Notice that the Wasm program gets this value from a global variable automatically introduced in the Go code,
	// see `%help`.
	GONB_WASM_URL_ENV = "GONB_WASM_URL"
)

type MIMEType string

const (
	MIMETextHTML       MIMEType = "text/html"
	MIMETextJavascript MIMEType = "text/javascript"
	MIMETextMarkdown   MIMEType = "text/markdown"
	MIMETextPlain      MIMEType = "text/plain"
	MIMEImagePNG       MIMEType = "image/png"
	MIMEImageSVG       MIMEType = "image/svg+xml"

	// MIMEJupyterInput maps to an `*InputRequest`, and requests input from Jupyter.
	// It's used by `gonbui.RequestInput`.
	//
	// It's a GoNB specific mime type.
	MIMEJupyterInput MIMEType = "gonb/jupyter_input"

	// MIMECommValue maps to a `*CommValue`. It can be used to send or request a value to/from
	// the front-end (notebook).
	// It's used by `comms.UpdateValue` and `comms.ReadValue`, used by widgets implementations.
	//
	// It's a GoNB specific mime type.
	MIMECommValue MIMEType = "gonb/comm_value"

	// MIMECommSubscribe maps to a `*CommSubscription`, and requests updates for the given
	// address in the front-end (notebook).
	// It's used by `comms.Subscribe`, used by widgets implementations.
	//
	// It's a GoNB specific mime type.
	MIMECommSubscribe MIMEType = "gonb/comm_subscribe"
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

// CommValueTypes currently accepted for communication with front-end.
// Can be used in generics for type matching, even though through the wire
// they are simply encoded as `any`.
type CommValueTypes interface {
	int | float64 | string | []int | []float64 | []string |
		map[string]int | map[string]float64 | map[string]string
}

// CommValue update or request to the front-end.
type CommValue struct {
	Address string
	Request bool
	Value   any
}

// CommSubscription to changes to an address in the front-end.
type CommSubscription struct {
	Address     string
	Unsubscribe bool // Set to true to unsubscribe instead.
}

func init() {
	gob.Register(&DisplayData{})
	gob.Register(&InputRequest{})
	gob.Register(&CommValue{})
	gob.Register(&CommSubscription{})

	// Register CommValueTypes.
	gob.Register([]int{})
	gob.Register([]float64{})
	gob.Register([]string{})
	gob.Register(map[string]int{})
	gob.Register(map[string]float64{})
	gob.Register(map[string]string{})
}
