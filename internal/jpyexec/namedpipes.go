package jpyexec

// This file implements the polling on the $GONB_PIPE and $GONB_PIPE_WIDGETS named pipes created
// to receive information from the program being executed and to send information from the
// widgets.
//
// It has a protocol (defined under `gonbui/protocol`) to display rich content.

import (
	"encoding/gob"
	"fmt"
	"github.com/janpfeifer/gonb/gonbui/protocol"
	"github.com/janpfeifer/gonb/internal/kernel"
	"github.com/pkg/errors"
	"io"
	"k8s.io/klog/v2"
	"os"
	"sync"
	"syscall"
)

func init() {
	// Register generic gob types we want to make sure are understood.
	gob.Register(map[string]any{})
	gob.Register([]string{})
	gob.Register([]any{})
}

// CommsHandler interface is used if Executor.UseNamedPipes is called, and a CommsHandler
// is provided.
//
// It is assumed there is at most one program being executed at a time. GoNB will never
// execute two cells simultaneously.
type CommsHandler interface {
	// ProgramStart is called when the program execution is about to start.
	// If program start failed (e.g.: during creation of pipes), ProgramStart may not be called,
	// and yet ProgramFinished is called.
	ProgramStart(exec *Executor)

	// ProgramFinished is called when the program execution finishes.
	// Notice this may be called even if ProgramStart has not been called, if the execution
	// failed during the creation of the various pipes.
	ProgramFinished()

	// ProgramSendValueRequest is called when the program requests a value to be sent to an address.
	ProgramSendValueRequest(address string, value any)

	// ProgramReadValueRequest handler.
	ProgramReadValueRequest(address string)

	// ProgramSubscribeRequest handler.
	ProgramSubscribeRequest(address string)

	// ProgramUnsubscribeRequest handler.
	ProgramUnsubscribeRequest(address string)
}

// PipeWriterFifoBufferSize is the number of CommValue messages that
// can be buffered when writing to the named pipe before dropping.
const PipeWriterFifoBufferSize = 128

// handleNamedPipes creates the named pipe and set up the goroutines to listen to them.
//
// TODO: make this more secure, maybe with a secret key also passed by the environment.
func (exec *Executor) handleNamedPipes() (err error) {
	exec.PipeWriterFifo = make(chan *protocol.CommValue, PipeWriterFifoBufferSize)

	// Create temporary named pipes in both directions.
	exec.namedPipeReaderPath, err = exec.createTmpFifo()
	if err != nil {
		return errors.Wrapf(err, "creating named pipe used to read from program %s", exec.cmd)
	}
	exec.namedPipeWriterPath, err = exec.createTmpFifo()
	if err != nil {
		return errors.Wrapf(err, "creating named pipe used to write to program %s", exec.cmd)
	}
	exec.cmd.Env = append(exec.cmd.Environ(),
		protocol.GONB_PIPE_ENV+"="+exec.namedPipeReaderPath,
		protocol.GONB_PIPE_BACK_ENV+"="+exec.namedPipeWriterPath)

	exec.openPipeReader()
	exec.openPipeWriter()
	return
}

func (exec *Executor) createTmpFifo() (string, error) {
	// Create a temporary file name.
	f, err := os.CreateTemp(exec.dir, "gonb_pipe_")
	if err != nil {
		return "", err
	}
	pipePath := f.Name()
	if err = f.Close(); err != nil {
		return "", err
	}
	if err = os.Remove(pipePath); err != nil {
		return "", err
	}

	// Create pipe.
	if err = syscall.Mkfifo(pipePath, 0600); err != nil {
		return "", errors.Wrapf(err, "failed to create pipe (Mkfifo) for %q", pipePath)
	}
	return pipePath, nil
}

