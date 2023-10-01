// Package dispatcher routes messages to/from Jupyter. This handles the logic of the communication,
// as opposed to encoding/validation and connection details, which are handled by the kernel package.
package dispatcher

import (
	"fmt"
	. "github.com/janpfeifer/gonb/common"
	"github.com/janpfeifer/gonb/gonbui/protocol"
	"github.com/janpfeifer/gonb/internal/goexec"
	"github.com/janpfeifer/gonb/internal/kernel"
	"github.com/janpfeifer/gonb/internal/specialcmd"
	"github.com/pkg/errors"
	"golang.org/x/exp/slices"
	"io"
	"k8s.io/klog/v2"
	"strings"
	"sync"
)

const (
	Version = "0.1.0"
)

// RunKernel takes a connected kernel and dispatches the various inputs the appropriate handlers.
// It returns only when the kernel stops running.
func RunKernel(k *kernel.Kernel, goExec *goexec.State) {
	var wg sync.WaitGroup
	poll := func(ch <-chan kernel.Message, fn func(msg kernel.Message, goExec *goexec.State) error) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			kernelStop := k.StoppedChan()
			for {
				select {
				case <-kernelStop:
					return
				case msg := <-ch:
					go func(msg kernel.Message) {
						err := fn(msg, goExec)
						if err != nil {
							if !k.IsStopped() {
								klog.Errorf("*** Failed to process incoming message, stopping kernel: %+v", err)
								k.Stop()
							}
							return
						}
					}(msg)
				}
			}
		}()
	}

	poll(k.Stdin(), func(msg kernel.Message, _ *goexec.State) error {
		if !msg.Ok() {
			return errors.WithMessagef(msg.Error(), "stdin message error")
		}
		return msg.DeliverInput()
	})
	poll(k.Shell(), handleShellMsg)
	poll(k.Control(), func(msg kernel.Message, goExec *goexec.State) error {
		if msg == nil {
			return nil
		}
		if !msg.Ok() {
			return errors.WithMessagef(msg.Error(), "control message error")
		}
		return handleShellMsg(msg, goExec)
	})

	wg.Wait()
}

// BusyMessageTypes are messages that triggers setting the kernel status to busy
// while they are being handled.
var BusyMessageTypes = []string{
	"execute_request", "inspect_request", "complete_request",
	"kernel_info_request",
	//"kernel_info_request", "shutdown_request",
}

// handleShellMsg responds to a message on the shell or control ROUTER socket.
//
// It's assumed that more than one message may be handled concurrently, in particular
// messages coming from the control socket.
func handleShellMsg(msg kernel.Message, goExec *goexec.State) (err error) {
	if !msg.Ok() {
		return errors.WithMessagef(msg.Error(), "shell message error")
	}
	msgType := msg.ComposedMsg().Header.MsgType
	klog.V(1).Infof("Dispatcher: handling %q", msgType)

	if slices.Contains(BusyMessageTypes, msgType) {
		// Tell the front-end that the kernel is working and when finished, notify the
		// front-end that the kernel is idle again.
		if err = kernel.PublishKernelStatus(msg, kernel.StatusBusy); err != nil {
			err = errors.WithMessagef(err, "publishing kernel status %q", kernel.StatusBusy)
			return
		}
		klog.V(2).Infof("> kernel status set to busy.")

		// Defer publishing of status idle again, before returning.
		defer func() {
			newErr := kernel.PublishKernelStatus(msg, kernel.StatusIdle)
			if err == nil && newErr != nil {
				err = errors.WithMessagef(err, "publishing kernel status %q", kernel.StatusIdle)
			}
			klog.V(2).Infof("> kernel status set to idle.")
		}()
	}

	switch msgType {
	case "kernel_info_request":
		if err = kernel.SendKernelInfo(msg, Version); err != nil {
			err = errors.WithMessagef(err, "replying to 'kernel_info_request'")
		}

	case "shutdown_request":
		if err = handleShutdownRequest(msg, goExec); err != nil {
			err = errors.WithMessagef(err, "replying 'shutdown_request'")
		}
	case "execute_request":
		if err = handleExecuteRequest(msg, goExec); err != nil {
			err = errors.WithMessagef(err, "replying to 'execute_request'")
		}
	case "inspect_request":
		if err = HandleInspectRequest(msg, goExec); err != nil {
			err = errors.WithMessagef(err, "replying to 'inspect_request'")
		}
	case "complete_request":
		if err := handleCompleteRequest(msg, goExec); err != nil {
			klog.Fatal(err)
		}

	case "comm_open", "comm_msg", "comm_comm_close", "comm_info_request":
		err = handleComms(msg, goExec)

	case "is_complete_request":
		klog.V(2).Infof("Received is_complete_request: ignoring, since it's not a console like kernel.")

	default:
		// Log, ignore, and hope for the best.
		klog.Infof("Unhandled shell-socket message %q", msg.ComposedMsg().Header.MsgType)
	}
	return
}

