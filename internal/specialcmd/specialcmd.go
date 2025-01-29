// Package specialcmd handles special (aka. "magic") commands, that come in two flavors:
//
//   - `%<cmd> {...args...}`: Control the environment (variables) and configure gonb.
//   - `!<shell commands>`: Execute shell commands.
//     Similar to the ipython kernel.
//
// In particular `%help` will print out currently available commands.
package specialcmd

import (
	_ "embed"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/janpfeifer/gonb/internal/jpyexec"
	"github.com/janpfeifer/gonb/version"
	"golang.org/x/exp/slices"

	. "github.com/janpfeifer/gonb/common"
	"github.com/janpfeifer/gonb/gonbui/protocol"
	"github.com/janpfeifer/gonb/internal/goexec"
	"github.com/janpfeifer/gonb/internal/kernel"
	"github.com/pkg/errors"
	"k8s.io/klog/v2"
)

// MillisecondsWaitForInput is the wait time for a bash script (started with `!` or `!*`
// special commands, when `%with_inputs` or `%with_password` is used) to run, before an
// input is prompted to the Jupyter Notebook.
const MillisecondsWaitForInput = 200

//go:embed help.md
var HelpMessage string

// cellStatus holds temporary status for the execution of the current cell.
type cellStatus struct {
	withInputs, withPassword bool
}

// Parse will check whether the given code to be executed has any special commands.
//
// Any special commands found in the code will be executed (if execute is set to true) and the corresponding lines used
// from the code will be returned in usedLines -- so they can be excluded from other executors (goexec).
//
// If any errors happen, it is returned in err.
func Parse(msg kernel.Message, goExec *goexec.State, execute bool, codeLines []string, usedLines Set[int]) (err error) {
	status := &cellStatus{}

	for lineNum, line := range codeLines {
		if usedLines.Has(lineNum) {
			continue
		}
		line = goexec.TrimGonbCommentPrefix(line)
		if len(line) > 1 && (line[0] == '%' || line[0] == '!') {
			var cmdStr string
			cmdStr = joinLine(codeLines, lineNum, usedLines) // Gather current line and following if line ends with a `\` symbol
			cmdType := cmdStr[0]
			cmdStr = cmdStr[1:]
			for cmdStr[0] == ' ' || cmdStr[0] == '\t' {
				cmdStr = cmdStr[1:] // Skip initial space
			}
			if len(cmdStr) == 0 {
				// Skip empty commands.
				continue
			}
			if execute {
				switch cmdType {
				case '%':
					err = execSpecialConfig(msg, goExec, cmdStr, status)
					if err != nil {
						return
					}
				case '!':
					err = execShell(msg, goExec, cmdStr, status)
					if err != nil {
						return
					}

					// Runs AutoTrack, in case go.mod has changed.
					err = goExec.AutoTrack()
					if err != nil {
						klog.Errorf("goExec.AutoTrack failed: %+v", err)
					}
				}
			}
		}
	}
	return
}

// joinLine starts from fromLine and joins consecutive lines if the current line terminates with a `\\\n`,
// allowing multi-line commands to be issued.
//
// It removes the `//gonb:` prefix from all the joined lines, if the prefix is present.
//
// It returns the joined lines with the '\\\n' replaced by a space, and appends the used lines (including
// fromLine) to usedLines.
func joinLine(lines []string, fromLine int, usedLines Set[int]) (cmdStr string) {
	for ; fromLine < len(lines); fromLine++ {
		line := goexec.TrimGonbCommentPrefix(lines[fromLine])
		cmdStr += line
		usedLines.Insert(fromLine)
		if cmdStr[len(cmdStr)-1] != '\\' {
			return
		}
		cmdStr = cmdStr[:len(cmdStr)-1] + " "
	}
	return
}

