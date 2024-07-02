package specialcmd

import (
	. "github.com/janpfeifer/gonb/common"
	"github.com/janpfeifer/gonb/internal/jpyexec"

	"fmt"
	"github.com/janpfeifer/gonb/internal/goexec"
	"github.com/janpfeifer/gonb/internal/kernel"
	"github.com/pkg/errors"
	"k8s.io/klog/v2"
	"os"
	"strings"
)

var (
	CellSpecialCommands = SetWithValues(
		"%%writefile",
		"%%script",
		"%%bash",
		"%%sh")
)

// IsGoCell returns whether the cell is expected to be a Go cell, based on the first line.
//
// The first line may contain special commands that change the interpretation of the cell, e.g.: "%%script", "%%writefile".
func IsGoCell(firstLine string) bool {
	firstLine = goexec.TrimGonbCommentPrefix(firstLine)
	parts := strings.Split(firstLine, " ")
	return !CellSpecialCommands.Has(parts[0])
}

// ExecuteSpecialCell checks whether it is a special cell (see [CellSpecialCommands]), and if so it executes the special cell command.
//
// It returns if this was a special cell (if true it executes it), and potentially an execution error, if one happened.
func ExecuteSpecialCell(msg kernel.Message, goExec *goexec.State, lines []string) (isSpecialCell bool, err error) {
	if len(lines) == 0 {
		return
	}
	line := goexec.TrimGonbCommentPrefix(lines[0])
	parts := splitCmd(line)
	if len(parts) == 0 || !CellSpecialCommands.Has(parts[0]) {
		return
	}
	isSpecialCell = true
	klog.V(2).Infof("Executing special cell command %q", parts)

	switch parts[0] {
	case "%%writefile":
		args := parts[1:]
		err = cellCmdWritefile(msg, goExec, args, lines[1:])

	case "%%script", "%%bash", "%%sh":
		var args []string
		if parts[0] == "%%script" {
			args = parts[1:]
		} else {
			if len(parts) != 1 {
				err = errors.Errorf("%q expects no extra arguments, %v was given", parts[0], parts[1:])
				return
			}
			args = []string{parts[0][2:]} // Trim the prefix "%%".
		}
		err = cellCmdScript(msg, goExec, args, lines[1:])

	default:
		err = errors.Errorf("special cell command %q not implemented", parts[0])
	}
	return
}

// cellCmdWritefile implements `%%writefile`.
func cellCmdWritefile(msg kernel.Message, goExec *goexec.State, args []string, lines []string) error {
	var appendToFile bool
	if len(args) > 1 && args[0] == "-a" {
		appendToFile = true
		args = args[1:]
	}
	if len(args) != 1 {
		return errors.Errorf("expected \"%%%%writefile [-a] <file_name>\", but got %q instead", args)
	}
	filePath := args[0]
	filePath = ReplaceTildeInDir(filePath)
	filePath = ReplaceEnvVars(filePath)
	err := writeLinesToFile(filePath, lines, appendToFile)
	if err != nil {
		return err
	}
	if appendToFile {
		_ = kernel.PublishWriteStream(msg, kernel.StreamStderr, fmt.Sprintf("Cell contents appended to %q.\n", filePath))
	} else {
		_ = kernel.PublishWriteStream(msg, kernel.StreamStderr, fmt.Sprintf("Cell contents written to %q.\n", filePath))
	}
	return nil
}

// writeLinesToFile. If `append` is true open the file with append.
func writeLinesToFile(filePath string, lines []string, appendToFile bool) error {
	var f *os.File
	var err error
	if appendToFile {
		f, err = os.OpenFile(filePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	} else {
		f, err = os.Create(filePath)
	}
	if err != nil {
		return errors.Wrapf(err, "failed to open %q", filePath)
	}
	defer func() { _ = f.Close() }()
	for _, line := range lines {
		_, err = fmt.Fprintln(f, line)
		if err != nil {
			return errors.Wrapf(err, "failed writing to %q", filePath)
		}
	}
	return nil
}

// cellCmdScript implements `%%script`, '%%bash', '%%sh'.
func cellCmdScript(msg kernel.Message, goExec *goexec.State, args []string, lines []string) error {
	if klog.V(2).Enabled() {
		klog.Infof("Execute: %q", args)
		klog.Infof("Input: %q", strings.Join(lines, "\n"))
	}
	return jpyexec.New(msg, args[0], args[1:]...).
		ExecutionCount(msg.Kernel().ExecCounter).
		WithStaticInput([]byte(strings.Join(lines, "\n") + "\n")).
		Exec()
}