// handleShutdownRequest sends a "shutdown" message.
func handleShutdownRequest(msg kernel.Message, goExec *goexec.State) error {
	content := msg.ComposedMsg().Content.(map[string]any)
	restart := content["restart"].(bool)
	type shutdownReply struct {
		Restart bool `json:"restart"`
	}
	reply := shutdownReply{
		Restart: restart,
	}
	if err := msg.Reply("shutdown_reply", reply); err != nil {
		return errors.WithMessagef(err, "replying shutdown_reply")
	}
	klog.Infof("Shutting down in response to shutdown_request")

	// Shutdown comms with front-end first.
	if err := goExec.Comms.Close(msg); err != nil {
		klog.Warningf("comms: failure closing connection to front-end: %+v", err)
	}

	msg.Kernel().Stop()
	return nil
}

type OutErr struct {
	out io.Writer
	err io.Writer
}

// handleExecuteRequest runs code from an execute_request method,
// and sends the various reply messages.
func handleExecuteRequest(msg kernel.Message, goExec *goexec.State) error {
	// Extract the data from the request.
	content := msg.ComposedMsg().Content.(map[string]any)
	code := content["code"].(string)
	silent := content["silent"].(bool)
	storeHistory := content["store_history"].(bool)

	if klog.V(2).Enabled() {
		klog.Infof("Message content: %+v", content)
	}

	// Prepare the map that will hold the reply content.
	replyContent := make(map[string]any)
	if storeHistory {
		msg.Kernel().ExecCounter++
		replyContent["execution_count"] = msg.Kernel().ExecCounter
	}

	// Tell the front-end what the kernel is about to execute.
	if !silent {
		klog.V(1).Infof("> publish \"execute_input\" with code")
		if err := kernel.PublishExecuteInput(msg, code); err != nil {
			return errors.WithMessagef(err, "publishing execution input")
		}
	}

	// Dispatch to various executors.
	msg.Kernel().Interrupted.Store(false)
	lines := strings.Split(code, "\n")
	specialLines := MakeSet[int]() // lines that are special commands and not Go.
	var executionErr error
	if err := specialcmd.Parse(msg, goExec, true, lines, specialLines); err != nil {
		executionErr = errors.WithMessagef(err, "executing special commands in cell")
	}
	hasMoreToRun := len(specialLines) < len(lines) || goExec.CellIsTest
	if executionErr == nil && !msg.Kernel().Interrupted.Load() && hasMoreToRun {
		executionErr = goExec.ExecuteCell(msg, msg.Kernel().ExecCounter, lines, specialLines)
	}

	// Final execution result.
	if executionErr == nil {
		// if the only non-nil value should be auto-rendered graphically, render it
		replyContent["status"] = "ok"
		replyContent["user_expressions"] = make(map[string]string)
	} else {
		name, value, traceback := goexec.JupyterErrorSplit(executionErr)
		replyContent["status"] = "error"
		replyContent["ename"] = name
		replyContent["evalue"] = value
		replyContent["traceback"] = traceback

		// Publish an execution_error message.
		if err := kernel.PublishExecutionError(msg, value, traceback, name); err != nil {
			return errors.WithMessagef(err, "publishing back execution error")
		}
	}

	// Send the output back to the notebook.
	if klog.V(2).Enabled() {
		klog.Infof("> execute_reply: %+v", replyContent)
	}
	if err := msg.Reply("execute_reply", replyContent); err != nil {
		return errors.WithMessagef(err, "publish 'execute_reply`")
	}
	return nil
}