// openPipeReader opens `exec.namedPipeReaderPath` and handles its proper closing, and removal of
// the named pipe when program execution is finished.
//
// The doneChan is listened to: when it is closed, it will trigger the listener goroutine to close the pipe,
// remove it and quit.
func (exec *Executor) openPipeReader() {
	// Synchronize pipe: if it's not opened by the program being executed,
	// we have to open it ourselves for writing, to avoid blocking
	// `os.Open` (it waits the other end of the fifo to be opened before returning).
	// See discussion in:
	// https://stackoverflow.com/questions/75255426/how-to-interrupt-a-blocking-os-open-call-waiting-on-a-fifo-in-go
	var muFifo sync.Mutex
	fifoOpenedForReading := false

	go func() {
		// Clean up after program is over, there are two scenarios:
		// 1. The executed program opened the pipe: then we just remove the pipePath.
		// 2. The executed program never opened the pipe: then the other end (goroutine
		//    below) will be forever blocked on os.Open call.
		<-exec.doneChan
		muFifo.Lock()
		if !fifoOpenedForReading {
			w, err := os.OpenFile(exec.namedPipeReaderPath, os.O_WRONLY, 0600)
			if err == nil {
				// Closing it allows the open of the pipe for reading (below) to unblock.
				_ = w.Close()
			}
		}
		muFifo.Unlock()
		_ = os.Remove(exec.namedPipeReaderPath)
	}()

	go func() {
		klog.V(2).Infof("Opening named pipeReader in %q", exec.namedPipeReaderPath)
		if exec.isDone {
			// In case program execution interrupted early.
			return
		}
		// Notice that opening pipeReader below blocks, until the other end
		// (the go program being executed) opens it as well.
		var err error
		exec.pipeReader, err = os.Open(exec.namedPipeReaderPath)
		if err != nil {
			klog.Warningf("Failed to open pipe (Mkfifo) %q for reading: %+v", exec.namedPipeReaderPath, err)
			return
		}
		klog.V(2).Infof("Opened named pipeReader in %q", exec.namedPipeReaderPath)
		muFifo.Lock()
		fifoOpenedForReading = true
		defer muFifo.Unlock()

		// Start polling of the pipeReader.
		go exec.pollNamedPipeReader()

		// Wait program execution to finish to close reader (in case it is not yet closed).
		<-exec.doneChan
		_ = exec.pipeReader.Close()
		_ = os.Remove(exec.namedPipeReaderPath)
	}()
}

// pollNamedPipeReader will continuously read for incoming requests with displaying content
// on the notebook or widgets updates.
func (exec *Executor) pollNamedPipeReader() {
	decoder := gob.NewDecoder(exec.pipeReader)
	for {
		data := &protocol.DisplayData{}
		err := decoder.Decode(data)
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrClosedPipe) || errors.Is(err, os.ErrClosed) {
			return
		} else if err != nil {
			klog.Infof("Named pipe: failed to parse message: %+v", err)
			return
		}

		// Special case for a request for input:
		if reqAny, found := data.Data[protocol.MIMEJupyterInput]; found {
			klog.V(2).Infof("Received InputRequest: %v", reqAny)
			req, ok := reqAny.(*protocol.InputRequest)
			if !ok {
				exec.reportCellError(errors.New("A MIMEJupyterInput sent to GONB_PIPE without an associated protocol.InputRequest!?"))
				continue
			}
			exec.dispatchInputRequest(req)
			continue
		}

		// CommValue: update or read value in the front-end.
		if reqAny, found := data.Data[protocol.MIMECommValue]; found {
			req, ok := reqAny.(protocol.CommValue)
			if !ok {
				exec.reportCellError(errors.Errorf(
					"Invalid message sent in named pipes to GoNB from cell, "+
						"this may affect widgets communication -- "+
						"MIMECommValue sent to $GONB_PIPE_BACK without an associated `protocol.CommValue` "+
						"type, got %T instead", reqAny))
				continue
			}

			// Special addresses:
			if req.Address == protocol.GonbuiSyncAddress {
				syncId, ok := req.Value.(int)
				if !ok {
					klog.Errorf("comms: Receive Sync request with invalid value %+v. Communication with cell program may be left in an unusable state!", req)
					continue
				}
				klog.V(2).Infof("comms: Received Sync(%d) at %q, sending back ack", syncId, req.Address)
				// Acknowledge with a reply to the special address.
				exec.PipeWriterFifo <- &protocol.CommValue{
					Address: protocol.GonbuiSyncAckAddress,
					Value:   syncId,
				}
				continue
			}

			if exec.commsHandler == nil {
				klog.V(2).Infof("Received and dropped (no handler registered) CommValue: %+v", req)
			} else if req.Request {
				klog.V(2).Infof("ProgramReadValueRequest(%q) requested", req.Address)
				exec.commsHandler.ProgramReadValueRequest(req.Address)
			} else {
				klog.V(2).Infof("ProgramSendValueRequest(%q, %v) requested", req.Address, req.Value)
				exec.commsHandler.ProgramSendValueRequest(req.Address, req.Value)
			}
			continue
		}

		// ProgramSubscribeRequest: (un-)subscribe to address in the front-end.
		if reqAny, found := data.Data[protocol.MIMECommSubscribe]; found {
			req, ok := reqAny.(protocol.CommSubscription)
			if !ok {
				exec.reportCellError(errors.Errorf(
					"Invalid message sent in named pipes to GoNB from cell, "+
						"this may affect widgets communication -- "+
						"MIMECommSubscribe sent to $GONB_PIPE_BACK without an associated `protocol.CommSubscription` "+
						"type, got %T instead", reqAny))
				continue
			}
			if exec.commsHandler == nil {
				klog.V(2).Infof("Received and dropped (no handler registered) ProgramSubscribeRequest: %+v", req)
			} else if req.Unsubscribe {
				klog.V(2).Infof("ProgramUnsubscribeRequest(%q) requested", req.Address)
				exec.commsHandler.ProgramUnsubscribeRequest(req.Address)
			} else {
				klog.V(2).Infof("ProgramSubscribeRequest(%q) requested", req.Address)
				exec.commsHandler.ProgramSubscribeRequest(req.Address)
			}
			continue
		}

		// Otherwise, just display with the corresponding MIME type:
		exec.dispatchDisplayData(data)
	}
}

