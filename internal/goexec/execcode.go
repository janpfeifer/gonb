package goexec

import (
	"bytes"
	"fmt"
	. "github.com/janpfeifer/gonb/common"
	"github.com/janpfeifer/gonb/internal/jpyexec"
	"github.com/janpfeifer/gonb/internal/kernel"
	"github.com/pkg/errors"
	"golang.org/x/exp/slices"
	"io"
	"k8s.io/klog/v2"
	"os"
	"os/exec"
	"path"
	"regexp"
	"strconv"
	"strings"
)

// cellExecParams are the parameters of ExecuteCell, packaged so they
// can be serialized in the channel `state.cellExecChan`.
type cellExecParams struct {
	msg       kernel.Message
	cellId    int
	lines     []string
	skipLines Set[int]
	done      *LatchWithValue[error]
}

// ExecuteCell takes the contents of a cell, parses it, merges new declarations with the ones
// from previous definitions, render a final main.go code with the whole content,
// compiles and runs it.
//
// skipLines are Lines that should not be considered as Go code. Typically, these are the special
// commands (like `%%`, `%args`, `%reset`, or bash Lines starting with `!`).
func (s *State) ExecuteCell(msg kernel.Message, cellId int, lines []string, skipLines Set[int]) error {
	params := &cellExecParams{
		msg:       msg,
		cellId:    cellId,
		lines:     lines,
		skipLines: skipLines,
		done:      NewLatchWithValue[error](),
	}
	s.cellExecChan <- params
	return params.done.Wait()
}

// serializeExecuteCell loops indefinitely waiting for cells to be executed.
// It exits only when the kernel stops.
func (s *State) serializeExecuteCell() {
	var stopC <-chan struct{}
	if s.Kernel != nil {
		stopC = s.Kernel.StoppedChan()
	} else {
		stopC = make(chan struct{})
	}
	for {
		select {
		case params := <-s.cellExecChan:
			// Received new execution request.
			params.done.Trigger(s.executeCellImpl(
				params.msg, params.cellId, params.lines, params.skipLines))

		case <-stopC:
			// Kernel stopped, exit.
			return
		}
	}
}

// executeCellImpl executes the cell and returns the error.
// See documentation of parameters in `State.ExecuteCell`.
// It is not reentrant, and calls to it should be serialized.
// ExecuteCell serializes the calls to this method.
func (s *State) executeCellImpl(msg kernel.Message, cellId int, lines []string, skipLines Set[int]) error {
	klog.V(1).Infof("ExecuteCell: %q", lines)

	defer s.PostExecuteCell()
	klog.V(2).Infof("ExecuteCell(): CellIsTest=%v, CellIsWasm=%v", s.CellIsTest, s.CellIsWasm)
	if s.CellIsTest && s.CellIsWasm {
		return errors.Errorf("Cannot execute test in a %%wasm cell. Please, choose either `%%wasm` or `%%test`.")
	}

	// Runs AutoTrack: makes sure redirects in go.mod and use clauses in go.work are tracked.
	err := s.AutoTrack()
	if err != nil {
		return err
	}

	klog.V(2).Infof("ExecuteCell: after AutoTrack")

	updatedDecls, mainDecl, _, fileToCellIdAndLine, err := s.parseLinesAndComposeMain(msg, cellId, lines, skipLines, NoCursor)
	if err != nil {
		klog.Infof("goexec.ExecuteCell() failed to parse the cell: %+v", err)
		return err
	}
	klog.V(2).Infof("ExecuteCell: after s.parseLinesAndComposeMain()")

	// ProgramExecutor `goimports` (or the code that implements it) -- it updates `updatedDecls` with
	// the new imports, if there are any.
	_, fileToCellIdAndLine, err = s.GoImports(msg, updatedDecls, mainDecl, fileToCellIdAndLine)

	klog.V(2).Infof("ExecuteCell: after s.GoImports()")

	if err != nil {
		klog.Infof("goexec.ExecuteCell() failed to run `go imports` and `go get`: %+v", err)
		return err
	}

	// And then compile it.
	if err := s.Compile(msg, fileToCellIdAndLine); err != nil {
		klog.Infof("goexec.ExecuteCell() failed to compile cell: %+v", err)
		return err
	}

	klog.V(2).Infof("ExecuteCell: after s.Compile()")

	// Compilation successful: save merged declarations into current State.
	s.Definitions = updatedDecls

	// Execute compiled code.
	return s.Execute(msg, fileToCellIdAndLine)
}

