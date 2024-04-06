//go:build !(linux || darwin)

package kernel

import (
	"os"
)

// CaptureSignals list all signals to be captured: except of `os.Interrupt` (Control+C), all others trigger
// a clean exit of GoNB kernel.
//
// Notice `os.Interrupt` is used by Jupyter to signal to interrupt the execution of the current cell.
var CaptureSignals = []os.Signal{os.Interrupt}
