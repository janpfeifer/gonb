//go:build gx

package gxexec

import (
	"fmt"
	"strings"

	"github.com/janpfeifer/gonb/common"
	"github.com/janpfeifer/gonb/internal/goexec"
	"github.com/janpfeifer/gonb/internal/kernel"

	"k8s.io/klog/v2"
)

// Executes the %%gx cell magic command.
//
// Supports compiling and generating binding for gx functions
// so they can be used from Go code in the notebook.
func ExecuteCell(msg kernel.Message, goExec *goexec.State, lines []string) error {
	if klog.V(2).Enabled() {
		klog.Infof("GX cell: %q", strings.Join(lines, "\n"))
	}

	// Find the last line that is gx code, ie before we see %go or %%
	last_gx_line := -1
	for i, line := range lines {
		if line == "%%go" || line == "%%" {
			break
		}
		last_gx_line = i
	}

	// Echo gx code
	// We'll actually want to compile Gx code here, and create some kind of
	// declaration 'memory' that will be used to merge multiple GX cells together.
	// For now we just print it.
	gx_code := strings.Join(lines[1:last_gx_line], "\n")

	if err := compile(gx_code); err != nil {
		kernel.PublishWriteStream(msg, kernel.StreamStderr, fmt.Sprintf("GX compile error: %v\n", err))
	}

	if err := kernel.PublishWriteStream(msg, kernel.StreamStdout, fmt.Sprintf("%s\n", gx_code)); err != nil {
		return err
	}

	if last_gx_line == len(lines)-1 {
		// No more code to execute
		return nil
	}

	// Now execute the rest of the go code detected by the presence of %go
	// or %% in the cell. Everything before it is skipLines.
	// ie skipLines is the set of lines before we see %% or %go
	skipLines := common.MakeSet[int]()
	for i := 0; i <= last_gx_line; i++ {
		skipLines.Insert(i)
	}

	skipLines.Insert(last_gx_line + 1) // also skip the %% or %go line

	// We pass all lines so that we get correct line numbers in errors,
	// but we skip the gx lines.
	// Eventually, we'll want to merge any generated bindings into the go code here.
	// which will be tricky.
	return goExec.ExecuteCell(msg, msg.Kernel().ExecCounter, lines, skipLines)
}
