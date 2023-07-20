package goexec

import (
	"bytes"
	"fmt"
	. "github.com/janpfeifer/gonb/common"
	"github.com/janpfeifer/gonb/kernel"
	"github.com/pkg/errors"
	"io"
	"k8s.io/klog/v2"
	"os/exec"
	"path"
	"regexp"
	"strconv"
	"strings"
)

// ExecuteCell takes the contents of a cell, parses it, merges new declarations with the ones
// from previous definitions, render a final main.go code with the whole content,
// compiles and runs it.
//
// skipLines are lines that should not be considered as Go code. Typically, these are the special
// commands (like `%%`, `%args`, `%reset`, or bash lines starting with `!`).
func (s *State) ExecuteCell(msg kernel.Message, cellId int, lines []string, skipLines Set[int]) error {
	// Runs AutoTrack: makes sure redirects in go.mod and use clauses in go.work are tracked.
	err := s.AutoTrack()
	if err != nil {
		return err
	}

	updatedDecls, mainDecl, _, fileToCellIdAndLine, err := s.parseLinesAndComposeMain(msg, cellId, lines, skipLines, NoCursor)
	if err != nil {
		return errors.WithMessagef(err, "in goexec.ExecuteCell()")
	}

	// Exec `goimports` (or the code that implements it)
	_, fileToCellIdAndLine, err = s.GoImports(msg, updatedDecls, mainDecl, fileToCellIdAndLine)

	if err != nil {
		return errors.WithMessagef(err, "goimports failed")
	}

	// And then compile it.
	if err := s.Compile(msg, fileToCellIdAndLine); err != nil {
		return err
	}

	// Compilation successful: save merged declarations into current State.
	s.Definitions = updatedDecls

	// Execute compiled code.
	return s.Execute(msg, fileToCellIdAndLine)
}

// BinaryPath is the path to the generated binary file.
func (s *State) BinaryPath() string {
	return path.Join(s.TempDir, s.Package)
}

// MainPath is the path to the main.go file.
func (s *State) MainPath() string {
	return path.Join(s.TempDir, "main.go")
}

// AlternativeDefinitionsPath is the path to a temporary file that holds the memorize definitions,
// when we are not able to include them in the `main.go`, because the current cell is not parseable.
func (s *State) AlternativeDefinitionsPath() string {
	return path.Join(s.TempDir, "other.go")
}

func (s *State) Execute(msg kernel.Message, fileToCellIdAndLine []CellIdAndLine) error {
	return kernel.PipeExecToJupyter(msg, s.BinaryPath(), s.Args...).
		WithStderr(newJupyterStackTraceMapperWriter(msg, "stderr", s.MainPath(), fileToCellIdAndLine)).
		Exec()
}

// Compile compiles the currently generate go files in State.TempDir to a binary named State.Package.
//
// If errors in compilation happen, linesPos is used to adjust line numbers to their content in the
// current cell.
func (s *State) Compile(msg kernel.Message, fileToCellIdAndLines []CellIdAndLine) error {
	cmd := exec.Command("go", "build", "-o", s.BinaryPath())
	cmd.Dir = s.TempDir
	var output []byte
	output, err := cmd.CombinedOutput()
	if err != nil {
		err := s.DisplayErrorWithContext(msg, fileToCellIdAndLines, string(output))
		return errors.Wrapf(err, "failed to run %q", cmd.String())
	}
	return nil
}

// GoImports execute `goimports` which adds imports to non-declared imports automatically.
// It also runs "go get" to download any missing dependencies.
//
// It returns the updated cursorInFile and fileToCellIdAndLines that reflect any changes in `main.go`.
func (s *State) GoImports(msg kernel.Message, decls *Declarations, mainDecl *Function, fileToCellIdAndLine []CellIdAndLine) (cursorInFile Cursor, updatedFileToCellIdAndLine []CellIdAndLine, err error) {
	klog.V(2).Infof("GoImports():")
	cursorInFile = NoCursor
	goimportsPath, err := exec.LookPath("goimports")
	if err != nil {
		_ = kernel.PublishWriteStream(msg, kernel.StreamStderr, `
Program goimports is not installed. It is used to automatically import
missing standard packages, and is a standard Go toolkit package. You
can install it from the notebook with:

!go install golang.org/x/tools/cmd/goimports@latest

`)
		err = errors.WithMessagef(err, "while trying to run goimports\n")
		return
	}
	cmd := exec.Command(goimportsPath, "-w", s.MainPath())
	cmd.Dir = s.TempDir
	var output []byte
	output, err = cmd.CombinedOutput()
	if err != nil {
		err = s.DisplayErrorWithContext(msg, fileToCellIdAndLine, string(output)+"\n"+err.Error())
		err = errors.Wrapf(err, "failed to run %q", cmd.String())
		return
	}

	// Parse declarations in created `main.go` file.
	var newDecls *Declarations
	newDecls, err = s.parseFromMainGo(msg, -1, NoCursor, nil)
	if err != nil {
		return
	}

	// Find only imports that `goimports` found were used.
	usedImports := MakeSet[string]()
	for key := range newDecls.Imports {
		usedImports.Insert(key)
	}

	// Import original declarations -- they have the correct cell line numbers.
	newDecls.MergeFrom(decls)

	// Remove unused imports, to avoid the "imported and not used" error.
	keys := SortedKeys(newDecls.Imports)
	for _, key := range keys {
		if !usedImports.Has(key) {
			delete(newDecls.Imports, key)
		}
	}

	delete(newDecls.Functions, "main")
	cursorInFile, updatedFileToCellIdAndLine, err = s.createMainFileFromDecls(newDecls, mainDecl)
	if err != nil {
		err = errors.WithMessagef(err, "while composing main.go with all declarations")
		return
	}
	klog.V(2).Infof("GoImports(): cursorInFile=%s", cursorInFile)

	// Download missing dependencies.
	if !s.AutoGet {
		return
	}
	cmd = exec.Command("go", "get")
	cmd.Dir = s.TempDir
	output, err = cmd.CombinedOutput()
	if err != nil {
		err = errors.Wrapf(err, "failed to run %q", cmd.String())
		strOutput := fmt.Sprintf("%v\n\n%s", err, output)
		strOutput = s.filterGoGetError(strOutput)
		err = s.DisplayErrorWithContext(msg, fileToCellIdAndLine, strOutput)
		return
	}
	return
}

