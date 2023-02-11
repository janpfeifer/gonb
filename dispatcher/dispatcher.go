// Package dispatcher routes messages to/from Jupyter. This handles the logic of the communication,
// as opposed to encoding/validation and connection details, which are handled by the kernel package.
package dispatcher

import (
	"fmt"
	"github.com/janpfeifer/gonb/goexec"
	"github.com/janpfeifer/gonb/gonbui/protocol"
	"github.com/janpfeifer/gonb/kernel"
	"github.com/janpfeifer/gonb/specialcmd"
	"github.com/pkg/errors"
	"io"
	"log"
	"strings"
	"sync"
)

const (
	Version = "0.1.0"
)

// RunKernel takes a connected kernel and dispatches the various inputs the appropriate handlers.
// It returns only when the Kernel stops running.
func RunKernel(k *kernel.Kernel, goExec *goexec.State) {
	var wg sync.WaitGroup
	poll := func(ch <-chan *kernel.Message, fn func(msg *kernel.Message, goExec *goexec.State) error) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-k.StoppedChan():
					return
				case msg := <-ch:
					err := fn(msg, goExec)
					if err != nil {
						log.Printf("*** Failed to process incoming message: %+v", err)
						log.Printf("*** Stopping kernel.")
						k.Stop()
						return
					}
				}
			}
		}()
	}

	poll(k.Stdin(), func(msg *kernel.Message, _ *goexec.State) error {
		if !msg.Ok() {
			return errors.WithMessagef(msg.Error(), "stdin message error")
		}
		return msg.DeliverInput()
	})
	poll(k.Shell(), handleMsg)
	poll(k.Control(), func(msg *kernel.Message, goExec *goexec.State) error {
		log.Printf("Control Message: %+v", msg.Composed)
		return handleMsg(msg, goExec)
	})
	wg.Wait()
}

// handleMsg responds to a message on the shell or control ROUTER socket.
//
// It's assumed that more than one message may be handled concurrently, in particular
// messages coming from the control socket.
func handleMsg(msg *kernel.Message, goExec *goexec.State) (err error) {
	_ = goExec
	if !msg.Ok() {
		return errors.WithMessagef(msg.Error(), "shell message error")
	}
	//log.Printf("\tReceived message from shell: %+v\n", msg.Composed)

	// Tell the front-end that the kernel is working and when finished notify the
	// front-end that the kernel is idle again.
	if err = kernel.PublishKernelStatus(msg, kernel.StatusBusy); err != nil {
		err = errors.WithMessagef(err, "publishing kernel status %q", kernel.StatusBusy)
		return
	}

	// Defer publishing of status idle again, before returning.
	defer func() {
		newErr := kernel.PublishKernelStatus(msg, kernel.StatusIdle)
		if err == nil && newErr != nil {
			err = errors.WithMessagef(err, "publishing kernel status %q", kernel.StatusBusy)
		}
	}()

	switch msg.Composed.Header.MsgType {
	case "kernel_info_request":
		if err = kernel.SendKernelInfo(msg, Version); err != nil {
			err = errors.WithMessagef(err, "replying to 'kernel_info_request'")
		}
	case "shutdown_request":
		if err = handleShutdownRequest(msg); err != nil {
			err = errors.WithMessagef(err, "replying 'shutdown_request'")
		}
	case "execute_request":
		if err = handleExecuteRequest(msg, goExec); err != nil {
			err = errors.WithMessagef(err, "replying to 'execute_request'")
		}
	case "inspect_request":
		if err = handleInspectRequest(msg, goExec); err != nil {
			err = errors.WithMessagef(err, "replying to 'inspect_request'")
		}
	case "is_complete_request":
		log.Printf("Received is_complete_request: ignoring, since it's not a console like kernel.")
	case "complete_request":
		if err := handleCompleteRequest(msg, goExec); err != nil {
			log.Fatal(err)
		}
	default:
		// Log, ignore, and hope for the best.
		log.Printf("unhandled shell message %q", msg.Composed.Header.MsgType)
	}
	return
}

// handleShutdownRequest sends a "shutdown" message.
func handleShutdownRequest(msg *kernel.Message) error {
	content := msg.Composed.Content.(map[string]interface{})
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
	log.Printf("Shutting down in response to shutdown_request")
	msg.Kernel.Stop()
	return nil
}

type OutErr struct {
	out io.Writer
	err io.Writer
}

