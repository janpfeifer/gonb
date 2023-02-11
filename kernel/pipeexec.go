package kernel

import (
	"github.com/janpfeifer/gonb/gonbui/protocol"
	"io"
	"log"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"

	"github.com/pkg/errors"
)

// PipeExecToJupyter executes the given command (command plus arguments) and pipe the output
// to Jupyter stdout and stderr streams connected to msg.
//
// If dir is not empty, before running the command the current directory is changed to dir.
//
// It returns an error if it failed to execute or created the pipes -- but not if the executed
// program returns an error for any reason.
func PipeExecToJupyter(msg Message, dir, name string, args ...string) error {
	return pipeExecToJupyter(msg, dir, name, args, -1, false)
}

// PipeExecToJupyterWithInput executes the given command (command plus arguments) and
// pipes the output and error to Jupyter stdout and stderr streams. It also plumbs
// the input from Jupyter input, after 500ms the program started (so if programs
// don't execute quick, and optional input will be made available).
//
// If dir is not empty, before running the command the current directory is changed to dir.
//
// It returns an error if it failed to execute or created the pipes -- but not if the executed
// program returns an error for any reason.
func PipeExecToJupyterWithInput(msg Message, dir, name string, args ...string) error {
	return pipeExecToJupyter(msg, dir, name, args, 500, false)
}

// PipeExecToJupyterWithPassword executes the given command (command plus arguments) and
// pipes the output and error to Jupyter stdout and stderr streams. It also plumbs
// one input from Jupyter input set as a password (input hidden).
//
// If dir is not empty, before running the command the current directory is changed to dir.
func PipeExecToJupyterWithPassword(msg Message, dir, name string, args ...string) error {
	return pipeExecToJupyter(msg, dir, name, args, 1, true)
}

func pipeExecToJupyter(msg Message, dir, name string, args []string, millisecondsToInput int, inputPassword bool) error {
	log.Printf("Executing: %s %v", name, args)

	cmd := exec.Command(name, args...)
	cmd.Dir = dir

	cmdStdout, err := cmd.StdoutPipe()
	if err != nil {
		return errors.WithMessagef(err, "failed to create pipe for stdout")
	}
	cmdStderr, err := cmd.StderrPipe()
	if err != nil {
		return errors.WithMessagef(err, "failed to create pipe for stderr")
	}

	// Pipe all stdout and stderr to Jupyter.
	jupyterStdout := NewJupyterStreamWriter(msg, StreamStdout)
	jupyterStderr := NewJupyterStreamWriter(msg, StreamStderr)
	var streamersWG sync.WaitGroup
	streamersWG.Add(2)
	go func() {
		defer streamersWG.Done()
		io.Copy(jupyterStdout, cmdStdout)
	}()
	go func() {
		defer streamersWG.Done()
		io.Copy(jupyterStderr, cmdStderr)
	}()

	// Optionally prepare stdin to start after millisecondsToInput.
	var (
		done     bool
		doneChan = make(chan struct{})
		muDone   sync.Mutex
		cmdStdin io.WriteCloser
	)
	cmdStdin, err = cmd.StdinPipe()
	if err != nil {
		return errors.WithMessagef(err, "failed to create pipe for stdin")
	}
	if millisecondsToInput > 0 {
		// Set function to handle incoming content.
		var writeStdinFn OnInputFn
		writeStdinFn = func(original, input *MessageImpl) error {
			muDone.Lock()
			defer muDone.Unlock()
			if done {
				return nil
			}
			content := input.Composed.Content.(map[string]any)
			value := content["value"].(string) + "\n"
			log.Printf("stdin value: %q", value)
			go func() {
				// Write concurrently, not to block, in case program doesn't
				// actually read anything from the stdin.
				_, err := cmdStdin.Write([]byte(value))
				if err != nil {
					// Could happen if something was not fully written, and channel was closed, in
					// which case it's ok.
					log.Printf("failed to write to stdin of %q %v: %+v", name, args, err)
				}
			}()
			// Reschedule itself for the next message.
			if !inputPassword {
				msg.PromptInput(" ", inputPassword, writeStdinFn)
			}
			return err
		}
		go func() {
			// Wait for the given time, and if command still running, ask
			// Jupyter for stdin input.
			time.Sleep(time.Duration(millisecondsToInput) * time.Millisecond)
			muDone.Lock()
			if !done {
				msg.PromptInput(" ", inputPassword, writeStdinFn)
			}
			muDone.Unlock()
		}()
	}

	// Prepare named-pipe to use for rich-data display.
	if err = StartNamedPipe(msg, dir, doneChan); err != nil {
		return errors.WithMessagef(err, "failed to create named pipe for display content")
	}

	// Define function to proper closing of the various concurrent plumbing
	doneFn := func() {
		muDone.Lock()
		done = true
		if millisecondsToInput > 0 {
			msg.CancelInput()
		}
		cmdStdin.Close()
		close(doneChan)
		muDone.Unlock()
	}

	// Start command.
	if err := cmd.Start(); err != nil {
		cmdStderr.Close()
		cmdStdout.Close()
		doneFn()
		return errors.WithMessagef(err, "failed to start to execute command %q", name)
	}

	// Wait for output pipes to finish.
	streamersWG.Wait()
	if err := cmd.Wait(); err != nil {
		errMsg := err.Error() + "\n"
		if msg.Kernel().Interrupted.Load() {
			errMsg = "^C\n" + errMsg
		}
		PublishWriteStream(msg, StreamStderr, errMsg)
	}
	doneFn()

	log.Printf("Execution finished successfully")
	return nil
}

// StartNamedPipe creates a named pipe in `dir` and starts a listener (on a separate goroutine) that reads
// the pipe and displays rich content. It also exports environment variable GONB_FIFO announcing the name of the
// named pipe.
//
// The doneChan is listened to: when it is closed, it will trigger the listener goroutine to close the pipe,
// remove it and quit.
//
// TODO: make this more secure, maybe with a secret key also passed by the environment.
func StartNamedPipe(msg Message, dir string, doneChan <-chan struct{}) error {
	// Create a temporary file name.
	f, err := os.CreateTemp(dir, "gonb_pipe_")
	if err != nil {
		return err
	}
	pipePath := f.Name()
	if err = f.Close(); err != nil {
		return err
	}
	if err = os.Remove(pipePath); err != nil {
		return err
	}

	// Create pipe.
	if err = syscall.Mkfifo(pipePath, 0600); err != nil {
		return errors.Wrapf(err, "failed to create pipe (Mkfifo) for %q", pipePath)
	}

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
		<-doneChan
		muFifo.Lock()
		if !fifoOpenedForReading {
			w, err := os.OpenFile(pipePath, os.O_WRONLY, 0600)
			if err == nil {
				w.Close()
			}
		}
		muFifo.Unlock()
		os.Remove(pipePath)
	}()

	os.Setenv(protocol.GONB_PIPE_ENV, pipePath)
	go func() {
		// Notice that opening pipeReader below blocks, until the other end
		// (the go program being executed) opens it as well.
		var pipeReader *os.File
		pipeReader, err = os.Open(pipePath)
		if err != nil {
			log.Printf("Failed to open pipe (Mkfifo) %q for reading: %+v", pipePath, err)
			return
		}
		muFifo.Lock()
		fifoOpenedForReading = true
		muFifo.Unlock()
		go PollDisplayRequests(msg, pipeReader)

		// Wait till channel is closed and then close reader.
		<-doneChan
		pipeReader.Close()
	}()
	return nil
}