// jupyterStackTraceMapperWriter implements an io.Writer that maps stack traces to their corresponding
// cell lines, to facilitate debugging.
type jupyterStackTraceMapperWriter struct {
	jupyterWriter       io.Writer
	mainPath            string
	fileToCellIdAndLine []CellIdAndLine
	regexpMainPath      *regexp.Regexp
}

// newJupyterStackTraceMapperWriter creates an io.Writer that allows for mapping of references to the `main.go`
// to its corresponding position in a cell.
func newJupyterStackTraceMapperWriter(msg kernel.Message, stream string, mainPath string, fileToCellIdAndLine []CellIdAndLine) io.Writer {
	r, err := regexp.Compile(fmt.Sprintf("%s:(\\d+)", regexp.QuoteMeta(mainPath)))
	if err != nil {
		klog.Errorf("Failed to compile expression to match %q: won't be able to map stack traces with cell lines", mainPath)
	}

	return &jupyterStackTraceMapperWriter{
		jupyterWriter:       kernel.NewJupyterStreamWriter(msg, stream),
		mainPath:            mainPath,
		regexpMainPath:      r,
		fileToCellIdAndLine: fileToCellIdAndLine,
	}
}

// Write implements io.Writer, and maps references to the `main.go` file to their corresponding lines in cells.
func (w *jupyterStackTraceMapperWriter) Write(p []byte) (int, error) {
	n := len(p) // Save original number of bytes.
	if w.regexpMainPath == nil {
		return w.jupyterWriter.Write(p)
	}
	p = w.regexpMainPath.ReplaceAllFunc(p, func(match []byte) []byte {
		klog.V(2).Infof("\tFiltering stderr: %s", match)
		lineNumStr := strings.Split(string(match), ":")[1]
		lineNum, err := strconv.Atoi(lineNumStr)
		if err != nil {
			klog.Warningf("Can't parse line number in error output %q, skipping", match)
			return match
		}
		lineNum -= 1 // Since line reporting starts with 1, but our indices start with 0.
		if lineNum < 0 || lineNum >= len(w.fileToCellIdAndLine) {
			klog.Warningf("Can't find line number %d in %q: skipping", lineNum, w.mainPath)
			return match
		}
		cellId, cellLineNum := w.fileToCellIdAndLine[lineNum].Id, w.fileToCellIdAndLine[lineNum].Line
		var cellText []byte
		const invertColor = "\033[7m"
		const resetColor = "\033[0m"
		// Since line reports usually start with 1, we report cellLineNum+1
		if cellId == -1 {
			cellText = []byte(fmt.Sprintf(" %s[[ Cell Line %d ]]%s ", invertColor, cellLineNum+1, resetColor))
		} else {
			cellText = []byte(fmt.Sprintf(" %s[[ Cell [%d] Line %d ]]%s ", invertColor, cellId, cellLineNum+1, resetColor))
		}
		res := bytes.Join([][]byte{cellText, match}, nil)
		return res
	})
	_, err := w.jupyterWriter.Write(p)
	if err != nil {
		return 0, err
	}
	// Return the original number of bytes: since we change what is written, we actually write more bytes.
	return n, nil
}

const (
	// GoGetWorkspaceIssue is an error output by `go get` due to it not interpreting correctly `go.work`.
	GoGetWorkspaceIssue = "cannot find module providing package"

	// GoGetWorkspaceNote is the note that explains the issue with `go get` and `go work`.
	GoGetWorkspaceNote = `---------------
Note: 'go get' doesn't know how to process go.work files. 
Consider adding the paths in 'use' clauses to 'go.mod' replace clauses.
You can do this manually with '!*go mod edit --replace=[module URI]=[local_path]'
Alternatively try '%goworkfix' that will do it automatically for you.
`
)

// filterGoGetError parses the "go get" execution error, and adds a warning in case it's about the
// `go get` not supporting workspaces (`go.work`).
func (s *State) filterGoGetError(output string) string {
	if !s.hasGoWork {
		// Nothing to do.
		return output
	}
	if strings.Index(output, "cannot find module providing package") == -1 {
		return output
	}

	modToPath, err := s.findGoWorkModules()
	if err != nil {
		return fmt.Sprintf("%s\n\nError while tracking potential issues with `go.work`:\n%+v", output, err.Error())
	}
	var parts []string
	for mod, p := range modToPath {
		if strings.Index(output, mod) != -1 {
			parts = append(parts, fmt.Sprintf("%s=%s", mod, p))
		}
	}

	var extraMsg string
	if len(parts) > 0 {
		extraMsg = fmt.Sprintf("\nConsider the following replace rules to your 'go.mod' file:\n\t%s\n\n"+
			"Again, or use '%%goworkfix' to have it done for you.\n", strings.Join(parts, "\n\t"))
	}
	output = fmt.Sprintf("%s\n%s%s", output, GoGetWorkspaceNote, extraMsg)
	return output
}
