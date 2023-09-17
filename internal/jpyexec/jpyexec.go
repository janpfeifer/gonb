// Package jpyexec handles the execution of programs piping the output to Jupyter.
//
// Presumably Go programs compiled from the notebook's cells contents, but could be anything.
//
// It optionally supports creating two named pipes (one in each direction) that allows:
//
// * Display of rich content: HTML, image, SVG, markdown, etc.
// * Widgets communication back-and-forth with the front-end.
package jpyexec

import (
	"github.com/janpfeifer/gonb/kernel"
	"github.com/pkg/errors"
	"io"
	"k8s.io/klog/v2"
	osexec "os/exec"
	"sync"
	"time"
)

// Executor holds the configuration and state when executing a command that is piped to Jupyter.
// Use New to create it.
type Executor struct {
	// Configuration, before execution
	msg                        kernel.Message
	executionCount             int
	command                    string
	args                       []string
	dir                        string
	useNamedPipes              bool
	stdoutWriter, stderrWriter io.Writer
	millisecondsToInput        int
	inputPassword              bool

	// State when execution starts (after call to Exec)
	cmd                                      *osexec.Cmd
	cmdStdout, cmdStderr                     io.ReadCloser
	cmdStdin                                 io.WriteCloser
	namedPipeReaderPath, namedPipeWriterPath string
	pipeReader                               io.ReadCloser  // GONB_PIPE
	pipeWriter                               io.WriteCloser // GONB_PIPE_BACK

	isDone   bool
	doneChan chan struct{}
	muDone   sync.Mutex
}

// New creates an executor for the given command plus arguments,
// and pipe the output to Jupyter stdout and stderr streams connected to msg.
//
// It returns a configuration/state Executor object, that can be further configured.
// Call Exec when the configuration is isDone to actually execute the command.
//
// See `UseNamedPipes` to add support for rich data (HTML, PNG, SVG, Markdown, etc.) and widgets.
// See `gonb/gonbui` and `gonb/gonbui/widgets`.
func New(msg kernel.Message, command string, args ...string) *Executor {
	return &Executor{
		msg:                 msg,
		executionCount:      -1,
		command:             command,
		args:                args,
		millisecondsToInput: -1,
	}
}

// UseNamedPipes enables the creation of the side named pipes that add support for
// rich data (HTML, PNG, SVG, Markdown, etc.) and widgets.
// See `gonb/gonbui` and `gonb/gonbui/widgets`.
func (exec *Executor) UseNamedPipes() *Executor {
	exec.useNamedPipes = true
	return exec
}

// ExecutionCount sets the "execution_count" updated field when replying to an "execute_request" message.
// If set it publishes data as "execute_result" messages, as opposed to "display_data".
//
// For the most practical purposes, they work the same.
// But since the protocol seems to distinguish them, there is an option to set it.
func (exec *Executor) ExecutionCount(c int) *Executor {
	exec.executionCount = c
	return exec
}

// InDir configures the Executor to execute within the given directory. Returns
// the modified builder.
func (exec *Executor) InDir(dir string) *Executor {
	exec.dir = dir
	return exec
}

// WithStderr configures piping of stderr to the given `io.Writer`.
func (exec *Executor) WithStderr(stderrWriter io.Writer) *Executor {
	exec.stderrWriter = stderrWriter
	return exec
}

// WithStdout configures piping of stderr to the given `io.Writer`.
func (exec *Executor) WithStdout(stdoutWriter io.Writer) *Executor {
	exec.stdoutWriter = stdoutWriter
	return exec
}

// WithInputs configures the Executor to also plumb the input from Jupyter input prompt.
//
// The prompt is displayed after millisecondsWait: so if the program exits quickly, nothing
// is displayed.
//
// If running Go programs, it's better to use widgets for input. Jupyter input mechanism is
// cumbersome.
func (exec *Executor) WithInputs(millisecondsWait int) *Executor {
	exec.millisecondsToInput = millisecondsWait
	exec.inputPassword = false
	return exec
}

// WithPassword configures the Executor to also plumb
// the input from Jupyter password input (hidden).
//
// The prompt is displayed after millisecondsWait: so if the program exits quickly, nothing
// is displayed.
//
// If running Go programs, it's better to use widgets for input. Jupyter input mechanism is
// cumbersome.
func (exec *Executor) WithPassword(millisecondsWait int) *Executor {
	exec.millisecondsToInput = millisecondsWait
	exec.inputPassword = true
	return exec
}