// reportCellError reports error to both, the notebook and the standard logger (gonb's stderr).
func (exec *Executor) reportCellError(err error) {
	errStr := fmt.Sprintf("%+v", err) // Error with stack.
	klog.Errorf("%s", errStr)
	err = kernel.PublishWriteStream(exec.Msg, kernel.StreamStderr, "GoNB Error:\n"+errStr)
	if err != nil {
		klog.Errorf("%+v", errors.WithStack(err))
	}
}

// dispatchDisplayData received through the named pipe (`$GONB_PIPE`).
func (exec *Executor) dispatchDisplayData(data *protocol.DisplayData) {
	// Log info about what is being displayed.
	msgData := kernel.Data{
		Data:      make(kernel.MIMEMap, len(data.Data)),
		Metadata:  make(kernel.MIMEMap),
		Transient: make(kernel.MIMEMap),
	}
	for mimeType, content := range data.Data {
		msgData.Data[string(mimeType)] = content
	}
	if klog.V(1).Enabled() {
		kernel.LogDisplayData(msgData.Data)
	}
	for key, content := range data.Metadata {
		msgData.Metadata[key] = content
	}
	var err error
	if data.DisplayID != "" {
		msgData.Transient["display_id"] = data.DisplayID
		err = kernel.PublishUpdateDisplayData(exec.Msg, msgData)
	} else {
		err = kernel.PublishData(exec.Msg, msgData)
	}
	if err != nil {
		klog.Errorf("Failed to display data (ignoring): %v", err)
	}
}

// dispatchInputRequest uses the standard Jupyter input mechanism.
// It is fundamentally broken -- it locks the UI even if the program already stopped running --
// so we suggest using the `gonb/gonbui/widgets` API instead.
func (exec *Executor) dispatchInputRequest(req *protocol.InputRequest) {
	klog.V(2).Infof("Received InputRequest %+v", req)
	writeStdinFn := func(original, input *kernel.MessageImpl) error {
		content := input.Composed.Content.(map[string]any)
		value := content["value"].(string) + "\n"
		klog.V(2).Infof("stdin value: %q", value)
		go func() {
			exec.muDone.Lock()
			cmdStdin := exec.cmdStdin
			exec.muDone.Unlock()
			if exec.isDone {
				return
			}
			// Write concurrently, not to block, in case program doesn't
			// actually read anything from the stdin.
			_, err := cmdStdin.Write([]byte(value))
			if err != nil {
				// Could happen if something was not fully written, and channel was closed, in
				// which case it's ok.
				klog.Warningf("failed to write to stdin of cell: %+v", err)
			}
		}()
		return nil
	}
	err := exec.Msg.PromptInput(req.Prompt, req.Password, writeStdinFn)
	if err != nil {
		exec.reportCellError(err)
	}
}