// PostExecuteCell reset state that is valid only for the duration of a cell.
// This includes s.CellIsTest and s.Args.
func (s *State) PostExecuteCell() {
	klog.V(2).Infof("PostExecuteCell(): CellIsTest=%v", s.CellIsTest)
	if s.CellIsWasm {
		// Remove declarations exported for running in WASM.
		s.RemoveWasmConstants(s.Definitions)
	}

	s.Args = nil
	s.CellIsTest = false
	s.CellTests = nil
	s.CellHasBenchmarks = false
	s.CellIsWasm = false
	s.WasmDivId = ""
}

// BinaryPath is the path to the generated binary file.
func (s *State) BinaryPath() string {
	return path.Join(s.TempDir, s.Package)
}

const (
	MainGo     = "main.go"
	MainTestGo = "main_test.go"
)

// CodePath is the path to where the code is going to be saved. Either `main.go` or `main_test.go` file.
func (s *State) CodePath() string {
	name := MainGo
	if s.CellIsTest {
		name = MainTestGo
	}
	return path.Join(s.TempDir, name)
}

// RemoveCode removes the code files (`main.go` or `main_test.go`).
// Usually used just before creating creating a new version.
func (s *State) RemoveCode() error {
	for _, name := range [2]string{MainGo, MainTestGo} {
		p := path.Join(s.TempDir, name)
		err := os.Remove(p)
		if err != nil && !os.IsNotExist(err) {
			return errors.Wrapf(err, "can't remove previously generated code in %q", p)
		}
	}
	return nil
}

// AlternativeDefinitionsPath is the path to a temporary file that holds the memorize definitions,
// when we are not able to include them in the `main.go`, because the current cell is not parseable.
func (s *State) AlternativeDefinitionsPath() string {
	return path.Join(s.TempDir, "other.go")
}

func (s *State) Execute(msg kernel.Message, fileToCellIdAndLine []CellIdAndLine) error {
	if s.CellIsWasm {
		return s.ExecuteWasm(msg)
	}
	args := s.Args
	if len(args) == 0 && s.CellIsTest {
		args = s.DefaultCellTestArgs()
	}
	err := jpyexec.New(msg, s.BinaryPath(), args...).
		UseNamedPipes(s.Comms).
		ExecutionCount(msg.Kernel().ExecCounter).
		WithStderr(newJupyterStackTraceMapperWriter(msg, "stderr", s.CodePath(), fileToCellIdAndLine)).
		Exec()
	if err != nil {
		klog.Infof("goexec.Execute(): failed to run the compiled cell: %+v", msg)
	}
	return err
}