// Exec executes the configured New configuration.
//
// It returns an error if it failed to execute or created the pipes -- but not if the executed
// program returns an error for any reason.
func (exec *Executor) Exec() error {
	klog.Infof("Executing: %s %v", exec.command, exec.args)
	exec.isDone = false
	exec.doneChan = make(chan struct{})

	// Make sure everyone is signal about program finished.
	// Notice this is called even if there are errors during the setup, so the various
	// writers/readers that were created are closed, even if the program was not executed.
	defer exec.done()

	cmd := osexec.Command(exec.command, exec.args...)
	exec.cmd = cmd
	cmd.Dir = exec.dir

	var err error
	exec.cmdStdout, err = cmd.StdoutPipe()
	if err != nil {
		return errors.WithMessagef(err, "failed to create pipe for stdout")
	}
	exec.cmdStderr, err = cmd.StderrPipe()
	if err != nil {
		return errors.WithMessagef(err, "failed to create pipe for stderr")
	}
	exec.cmdStdin, err = cmd.StdinPipe()
	if err != nil {
		return errors.WithMessagef(err, "failed to create pipe for stdin")
	}

	// Pipe all stdout and stderr to Jupyter (or the provided `io.Writer`'s).
	if exec.stdoutWriter == nil {
		exec.stdoutWriter = kernel.NewJupyterStreamWriter(exec.msg, kernel.StreamStdout)
	}
	if exec.stderrWriter == nil {
		exec.stderrWriter = kernel.NewJupyterStreamWriter(exec.msg, kernel.StreamStderr)
	}
	var streamersWG sync.WaitGroup
	streamersWG.Add(2)
	go func() {
		defer streamersWG.Done()
		_, err := io.Copy(exec.stdoutWriter, exec.cmdStdout)
		if err != nil {
			klog.Errorf("Failed copying execution stdout: %+v", err)
		}
	}()
	go func() {
		defer streamersWG.Done()
		_, err := io.Copy(exec.stderrWriter, exec.cmdStderr)
		if err != nil && err != io.EOF {
			klog.Errorf("Failed copying execution stderr: %+v", err)
		}
	}()

	// Handle Jupyter input.
	if exec.millisecondsToInput > 0 {
		exec.handleJupyterInput()
	}

	// Handle named pipes (for rich data output and widgets).
	if exec.useNamedPipes {
		if err = exec.handleNamedPipes(); err != nil {
			return err
		}
	}

	// Start command.
	if err := cmd.Start(); err != nil {
		klog.Warningf("Failed to start command %q", exec.command)
		return errors.WithMessagef(err, "failed to start to execute command %q", exec.command)
	}

	// Wait for output pipes to finish.
	streamersWG.Wait()
	if err := cmd.Wait(); err != nil {
		errMsg := err.Error() + "\n"
		if exec.msg.Kernel().Interrupted.Load() {
			errMsg = "^C\n" + errMsg
		}
		_ = kernel.PublishWriteStream(exec.msg, kernel.StreamStderr, errMsg)
	}

	klog.V(2).Infof("Execution finished successfully")
	// Notice some of the cleanup will happen in parallel after return,
	// triggered by the deferred exec.done() that gets executed.
	return nil
}

// done signals program finished executing, and triggers the closing of everything.
func (exec *Executor) done() {
	exec.muDone.Lock()
	defer exec.muDone.Unlock()
	if exec.isDone {
		// Already closed.
		return
	}
	exec.isDone = true
	if exec.millisecondsToInput > 0 {
		_ = exec.msg.CancelInput()
	}
	_ = exec.cmdStdin.Close()
	close(exec.doneChan)
	_ = exec.cmdStderr.Close()
	_ = exec.cmdStdout.Close()
}

// handleJupyterInput should only be called if exec.millisecondsToInput is set.
func (exec *Executor) handleJupyterInput() {
	// Set function to handle incoming content.
	var writeStdinFn kernel.OnInputFn
	schedulePromptFn := func() {
		// Wait for the given time, and if command still running, ask
		// Jupyter for stdin input.
		time.Sleep(time.Duration(exec.millisecondsToInput) * time.Millisecond)
		klog.V(2).Infof("%d milliseconds elapsed, prompt for input", exec.millisecondsToInput)
		exec.muDone.Lock()
		if !exec.isDone {
			_ = exec.msg.PromptInput(" ", exec.inputPassword, writeStdinFn)
		}
		exec.muDone.Unlock()
	}
	writeStdinFn = func(original, input *kernel.MessageImpl) error {
		exec.muDone.Lock()
		defer exec.muDone.Unlock()
		if exec.isDone {
			return nil
		}
		content := input.Composed.Content.(map[string]any)
		value := content["value"].(string) + "\n"
		klog.V(2).Infof("stdin value: %q", value)
		go func() {
			// Write concurrently, not to block, in case program doesn't
			// actually read anything from the stdin.
			_, err := exec.cmdStdin.Write([]byte(value))
			if err != nil {
				// Could happen if something was not fully written, and channel was closed, in
				// which case it's ok.
				klog.Warningf("failed to write to stdin of %q %v: %+v", exec.command, exec.args, err)
			}
		}()
		// Reschedule itself for the next message.
		go schedulePromptFn()
		return nil
	}
	go schedulePromptFn()
}