// handleInspectRequest presents rich data (HTML?) with contextual information for the
// contents under the cursor.
func handleInspectRequest(msg *kernel.Message, goExec *goexec.State) error {
	_ = goExec
	// TODO: Implementing this likely requiring generating the main.go -- and tracking
	//       the adjusted cursor position -- and using `gopls` to request contextual information.
	//       Similar to handleIsCompleteRequest.
	content := msg.Composed.Content.(map[string]interface{})
	code := content["code"].(string)
	cursorPos := content["cursor_pos"].(float64)
	detailLevel := content["detail_level"].(float64)
	_ = code
	_ = cursorPos
	_ = detailLevel

	log.Printf("inspect_request: cursorPos=%f, detailLevel=%f", cursorPos, detailLevel)
	replyContent := fmt.Sprintf("<div>Cursor at %f<br/>\n<pre>%s</pre></div>", cursorPos, code)
	reply := &kernel.InspectReply{
		Status:   "ok",
		Found:    false,
		Data:     make(kernel.MIMEMap),
		Metadata: make(kernel.MIMEMap),
	}
	reply.Data[string(protocol.MIMETextHTML)] = replyContent
	return msg.Reply("inspect_reply", reply)
}

// handleCompleteRequest replies with a `complete_reply` message, to auto-complete code.
func handleCompleteRequest(msg *kernel.Message, goExec *goexec.State) error {
	_ = goExec
	// TODO: Implementing this likely requiring generating the main.go -- and tracking
	//       the adjusted cursor position -- and using `gopls` to request contextual information.
	//       Similar to handleInspectRequest.
	// Extract the data from the request.
	content := msg.Composed.Content.(map[string]interface{})
	code := content["code"].(string)
	cursorPos := content["cursor_pos"].(float64)
	_ = code
	_ = cursorPos

	log.Printf("\tCompleteRequest")
	reply := &kernel.CompleteReply{
		Status:      "ok",
		Matches:     []string{},
		CursorStart: 0,
		CursorEnd:   0,
		Metadata:    make(kernel.MIMEMap),
	}
	return msg.Reply("complete_reply", reply)
}

// handleExecuteRequest runs code from an execute_request method,
// and sends the various reply messages.
func handleExecuteRequest(msg *kernel.Message, goExec *goexec.State) error {
	// Extract the data from the request.
	content := msg.Composed.Content.(map[string]interface{})
	code := content["code"].(string)
	msg.Silent = content["silent"].(bool)
	msg.StoreHistory = content["store_history"].(bool)

	// Prepare the map that will hold the reply content.
	replyContent := make(map[string]interface{})
	if msg.StoreHistory {
		msg.Kernel.ExecCounter++
		replyContent["execution_count"] = msg.Kernel.ExecCounter
	}

	// Tell the front-end what the kernel is about to execute.
	if !msg.Silent {
		if err := kernel.PublishExecutionInput(msg, msg.Kernel.ExecCounter, code); err != nil {
			return errors.WithMessagef(err, "publishing execution input")
		}
	}

	// Dispatch to various executors.
	msg.Kernel.Interrupted.Store(false)
	lines := strings.Split(code, "\n")
	usedLines := make(map[int]bool)
	var executionErr error
	if err := specialcmd.Exec(msg, goExec, lines, usedLines); err != nil {
		executionErr = errors.WithMessagef(err, "executing special commands in cell")
	}
	hasMoreToRun := len(usedLines) < len(lines)
	if executionErr == nil && !msg.Kernel.Interrupted.Load() && hasMoreToRun {
		executionErr = goExec.ExecuteCell(msg, lines, usedLines)
	}

	// Final execution result.
	if executionErr == nil {
		// if the only non-nil value should be auto-rendered graphically, render it
		replyContent["status"] = "ok"
		replyContent["user_expressions"] = make(map[string]string)
	} else {
		replyContent["status"] = "error"
		replyContent["ename"] = "ERROR"
		replyContent["evalue"] = executionErr.Error()
		replyContent["traceback"] = nil

		// Publish an execution_error message.
		if err := msg.PublishExecutionError(executionErr.Error(), []string{executionErr.Error()}); err != nil {
			return errors.WithMessagef(err, "publishing back execution error")
		}
	}

	// Send the output back to the notebook.
	if err := msg.Reply("execute_reply", replyContent); err != nil {
		return errors.WithMessagef(err, "publish 'execute_reply`")
	}
	return nil
}