// Compile compiles the currently generate go files in State.TempDir to a binary named State.Package.
//
// If errors in compilation happen, linesPos is used to adjust line numbers to their content in the
// current cell.
func (s *State) Compile(msg kernel.Message, fileToCellIdAndLines []CellIdAndLine) error {
	var args []string
	if s.CellIsTest {
		args = []string{"test", "-c", "-o", s.BinaryPath()}
	} else if s.CellIsWasm {
		args = []string{"build", "-o", path.Join(s.WasmDir, CompiledWasmName)}
	} else {
		args = []string{"build", "-o", s.BinaryPath()}
	}
	args = append(args, s.GoBuildFlags...)
	cmd := exec.Command("go", args...)
	cmd.Dir = s.TempDir
	if s.CellIsWasm {
		// Set GOARCH and GOOS in cmd.Env.
		cmd.Env = append(
			slices.DeleteFunc(cmd.Environ(), func(s string) bool {
				return strings.HasPrefix(s, "GOARCH=") ||
					strings.HasPrefix(s, "GOOS=")
			}),
			"GOARCH=wasm",
			"GOOS=js",
		)
	}

	var output []byte
	klog.V(2).Infof("Executing %s", cmd)
	output, err := cmd.CombinedOutput()
	if err != nil {
		klog.Errorf("Failed %q:\n%s\n", cmd, output)
		err := s.DisplayErrorWithContext(msg, fileToCellIdAndLines, string(output), err)
		return errors.Wrapf(err, "failed to run %q", cmd)
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
	cmd := exec.Command(goimportsPath, "-w", s.CodePath())
	cmd.Dir = s.TempDir
	var output []byte
	klog.V(2).Infof("Executing %s", cmd)
	output, err = cmd.CombinedOutput()
	if err != nil {
		err = s.DisplayErrorWithContext(msg, fileToCellIdAndLine, string(output)+"\n"+err.Error(), err)
		err = errors.Wrapf(err, "failed to run %q", cmd.String())
		return
	}

	// Parse declarations in created `main.go` file.
	var newDecls *Declarations
	newDecls, err = s.parseFromGoCode(msg, -1, NoCursor, nil)
	newDecls.DropFuncInit() // These may be generated, we don't want to memorize these.
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

	// Remove unused imports, to avoid the "imported and not used" err.
	keys := SortedKeys(newDecls.Imports)
	for _, key := range keys {
		if !usedImports.Has(key) {
			delete(newDecls.Imports, key)
		}
	}

	delete(newDecls.Functions, "main")
	cursorInFile, updatedFileToCellIdAndLine, err = s.createCodeFileFromDecls(newDecls, mainDecl)
	if err != nil {
		err = errors.WithMessagef(err, "while composing main.go with all declarations")
		return
	}
	klog.V(2).Infof("GoImports(): cursorInFile=%s", cursorInFile)

	// Download missing dependencies.
	if !s.AutoGet {
		return
	}

	args := []string{"get"}
	if s.CellIsTest {
		args = append(args, "-t")
	}
	cmd = exec.Command("go", args...)
	cmd.Dir = s.TempDir
	klog.V(2).Infof("Executing %s", cmd)
	output, err = cmd.CombinedOutput()
	if err != nil {
		err = errors.Wrapf(err, "failed to run %q", cmd.String())
		strOutput := fmt.Sprintf("%v\n\n%s", err, output)
		strOutput = s.filterGoGetError(strOutput)
		err = s.DisplayErrorWithContext(msg, fileToCellIdAndLine, strOutput, err)
		return
	}
	return
}

// jupyterStackTraceMapperWriter implements an io.Writer that maps stack traces to their corresponding
// cell Lines, to facilitate debugging.
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
		klog.Errorf("Failed to compile expression to match %q: won't be able to map stack traces with cell Lines", mainPath)
	}

	return &jupyterStackTraceMapperWriter{
		jupyterWriter:       kernel.NewJupyterStreamWriter(msg, stream),
		mainPath:            mainPath,
		regexpMainPath:      r,
		fileToCellIdAndLine: fileToCellIdAndLine,
	}
}

// Write implements io.Writer, and maps references to the `main.go` file to their corresponding Lines in cells.
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
			klog.Warningf("Can't parse line number in err output %q, skipping", match)
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
	// GoGetWorkspaceIssue is an err output by `go get` due to it not interpreting correctly `go.work`.
	GoGetWorkspaceIssue = "cannot find module providing package"

	// GoGetWorkspaceNote is the note that explains the issue with `go get` and `go work`.
	GoGetWorkspaceNote = `---------------
Note: 'go get' doesn't know how to process go.work files. 
Consider adding the paths in 'use' clauses to 'go.mod' replace clauses.
You can do this manually with '!*go mod edit --replace=[module URI]=[local_path]'
Alternatively try '%goworkfix' that will do it automatically for you.
`
)

// filterGoGetError parses the "go get" execution err, and adds a warning in case it's about the
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