// openPipeWriter opens `exec.namedPipeWriterPath` and handles its proper closing, and removal of
// the named pipe when program execution is finished.
//
// The doneChan is listened to: when it is closed, it will trigger the listener goroutine to close the pipe,
// remove it and quit.
func (exec *Executor) openPipeWriter() {
	// Synchronize pipe: if it's not opened by the program being executed,
	// we have to open it ourselves for writing, to avoid blocking
	// `os.Open` (it waits the other end of the fifo to be opened before returning).
	// See discussion in:
	// https://stackoverflow.com/questions/75255426/how-to-interrupt-a-blocking-os-open-call-waiting-on-a-fifo-in-go
	var muFifo sync.Mutex
	fifoOpened := false

	go func() {
		// Clean up after program is over, there are two scenarios:
		// 1. The executed program opened the pipe: then we just remove the pipePath.
		// 2. The executed program never opened the pipe: then the other end (goroutine
		//    below) will be forever blocked on os.Open call.
		<-exec.doneChan
		muFifo.Lock()
		if !fifoOpened {
			r, err := os.OpenFile(exec.namedPipeWriterPath, os.O_RDONLY, 0600)
			if err == nil {
				// Closing it allows the open of the pipe for writing (below) to unblock.
				_ = r.Close()
			}
		}
		muFifo.Unlock()
		_ = os.Remove(exec.namedPipeWriterPath)
	}()

	go func() {
		klog.V(2).Infof("Opening named pipeWriter in %q", exec.namedPipeWriterPath)
		if exec.isDone {
			// In case program execution interrupted early.
			klog.Warningf("Opening of NamedPipeWriter in %q failed, since program already stopped/crashed", exec.namedPipeWriterPath)
			return
		}
		// Notice that opening the pipe below blocks, until the other end (the go program being executed) opens it
		// as well.
		f, err := os.OpenFile(exec.namedPipeWriterPath, os.O_WRONLY, 0600)
		if err != nil {
			klog.Warningf("Failed to open pipe (Mkfifo) %q for writing: %+v", exec.namedPipeWriterPath, err)
			return
		}
		klog.V(2).Infof("Opened named pipeWriter in %q", exec.namedPipeWriterPath)
		muFifo.Lock()
		exec.pipeWriter = f
		fifoOpened = true
		defer muFifo.Unlock()

		// Start polling of the pipeReader.
		go exec.pollPipeWriterFifo()

		// Wait program execution to finish to close the writer (file and fifo).
		<-exec.doneChan
		close(exec.PipeWriterFifo)
		_ = exec.pipeWriter.Close()
		_ = os.Remove(exec.namedPipeWriterPath)
	}()
}

// pollPipeWriterFifo polls messages from `Executor.PipeWriterFifo` and encodes them to
// the named pipe writer.
func (exec *Executor) pollPipeWriterFifo() {
	encoder := gob.NewEncoder(exec.pipeWriter)
	klog.V(2).Infof("jpyexec: pollPipeWriterFifo() listening to requests.")
	for msg := range exec.PipeWriterFifo {
		if klog.V(2).Enabled() {
			klog.Infof("jpyexec: encoding %+v to named pipe to cell program", msg)
		}
		err := encoder.Encode(msg)
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrClosedPipe) || errors.Is(err, os.ErrClosed) {
			return
		} else if err != nil {
			klog.Infof("while writing to cell program, failed to encode message %+v. "+
				"Communication with cell program broken, widgets won't work properly. "+
				"You can try re-executing the cell. Error: %+v", msg, err)
			return
		}
	}
	klog.V(2).Infof("jpyexec: pollPipeWriterFifo() closed.")
}
