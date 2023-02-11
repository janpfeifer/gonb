// Package specialcmd handles special commands, that come in two flavors:
//
// - `%<cmd> {...args...}` : Control the environment (variables) and configure gonb.
// - `!<shell commands>` : Execute shell commands. Similar to the ipython kernel.
//
// In particular `%help` will print out currently available commands.
package specialcmd

import (
	"fmt"
	"github.com/janpfeifer/gonb/goexec"
	"github.com/janpfeifer/gonb/kernel"
	"github.com/pkg/errors"
	"log"
)

const HelpMessage = `GoNB is a Go kernel that compiles and executed on-the-fly Go code. 

When executing a cell, *GoNB* will save the cell contents (except non-Go commands see
below) into a "main.go" file, compile and execute it.

It also saves any global declarations (imports, functions, types, variables, constants)
and reuse them at the next cell execution -- so you can define a function in one
cell, and reuse in the next one. Just the "func main()" is not reused.

A "hello world" example would look like:

	func main() {
		fmt.Printf("Hello world!\n");
	}

But to avoid having to type "func main()" all the time, you can use "%%" and everything
after is wrapped inside a "func main() { ... }". So our revised "hello world" looks like:

	%%
	fmt.Printf("Hello world!\n")


- "init()" functions: since there is always only one definition per function name, 
  it's not possible for each cell to have it's own init() function. Instead GoNB
  converts any function named "init_<my_stuff>()" to "init()" before compiling and
  executing. This way each cell can create its own "init_...()" and have it called
  at every cell execution.

Special non-Go commands: 

- "%main" or "%%": Marks the lines as follows to be wrapped in a "func main() {...}" during 
  execution. A shortcut to quickly execute code. It also automatically includes "flag.Parse()"
  as the very first statement.
- "%args": Sets arguments to be passed when executing the Go code. This allows one to
  use flags as a normal program.
- "%autoget" and "%noautoget": Default is "%autoget", which automatically does "go get" for
  packages not yet available.
- "%reset": clears all memorized declarations (imports, functions, variables, types and 
  constants).
- "%with_inputs": will prompt for inputs for the next shell command. Use this if
  the next shell command ("!") you execute reads the stdin. Jupyter will require
  you to enter one last value after the shell script executes.
- "%with_password": will prompt for a password passed to the next shell command.
  Do this is if your next shell command requires a password.

Executing shell commands:

- "!<shell_cmd>": executes the given command on a new shell. It makes it easy to run
  commands on the kernels box, for instance to install requirements, or quickly
  check contents of directories or files. Lines ending in "\" are continued on
  the next line -- so multi-line commands can be entered. But each command is
  executed in its own shell, that is, variables and state is not carried over.
- "!*<shell_cmd>": same as "!<shell_cmd>" except it first changes directory to
  the temporary directory used to compile the go code -- the latest execution
  is always saved in the file "main.go". It's also where the "go.mod" file for
  the notebook is created and maintained. Useful for manipulating "go.mod",
  for instance to get a package from some specific version, something 
  like "!*go get github.com/my/package@v3".
`

// cellStatus holds temporary status for the execution of the current cell.
type cellStatus struct {
	withInputs, withPassword bool
}

// Exec will check whether the given code to be executed has any special
// commands.
//
// Any special commands found in the code will be executed and the corresponding lines used
// from the code will be returned in usedLines -- so they can be excluded from other executors.
//
// If any errors happen, it is returned in err.
func Exec(msg kernel.Message, goExec *goexec.State, codeLines []string, usedLines map[int]bool) (err error) {
	status := &cellStatus{}
	for lineNum := 0; lineNum < len(codeLines); lineNum++ {
		if usedLines[lineNum] {
			continue
		}
		line := codeLines[lineNum]
		if len(line) > 1 && (line[0] == '%' || line[0] == '!') {
			var cmdStr string
			cmdStr = joinLine(codeLines, lineNum, usedLines)
			cmdType := cmdStr[0]
			cmdStr = cmdStr[1:]
			for cmdStr[0] == ' ' {
				cmdStr = cmdStr[1:] // Skip initial space
			}
			if len(cmdStr) == 0 {
				// Skip empty commands.
				continue
			}
			switch cmdType {
			case '%':
				err = execInternal(msg, goExec, cmdStr, status)
				if err != nil {
					return
				}
			case '!':
				err = execShell(msg, goExec, cmdStr, status)
				if err != nil {
					return
				}
			}
		}
	}
	return
}

// joinLine starts from fromLine and joins consecutive lines if the current line terminates with a `\n`,
// allowing multi-line commands to be issued.
//
// It returns the joined lines with the '\\\n' replaced by a space, and appends the used lines (including
// fromLine) to usedLines.
func joinLine(lines []string, fromLine int, usedLines map[int]bool) (cmdStr string) {
	for ; fromLine < len(lines); fromLine++ {
		cmdStr += lines[fromLine]
		usedLines[fromLine] = true
		if cmdStr[len(cmdStr)-1] != '\\' {
			return
		}
		cmdStr = cmdStr[:len(cmdStr)-1] + " "
	}
	return
}

// execInternal executes internal configuration commands, see HelpMessage for details.
//
// It only returns errors for system errors that will lead to the kernel restart. Syntax errors
// on the command themselves are simply reported back to jupyter and are not returned here.
func execInternal(msg kernel.Message, goExec *goexec.State, cmdStr string, status *cellStatus) error {
	_ = goExec
	content := msg.ComposedMsg().Content.(map[string]any)
	parts := splitCmd(cmdStr)
	switch parts[0] {
	case "%":
		// Handled by goexec, nothing to do here.
	case "args":
		// Set arguments for execution, allows one to set flags, etc.
		goExec.Args = parts[1:]
		log.Printf("args=%+q", parts)
	case "autoget":
		goExec.AutoGet = true
	case "noautoget":
		goExec.AutoGet = false
	case "help":
		_ = kernel.PublishWriteStream(msg, kernel.StreamStdout, HelpMessage)
	case "main":
		// Handled by goexec, nothing to do here.
	case "reset":
		goExec.Reset()
		err := kernel.PublishWriteStream(msg, kernel.StreamStdout, "* State reset: all memorized declarations discarded.\n")
		if err != nil {
			log.Printf("Error while reseting kernel: %+v", err)
		}
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
	default:
		err := kernel.PublishWriteStream(msg, kernel.StreamStderr, fmt.Sprintf("\"%%%s\" unknown or not implemented yet.", parts[0]))
		if err != nil {
			log.Printf("Error while reporting back on unimplmented message command \"%%%s\" kernel: %+v", parts[0], err)
		}
	}
	return nil
}

// execInternal executes internal configuration commands, see HelpMessage for details.
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
		return kernel.PipeExecToJupyterWithInput(msg, execDir, "/bin/bash", "-c", cmdStr)
	} else if status.withPassword {
		status.withInputs = false
		status.withPassword = false
		return kernel.PipeExecToJupyterWithPassword(msg, execDir, "/bin/bash", "-c", cmdStr)
	} else {
		return kernel.PipeExecToJupyter(msg, execDir, "/bin/bash", "-c", cmdStr)
	}
}

// splitCmd split the special command into it's parts separated by space(s). It also
// accepts quotes to allow spaces to be included in a part. Eg.: `%args --text "hello world"`
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