// execSpecialConfig executes special configuration commands (that start with "%), except cell commands.
// See [HelpMessage] for details, and [execCellSpecialCmd].
//
// It only returns errors for system errors that will lead to the kernel restart. Syntax errors
// on the command themselves are simply reported back to jupyter and are not returned here.
//
// It supports msg == nil for testing.
func execSpecialConfig(msg kernel.Message, goExec *goexec.State, cmdStr string, status *cellStatus) error {
	_ = goExec
	var content map[string]any
	if msg != nil && msg.ComposedMsg().Content != nil {
		content = msg.ComposedMsg().Content.(map[string]any)
	}
	parts := splitCmd(cmdStr)
	switch parts[0] {

	// Configures how cell will be executed.
	case "%", "main", "args", "test", "exec":
		// Set arguments for execution, allows one to set flags, etc.
		if parts[0] == "exec" {
			if len(parts) == 1 {
				return errors.Errorf("%%exec requires the name of the function to execute")
			}
			// The function to execute is in parts[1], but it will be extracted later when parsing the cell code.
			goExec.Args = parts[2:]
		} else {
			goExec.Args = parts[1:]
		}
		klog.V(2).Infof("Program args to use (%%%s): %+q", parts[0], goExec.Args)
		if parts[0] == "test" {
			goExec.CellIsTest = true
		}
		// %% and %main are also handled specially by goexec, where it starts a main() clause.
	case "wasm":
		if len(parts) > 1 {
			return errors.Errorf("`%%wasm` takes no extra parameters.")
		}
		goExec.CellIsWasm = true
		var err error
		err = goExec.MakeWasmSubdir()
		if err != nil {
			return errors.WithMessagef(err, "failed to prepare `%%wasm`")
		}
		goExec.WasmDivId = UniqueId() // Unique ID for this cell.

	case "widgets":
		return goExec.Comms.InstallWebSocket(msg)

	case "widgets_hb":
		var hb bool
		hb, err := goExec.Comms.SendHeartbeatAndWait(msg, 1*time.Second)
		if err != nil {
			return err
		}
		if hb {
			return kernel.PublishHtml(msg, "Heartbeat pong received back.")
		} else {
			return kernel.PublishHtml(msg, "Timed-out, no heartbeat pong received. Try installing front-end websockets with %widgets ?")
		}

	case "env":
		// Set environment variables.
		if len(parts) == 2 {
			// Adjust parts if one uses `%env KEY=VALUE` format instead.
			if eqPos := strings.Index(parts[1], "="); eqPos > 1 {
				key := parts[1][:eqPos]
				value := parts[1][eqPos+1:]
				parts = []string{parts[0], key, value}
			}
		}
		if len(parts) != 3 {
			return errors.Errorf("`%%env <VAR_NAME> <value>` (or `%%env <VAR_NAME>=<value>`): it takes 2 arguments, the variable name and it's content, but %d were given", len(parts)-1)
		}
		err := os.Setenv(parts[1], parts[2])
		if err != nil {
			return errors.Wrapf(err, "`%%env %q %q` failed", parts[1], parts[2])
		}
		err = kernel.PublishWriteStream(msg, kernel.StreamStdout,
			fmt.Sprintf("Set: %s=%q\n", parts[1], parts[2]))
		if err != nil {
			klog.Errorf("Failed to output: %+v", err)
		}

	case "cd":
		if len(parts) == 1 {
			pwd, _ := os.Getwd()
			_ = kernel.PublishWriteStream(msg, kernel.StreamStdout,
				fmt.Sprintf("Current directory: %q\n", pwd))
		} else if len(parts) > 2 {
			return errors.Errorf("`%%cd [<directory>]`: it takes none or one argument, but %d were given", len(parts)-1)
		} else {
			err := os.Chdir(ReplaceTildeInDir(parts[1]))
			if err != nil {
				return errors.Wrapf(err, "`%%cd %q` failed", parts[1])
			}
			pwd, _ := os.Getwd()
			err = kernel.PublishWriteStream(msg, kernel.StreamStdout,
				fmt.Sprintf("Changed directory to %q\n", pwd))
			if err != nil {
				klog.Errorf("Failed to output: %+v", err)
			}
			err = os.Setenv(protocol.GONB_DIR_ENV, pwd)
			if err != nil {
				klog.Errorf("Failed to set environment variable %q: %+v", protocol.GONB_DIR_ENV, err)
			}
		}

		// Flags for `go build`:
	case "goflags":
		if len(parts) > 1 {
			nonEmptyArgs := slices.DeleteFunc(parts[1:], func(s string) bool { return s == "" })
			goExec.GoBuildFlags = nonEmptyArgs
		}
		err := kernel.PublishWriteStream(msg, kernel.StreamStdout,
			fmt.Sprintf("%%goflags=%q\n", goExec.GoBuildFlags))
		if err != nil {
			klog.Errorf("Failed publishing contents: %+v", err)
		}

	// Automatic `go get` control:
	case "autoget":
		goExec.AutoGet = true
	case "noautoget":
		goExec.AutoGet = false
	case "help":
		//_ = kernel.PublishWriteStream(msg, kernel.StreamStdout, HelpMessage)
		err := kernel.PublishMarkdown(msg, HelpMessage)
		if err != nil {
			klog.Errorf("Failed publishing help contents: %+v", err)
		}
	case "version":
		err := kernel.PublishMarkdown(msg, version.AppVersion.PrintMarkdown())
		if err != nil {
			klog.Errorf("Failed publishing version contents: %+v", err)
		}

	// Definitions management.
	case "reset":
		if len(parts) == 1 {
			resetDefinitions(msg, goExec)
		} else {
			if len(parts) > 2 || parts[1] != "go.mod" {
				return errors.Errorf("%%reset only take one optional parameter \"go.mod\"")
			}
		}
		return goExec.GoModInit()
	case "ls", "list":
		listDefinitions(msg, goExec)
	case "rm", "remove":
		removeDefinitions(msg, goExec, parts[1:])

	// Input handling.
	case "with_inputs":
		allowInput := content["allow_stdin"].(bool)
		if !allowInput && (status.withInputs || status.withPassword) {
			return errors.Errorf("%%with_inputs not available in this notebook, it doesn't allow input prompting")
		}
		status.withInputs = true
	case "with_password":
		allowInput := content["allow_stdin"].(bool)
		if !allowInput && (status.withInputs || status.withPassword) {
			return errors.Errorf("%%with_password not available in this notebook, it doesn't allow input prompting")
		}
		status.withPassword = true

	// Files that need tracking for `gopls` (for auto-complete and contextual help).
	case "track":
		execTrack(msg, goExec, parts[1:])
	case "untrack":
		if len(parts) != 2 {
			return errors.New("%untrack takes one argument, the name Go file to tack")
		}
		execUntrack(msg, goExec, parts[1:])

	// Fix issues with `go work`.
	case "goworkfix":
		return goExec.GoWorkFix(msg)

	// Capture output of cell.
	case "capture":
		args := parts[1:]
		var appendToFile bool
		if len(args) > 1 && args[0] == "-a" {
			appendToFile = true
			args = args[1:]
		}
		if len(args) != 1 {
			return errors.New("%capture takes one argument, the name of the file where to save the captured output")
		}
		filePath := args[0]
		filePath = ReplaceTildeInDir(filePath)
		filePath = ReplaceEnvVars(filePath)
		var err error
		var f *os.File
		if appendToFile {
			f, err = os.OpenFile(filePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
			if err != nil {
				err = errors.Wrapf(err, "failed to append to \"%%capture\" file %q", filePath)
				klog.Errorf("Error: %+v", err)
				return err
			}
		} else {
			f, err = os.Create(filePath)
			if err != nil {
				err = errors.Wrapf(err, "failed to create \"%%capture\" file %q", filePath)
				klog.Errorf("Error: %+v", err)
				return err
			}
		}
		// Notice, file will be closed in goExec.PostExecuteCell(), where all "one-shot" state is cleaned up.
		goExec.CaptureFile = f

	default:
		if CellSpecialCommands.Has("%" + parts[0]) {
			// Cell special commands should always come first, and if they are parsed here (as opposed to being processed by specialCells)
			// they were in the middle somewhere.
			return errors.Errorf("\"%%%s\" can only appear at the start of the cell", parts[0])
		}

		// Unknown special command.
		err := kernel.PublishWriteStream(msg, kernel.StreamStderr, fmt.Sprintf("\"%%%s\" unknown or not implemented yet.", parts[0]))
		if err != nil {
			klog.Errorf("Error while reporting back on unimplemented message command \"%%%s\" kernel: %+v", parts[0], err)
		}
	}
	return nil
}

// execShell executes `cmdStr` properly redirecting outputs to display in the notebook.
//
// It only returns errors for system errors that will lead to the kernel restart. Syntax errors
// on the command themselves are simply reported back to jupyter and are not returned here.
func execShell(msg kernel.Message, goExec *goexec.State, cmdStr string, status *cellStatus) error {
	var execDir string // Default "", means current directory.
	if cmdStr[0] == '*' {
		cmdStr = cmdStr[1:]
		execDir = goExec.TempDir
	}
	if status.withInputs {
		status.withInputs = false
		status.withPassword = false
		return jpyexec.New(msg, "/bin/bash", "-c", cmdStr).
			ExecutionCount(msg.Kernel().ExecCounter).
			InDir(execDir).WithInputs(MillisecondsWaitForInput).Exec()
	} else if status.withPassword {
		status.withInputs = false
		status.withPassword = false
		return jpyexec.New(msg, "/bin/bash", "-c", cmdStr).
			ExecutionCount(msg.Kernel().ExecCounter).
			InDir(execDir).WithPassword(MillisecondsWaitForInput).Exec()
	} else {
		return jpyexec.New(msg, "/bin/bash", "-c", cmdStr).
			ExecutionCount(msg.Kernel().ExecCounter).
			InDir(execDir).Exec()
	}
}

// splitCmd split the special command into it's parts separated by space(s). It also
// accepts quotes to allow spaces to be included in a part. E.g.: `%args --text "hello world"`
// should be split into ["%args", "--text", "hello world"].
func splitCmd(cmd string) (parts []string) {
	partStarted := false
	inQuotes := false
	part := ""
	for pos := 0; pos < len(cmd); pos++ {
		c := cmd[pos]

		isSpace := c == ' ' || c == '\t' || c == '\n'
		if !inQuotes && isSpace {
			if partStarted {
				parts = append(parts, part)
			}
			part = ""
			partStarted = false
			continue
		}

		isQuote := c == '"'
		if isQuote {
			if inQuotes {
				inQuotes = false
			} else {
				inQuotes = true
				partStarted = true // Allows for empty argument.
			}
			continue
		}

		isEscape := c == '\\'
		// Outside of quotes "\" is only a normal character.
		if isEscape && inQuotes {
			if pos == len(cmd)-1 {
				// Odd last character ... but we don't do anything then.
				break
			}
			pos++
			c = cmd[pos]
			switch c {
			case 'n':
				c = '\n'
			case 't':
				c = '\t'
			default:
				// No effect. But it allows backslash+quote to render a quote within quotes.
			}
		}

		part = fmt.Sprintf("%s%c", part, c)
		partStarted = true
	}
	if partStarted {
		parts = append(parts, part)
	}
	return
}