// HandleInspectRequest presents rich data (HTML?) with contextual information for the
// contents under the cursor.
func HandleInspectRequest(msg kernel.Message, goExec *goexec.State) error {
	content := msg.ComposedMsg().Content.(map[string]any)
	code := content["code"].(string)
	cursorPos := int(content["cursor_pos"].(float64))
	detailLevel := int(content["detail_level"].(float64))
	klog.V(1).Infof("inspect_request: cursorPos(utf16)=%d, detailLevel=%d", cursorPos, detailLevel)

	// Find cursorLine and cursorCol from cursorPos. Both are 0-based.
	lines, cursorLine, cursorCol := kernel.JupyterToLinesAndCursor(code, cursorPos)

	// Separate special commands from Go commands.
	usedLines := MakeSet[int]()
	if err := specialcmd.Parse(msg, goExec, false, lines, usedLines); err != nil {
		return errors.WithMessagef(err, "parsing special commands in cell")
	}

	// Get data contents for reply.
	var data kernel.MIMEMap
	if usedLines.Has(cursorLine) {
		// If special command, use our help message as inspect content.
		data = kernel.MIMEMap{string(protocol.MIMETextPlain): any(specialcmd.HelpMessage)}
	} else {
		// Parse Go.
		var err error
		data, err = goExec.InspectIdentifierInCell(lines, usedLines, cursorLine, cursorCol)
		if err != nil {
			data = kernel.MIMEMap{
				string(protocol.MIMETextPlain): any(
					fmt.Sprintf("%s", err.Error())),
				//fmt.Sprintf("Failed to inspect: in cell=[line=%d, col=%d]:\n%+v", cursorLine+1, cursorCol+1, err)),
			}
		}
	}

	// Send reply.
	reply := &kernel.InspectReply{
		Status:   "ok",
		Found:    len(data) > 0,
		Data:     data,
		Metadata: make(kernel.MIMEMap),
	}
	return msg.Reply("inspect_reply", reply)
}

// handleCompleteRequest replies with a `complete_reply` message, to auto-complete code.
func handleCompleteRequest(msg kernel.Message, goExec *goexec.State) (err error) {
	klog.V(2).Infof("`complete_request`:")

	// Start with empty reply, and makes sure reply is sent at the end.
	reply := &kernel.CompleteReply{
		Status:      "ok",
		Matches:     []string{},
		CursorStart: 0,
		CursorEnd:   0,
		Metadata:    make(kernel.MIMEMap),
	}

	// Reply is sent in this deferred function, that may be just the error
	// that happened.
	defer func() {
		if err != nil {
			klog.Warningf("Handling `complete_request` failed: %+v", err)
			reply.Status = "error"
		}
		klog.V(2).Infof("`complete_reply`: %s, %d matches", reply.Status, len(reply.Matches))
		err = msg.Reply("complete_reply", reply)
	}()

	content := msg.ComposedMsg().Content.(map[string]any)
	if _, found := content["code"]; !found {
		return
	}
	if _, found := content["cursor_pos"]; !found {
		return
	}
	code := content["code"].(string)
	cursorPos := int(content["cursor_pos"].(float64))
	reply.CursorStart = cursorPos
	reply.CursorEnd = cursorPos
	klog.V(2).Infof("complete_request: cursorPos(utf16)=%d", cursorPos)

	// Find cursorLine and cursorCol from cursorPos. Both are 0-based.
	lines, cursorLine, cursorCol := kernel.JupyterToLinesAndCursor(code, cursorPos)

	// Separate special commands from Go commands.
	usedLines := MakeSet[int]()
	if err = specialcmd.Parse(msg, goExec, false, lines, usedLines); err != nil {
		err = errors.WithMessagef(err, "parsing special commands in cell")
		return
	}
	if usedLines.Has(cursorLine) {
		// No auto-complete for special commands.
		return
	}

	err = goExec.AutoCompleteOptionsInCell(lines, usedLines, cursorLine, cursorCol, reply)
	return
}