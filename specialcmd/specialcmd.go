// Package specialcmd handles special commands, that come in two flavors:
//
//   - `%<cmd> {...args...}`: Control the environment (variables) and configure gonb.
//   - `!<shell commands>`: Execute shell commands.
//     Similar to the ipython kernel.
//
// In particular `%help` will print out currently available commands.
package specialcmd

import (
	"fmt"
	. "github.com/janpfeifer/gonb/common"
	"github.com/janpfeifer/gonb/goexec"
	"github.com/janpfeifer/gonb/kernel"
	"github.com/pkg/errors"
	"k8s.io/klog/v2"
	"log"
	"os"
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

- "%%" or "%main": Marks the lines as follows to be wrapped in a "func main() {...}" during 
  execution. A shortcut to quickly execute code. It also automatically includes "flag.Parse()"
  as the very first statement. Anything "%%" or "%main" are taken as arguments
  to be passed to the program -- it resets previous values given by "%args".
- "%args": Sets arguments to be passed when executing the Go code. This allows one to
  use flags as a normal program. Notice that if a value after "%%" or "%main" is given, it will
  overwrite the values here.
- "%autoget" and "%noautoget": Default is "%autoget", which automatically does "go get" for
  packages not yet available.
- "%cd [<directory>]": Change current directory of the Go kernel, and the directory from where
  the cells are executed. If no directory is given it reports the current directory.
- "%env VAR value": Sets the environment variable VAR to the given value. These variables
  will be available both for Go code as well as for shell scripts.
- "%with_inputs": will prompt for inputs for the next shell command. Use this if
  the next shell command ("!") you execute reads the stdin. Jupyter will require
  you to enter one last value after the shell script executes.
- "%with_password": will prompt for a password passed to the next shell command.
  Do this is if your next shell command requires a password.

Managing memorized definitions;

- "%list" (or "%ls"): Lists all memorized definitions (imports, constants, types, variables and
  functions) that are carried from one cell to another.
- "%remove <definitions>" (or "%rm <definitions>"): Removes (forgets) given definition(s). Use as key the
  value(s) listed with "%ls".
- "%reset" clears memory of memorized definitions.

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

Tracking of Go files being developed:

- "%track [file_or_directory]": add file or directory to list of tracked files,
  which are monitored by GoNB (and 'gopls') for auto-complete or contextual help.
  If no file is given, it lists the currently tracked files.
- "%untrack [file_or_directory][...]": remove file or directory from list of tracked files.
  If suffixed with "..." it will remove all files prefixed with the string given (without the
  "..."). If no file is given, it lists the currently tracked files. 
`

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
	for lineNum := 0; lineNum < len(codeLines); lineNum++ {
		if _, found := usedLines[lineNum]; found {
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
			if execute {
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

// joinLine starts from fromLine and joins consecutive lines if the current line terminates with a `\n`,
// allowing multi-line commands to be issued.
//
// It returns the joined lines with the '\\\n' replaced by a space, and appends the used lines (including
// fromLine) to usedLines.
func joinLine(lines []string, fromLine int, usedLines Set[int]) (cmdStr string) {
	for ; fromLine < len(lines); fromLine++ {
		cmdStr += lines[fromLine]
		usedLines[fromLine] = struct{}{}
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
	case "%", "main", "args":
		// Set arguments for execution, allows one to set flags, etc.
		goExec.Args = parts[1:]
		log.Printf("args=%+q", parts)
		// %% and %main are also handled specially by goexec, where it starts a main() clause.

	case "env":
		// Set environment variables.
		if len(parts) != 3 {
			return errors.Errorf("`%%env <VAR_NAME> <value>`: it takes 2 arguments, the variable name and it's content, but %d were given", len(parts)-1)
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
		}

	case "autoget":
		goExec.AutoGet = true
	case "noautoget":
		goExec.AutoGet = false
	case "help":
		_ = kernel.PublishWriteStream(msg, kernel.StreamStdout, HelpMessage)

		// Definitions management.
	case "reset":
		resetDefinitions(msg, goExec)
	case "ls", "list":
		listDefinitions(msg, goExec)
	case "rm", "remove":
		removeDefinitions(msg, goExec, parts[1:])

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
	case "track":
		execTrack(msg, goExec, parts[1:])
	case "untrack":
		execUntrack(msg, goExec, parts[1:])
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
		return kernel.PipeExecToJupyter(msg, "/bin/bash", "-c", cmdStr).InDir(execDir).WithInput(500).Exec()
	} else if status.withPassword {
		status.withInputs = false
		status.withPassword = false
		return kernel.PipeExecToJupyter(msg, "/bin/bash", "-c", cmdStr).InDir(execDir).WithPassword(1).Exec()
	} else {
		return kernel.PipeExecToJupyter(msg, "/bin/bash", "-c", cmdStr).InDir(execDir).Exec()
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
