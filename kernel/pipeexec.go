package kernel

import (
	"github.com/janpfeifer/gonb/gonbui/protocol"
	"io"
	"k8s.io/klog/v2"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"

	"github.com/pkg/errors"
)

// PipeExecToJupyterBuilder holds the configuration to executing a command that is piped to Jupyter.
// Use PipeExecToJupyter to create it.
type PipeExecToJupyterBuilder struct {
	msg     Message
	command string
	args    []string
	dir     string

	stdoutWriter, stderrWriter io.Writer

	millisecondsToInput int
	inputPassword       bool
}

// PipeExecToJupyter creates a builder that will execute the given command (command plus arguments)
// and pipe the output to Jupyter stdout and stderr streams connected to msg.
//
// It returns a builder object that can be further configured. Call Exec when the configuration is
// done to actually execute the command.
func PipeExecToJupyter(msg Message, command string, args ...string) *PipeExecToJupyterBuilder {
	return &PipeExecToJupyterBuilder{
		msg:                 msg,
		command:             command,
		args:                args,
		millisecondsToInput: -1,
	}
}

// InDir configures the PipeExecToJupyterBuilder to execute within the given directory. Returns
// the modified builder.
func (builder *PipeExecToJupyterBuilder) InDir(dir string) *PipeExecToJupyterBuilder {
	builder.dir = dir
	return builder
}

// WithStderr configures piping of stderr to the given `io.Writer`.
func (builder *PipeExecToJupyterBuilder) WithStderr(stderrWriter io.Writer) *PipeExecToJupyterBuilder {
	builder.stderrWriter = stderrWriter
	return builder
}

// WithStdout configures piping of stderr to the given `io.Writer`.
func (builder *PipeExecToJupyterBuilder) WithStdout(stdoutWriter io.Writer) *PipeExecToJupyterBuilder {
	builder.stdoutWriter = stdoutWriter
	return builder
}

// WithInput configures the PipeExecToJupyterBuilder to also plumb
// the input from Jupyter input prompt.
//
// The prompt is displayed after millisecondsWait: so if the program exits quickly, nothing
// is displayed.
func (builder *PipeExecToJupyterBuilder) WithInput(millisecondsWait int) *PipeExecToJupyterBuilder {
	builder.millisecondsToInput = millisecondsWait
	builder.inputPassword = false
	return builder
}

// WithPassword configures the PipeExecToJupyterBuilder to also plumb
// the input from Jupyter password input (hidden).
//
// The prompt is displayed after millisecondsWait: so if the program exits quickly, nothing
// is displayed.
func (builder *PipeExecToJupyterBuilder) WithPassword(millisecondsWait int) *PipeExecToJupyterBuilder {
	builder.millisecondsToInput = millisecondsWait
	builder.inputPassword = true
	return builder
}

// Exec executes the configured PipeExecToJupyter configuration.
//
// It returns an error if it failed to execute or created the pipes -- but not if the executed
// program returns an error for any reason.
func (builder *PipeExecToJupyterBuilder) Exec() error {
	klog.Infof("Executing: %s %v", builder.command, builder.args)

	cmd := exec.Command(builder.command, builder.args...)
	cmd.Dir = builder.dir

	cmdStdout, err := cmd.StdoutPipe()
	if err != nil {
		return errors.WithMessagef(err, "failed to create pipe for stdout")
	}
	cmdStderr, err := cmd.StderrPipe()
	if err != nil {
		return errors.WithMessagef(err, "failed to create pipe for stderr")
	}

	// Pipe all stdout and stderr to Jupyter.
	if builder.stdoutWriter == nil {
		builder.stdoutWriter = NewJupyterStreamWriter(builder.msg, StreamStdout)
	}
	if builder.stderrWriter == nil {
		builder.stderrWriter = NewJupyterStreamWriter(builder.msg, StreamStderr)
	}
	var streamersWG sync.WaitGroup
	streamersWG.Add(2)
	go func() {
		defer streamersWG.Done()
		_, err := io.Copy(builder.stdoutWriter, cmdStdout)
		if err != nil {
			klog.Errorf("Failed copying execution stdout: %+v", err)
		}
	}()
	go func() {
		defer streamersWG.Done()
		_, err := io.Copy(builder.stderrWriter, cmdStderr)
		if err != nil && err != io.EOF {
			klog.Errorf("Failed copying execution stderr: %+v", err)
		}
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
	if builder.millisecondsToInput > 0 {
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
			klog.V(2).Infof("stdin value: %q", value)
			go func() {
				// Write concurrently, not to block, in case program doesn't
				// actually read anything from the stdin.
				_, err := cmdStdin.Write([]byte(value))
				if err != nil {
					// Could happen if something was not fully written, and channel was closed, in
					// which case it's ok.
					klog.Warningf("failed to write to stdin of %q %v: %+v", builder.command, builder.args, err)
				}
			}()
			// Reschedule itself for the next message.
			if !builder.inputPassword {
				_ = builder.msg.PromptInput(" ", builder.inputPassword, writeStdinFn)
			}
			return err
		}
		go func() {
			// Wait for the given time, and if command still running, ask
			// Jupyter for stdin input.
			time.Sleep(time.Duration(builder.millisecondsToInput) * time.Millisecond)
			klog.V(2).Infof("%d milliseconds elapsed, prompt for input", builder.millisecondsToInput)
			muDone.Lock()
			if !done {
				_ = builder.msg.PromptInput(" ", builder.inputPassword, writeStdinFn)
			}
			muDone.Unlock()
		}()
	}

	// Prepare named-pipe to use for rich-data display.
	if err = StartNamedPipe(builder.msg, builder.dir, doneChan); err != nil {
		return errors.WithMessagef(err, "failed to create named pipe for display content")
	}

	// Define function to proper closing of the various concurrent plumbing
	doneFn := func() {
		muDone.Lock()
		done = true
		if builder.millisecondsToInput > 0 {
			_ = builder.msg.CancelInput()
		}
		_ = cmdStdin.Close()
		close(doneChan)
		muDone.Unlock()
	}

	// Start command.
	if err := cmd.Start(); err != nil {
		klog.Warningf("Failed to start command %q", builder.command)
		_ = cmdStderr.Close()
		_ = cmdStdout.Close()
		doneFn()
		return errors.WithMessagef(err, "failed to start to execute command %q", builder.command)
	}

	// Wait for output pipes to finish.
	streamersWG.Wait()
	if err := cmd.Wait(); err != nil {
		errMsg := err.Error() + "\n"
		if builder.msg.Kernel().Interrupted.Load() {
			errMsg = "^C\n" + errMsg
		}
		_ = PublishWriteStream(builder.msg, StreamStderr, errMsg)
	}
	doneFn()

	klog.V(2).Infof("Execution finished successfully")
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
				_ = w.Close()
			}
		}
		muFifo.Unlock()
		_ = os.Remove(pipePath)
	}()

	_ = os.Setenv(protocol.GONB_PIPE_ENV, pipePath)
	go func() {
		// Notice that opening pipeReader below blocks, until the other end
		// (the go program being executed) opens it as well.
		var pipeReader *os.File
		pipeReader, err = os.Open(pipePath)
		if err != nil {
			klog.Warningf("Failed to open pipe (Mkfifo) %q for reading: %+v", pipePath, err)
			return
		}
		muFifo.Lock()
		fifoOpenedForReading = true
		muFifo.Unlock()
		go PollDisplayRequests(msg, pipeReader)

		// Wait till channel is closed and then close reader.
		<-doneChan
		_ = pipeReader.Close()
	}()
	return nil
}
