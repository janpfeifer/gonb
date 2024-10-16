// Package goexec executes cells with Go code for the gonb kernel.
//
// It defines a State object, that carries all the globals defined so far. It provides
// the ExecuteCell method, to run a new cell.
package goexec

import (
	"fmt"
	"github.com/janpfeifer/gonb/common"
	"github.com/janpfeifer/gonb/gonbui/protocol"
	"github.com/janpfeifer/gonb/internal/comms"
	"github.com/janpfeifer/gonb/internal/goexec/goplsclient"
	"github.com/janpfeifer/gonb/internal/kernel"
	"github.com/pkg/errors"
	"io"
	"k8s.io/klog/v2"
	"os"
	"os/exec"
	"path"
	"regexp"
	"slices"
)

const (
	// GonbTempDirEnvName is the name of the environment variable that is set with
	// the temporary directory used to compile user's Go code.
	// It can be used by the executed Go code or by the bash scripts (started with `!`).
	GonbTempDirEnvName = "GONB_TMP_DIR"

	// InitFunctionPrefix -- functions named with this prefix will be rendered as
	// a separate `func init()`.
	InitFunctionPrefix = "init_"
)

// State holds information about Go code execution for this kernel. It's a singleton (for now).
// It hols the directory, ids, configuration, command line arguments to use and currently
// defined Go code.
//
// That is, if the user runs a cell that defines, let's say `func f(x int) int { return x+1 }`,
// the definition of `f` will be stored in Definitions field.
type State struct {
	// Kernel is set when actually connecting to JupyterServer.
	// In tests its left as nil.
	Kernel *kernel.Kernel

	// Temporary directory where Go program is build at each execution.
	UniqueID, Package, TempDir string

	// Building and executing go code configuration:
	Args         []string // Args to be passed to the program, after being executed.
	GoBuildFlags []string // Flags to be passed to `go build`, in State.Compile.
	AutoGet      bool     // Whether to do a "go get" before compiling, to fetch missing external modules.

	// Global elements defined mapped by their keys.
	Definitions *Declarations

	// gopls client
	gopls *goplsclient.Client

	// trackingInfo is everything related to tracking.
	trackingInfo *trackingInfo

	// hasGoWork: whether a go.work was created: this requires some special treatment when
	// executing `go get`, that doesn't support it. See issue #31, and gonuts discussion in
	// https://groups.google.com/g/golang-nuts/c/2Ht4c-eZzgQ.
	//
	// This is set by State.autoTrackGoWork.
	hasGoWork bool

	// goWorkUsePaths contains the paths that are marked as `use` in the `go.work` file for the kernel.
	// It is only valid if hasGoWork is true.
	//
	// This is set by State.autoTrackGoWork.
	goWorkUsePaths common.Set[string]

	// preserveTempDir indicates the temporary directory should be logged and
	// preserved for debugging.
	preserveTempDir bool

	// rawError indicates no HTML context to compilation errors should be added.
	rawError bool

	// cellExecChan serializes requests to `ExecuteCell`, since requests come from
	// Jupyter before previous cell execution finishes, and we want to keep the order.
	cellExecChan chan *cellExecParams

	// CellIsTest indicates whether the current cell is to be compiled with `go test` (as opposed to `go build`).
	// This also triggers writing the code to `main_test.go` as opposed to `main.go`.
	// Usually this is set and reset after the execution -- the default being the normal build.
	CellIsTest        bool
	CellTests         []string // Tests defined in this cell. Only used if CellIsTest==true.
	CellHasBenchmarks bool

	// CellIsWasm indicates whether the current cell is to be compiled for WebAssembly (wasm).
	CellIsWasm                  bool
	WasmDir, WasmUrl, WasmDivId string

	// Comms represents the communication with the front-end.
	Comms *comms.State

	// CaptureFile is the file where to write any cell output. It is closed and set to nil at the end of the cell
	// executions.
	// If nil, no output is to be captured.
	CaptureFile io.WriteCloser
}

// Declarations is a collection of declarations that we carry over from one cell to another.
type Declarations struct {
	Functions map[string]*Function
	Variables map[string]*Variable
	Types     map[string]*TypeDecl
	Imports   map[string]*Import
	Constants map[string]*Constant
}

// New returns an empty State object, that can be used to execute Cells.
//
// If preserveTempDir is set to true, the temporary directory is logged,
// and it's preserved when the kernel exits -- helpful for debugging.
//
// If rawError is true, the parsing of compiler errors doesn't generate HTML, instead it
// uses only text.
//
// The kernel object passed in `k` can be nil for testing, but this may lead to some leaking
// goroutines, that stop when the kernel stops.
func New(k *kernel.Kernel, uniqueID string, preserveTempDir, rawError bool) (*State, error) {
	s := &State{
		Kernel:          k,
		UniqueID:        uniqueID,
		Package:         "gonb_" + uniqueID,
		Definitions:     NewDeclarations(),
		AutoGet:         true,
		trackingInfo:    newTrackingInfo(),
		preserveTempDir: preserveTempDir,
		rawError:        rawError,
		Comms:           comms.New(),
		cellExecChan:    make(chan *cellExecParams),
	}

	// Goroutine that processes incoming ExecuteCell requests.
	// It stops when the kernel stops.
	go s.serializeExecuteCell()

	// Create directory.
	s.TempDir = path.Join(os.TempDir(), s.Package)
	err := os.Mkdir(s.TempDir, 0700)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to create temporary directory %q", s.TempDir)
	}
	if s.preserveTempDir {
		klog.Infof("Temporary work directory: %s", s.TempDir)
	}

	// Set environment variables with currently used GoNB directories.
	pwd, err := os.Getwd()
	if err != nil {
		klog.Exitf("Failed to get current directory with os.Getwd(): %+v", err)
		err = nil
	} else {
		err = os.Setenv(protocol.GONB_DIR_ENV, pwd)
		if err != nil {
			klog.Errorf("Failed to set environment variable %q: %+v", protocol.GONB_DIR_ENV, err)
			err = nil
		}
	}
	err = os.Setenv(protocol.GONB_TMP_DIR_ENV, s.TempDir)
	if err != nil {
		klog.Errorf("Failed to set environment variable %q: %+v", protocol.GONB_TMP_DIR_ENV, err)
		err = nil
	}

	if err = s.GoModInit(); err != nil {
		return nil, err
	}

	if _, err = exec.LookPath("gopls"); err == nil {
		s.gopls = goplsclient.New(s.TempDir)
		err = s.gopls.Start()
		if err != nil {
			klog.Errorf("Failed to start `gopls`: %v", err)
		}
		klog.V(1).Infof("Started `gopls`.")
	} else {
		msg := `
Program gopls is not installed. It is used to inspect into code
and provide contextual information and autocompletion. It is a 
standard Go toolkit package. You can install it from the notebook
with:

` + "```" + `
!go install golang.org/x/tools/gopls@latest
` + "```\n"
		klog.Errorf(msg)
	}

	// Try to find out Jupyter root's directory.
	jupyterRoot, err := JupyterRootDirectory()
	if err != nil {
		klog.Warningf("Could not find Jupyter root directory, %%wasm will not work: %v", err)
	} else {
		err = os.Setenv(protocol.GONB_JUPYTER_ROOT_ENV, jupyterRoot)
		if err != nil {
			klog.Errorf("Failed to set environment variable %q: %v", protocol.GONB_JUPYTER_ROOT_ENV, err)
			err = nil
		}
	}

	klog.Infof("GoNB: jupyter root in %q, tmp Go code in %q", jupyterRoot, s.TempDir)
	return s, nil
}

// GoModInit removes current `go.mod` if it already exists, and recreate it with `go mod init`.
func (s *State) GoModInit() error {
	err := os.Remove(path.Join(s.TempDir, "go.mod"))
	if err != nil && !os.IsNotExist(err) {
		klog.Errorf("Failed to remove go.mod: %+v", err)
		return errors.Wrapf(err, "failed to remove go.mod")
	}
	// ProgramExecutor `go mod init` on given directory.
	cmd := exec.Command("go", "mod", "init", s.Package)
	cmd.Dir = s.TempDir
	var output []byte
	output, err = cmd.CombinedOutput()
	if err != nil {
		klog.Errorf("Failed to run `go mod init %s`:\n%s", s.Package, output)
		return errors.Wrapf(err, "failed to run %q", cmd.String())
	}
	return nil
}

// Stop stops gopls and removes temporary files and directories.
func (s *State) Stop() error {
	if s.gopls != nil {
		s.gopls.Shutdown()
		s.gopls = nil
	}
	if s.TempDir != "" && !s.preserveTempDir {
		err := os.RemoveAll(s.TempDir)
		if err != nil {
			return errors.Wrapf(err, "Failed to remove goexec.State temporary directory %s", s.TempDir)
		}
		s.TempDir = "/"
	}
	if s.Comms != nil {
		// Close without a message (no sending back a comm_close message),
		// if not yet closed.
		s.Comms.Close(nil)
	}
	return nil
}

func NewDeclarations() *Declarations {
	return &Declarations{
		Imports:   make(map[string]*Import),
		Functions: make(map[string]*Function),
		Variables: make(map[string]*Variable),
		Types:     make(map[string]*TypeDecl),
		Constants: make(map[string]*Constant),
	}
}

// Copy returns a new deep copy of the declarations.
func (d *Declarations) Copy() *Declarations {
	d2 := &Declarations{
		Imports:   make(map[string]*Import, len(d.Imports)),
		Functions: make(map[string]*Function, len(d.Functions)),
		Variables: make(map[string]*Variable, len(d.Variables)),
		Types:     make(map[string]*TypeDecl, len(d.Types)),
		Constants: make(map[string]*Constant, len(d.Constants)),
	}
	d2.MergeFrom(d)
	return d2
}

// MergeFrom declarations in d2.
func (d *Declarations) MergeFrom(d2 *Declarations) {
	copyMap(d.Imports, d2.Imports)
	copyMap(d.Functions, d2.Functions)
	variablesCopyFrom(d.Variables, d2.Variables)
	copyMap(d.Types, d2.Types)
	copyMap(d.Constants, d2.Constants)
}

func copyMap[K comparable, V any](dst, src map[K]V) {
	for k, v := range src {
		dst[k] = v
	}
}

// variablesCopyFrom is similar to copyMap above, but it handles the special tuple cases.
func variablesCopyFrom(dst, src map[string]*Variable) {
	for k, v := range src {
		if oldV, found := dst[k]; found {
			// If there is a previous variable with the same name, tied to a tuple that is not the same
			// tuple that is being merged, then remove all the definitions of the previous tuple.
			if oldV.TupleDefinitions != nil && !slices.Equal(oldV.TupleDefinitions, v.TupleDefinitions) {
				for _, removeV := range oldV.TupleDefinitions {
					// One of the removeV will be equal to oldV, which is fine.
					delete(dst, removeV.Key)
				}
			}
		}
		dst[k] = v
	}
}

// ClearCursor wherever declaration it may be.
func (d *Declarations) ClearCursor() {
	clearCursor(d.Imports)
	clearCursor(d.Functions)
	clearCursor(d.Variables)
	clearCursor(d.Types)
	clearCursor(d.Constants)
}

func clearCursor[K comparable, V interface{ ClearCursor() }](data map[K]V) {
	for _, v := range data {
		v.ClearCursor()
	}
}

// DropFuncInit drops declarations of `func init()`: the parser generates this for the `func init_*`,
// and it shouldn't be considered new declarations if reading from generated code.
func (d *Declarations) DropFuncInit() {
	if _, found := d.Functions["init"]; found {
		delete(d.Functions, "init")
	}
}

//go:generate stringer -type=ElementType goexec.go

type ElementType int

const (
	Invalid ElementType = iota
	FunctionType
	ImportType
	VarType
	ConstType
)

// Cursor represents a cursor position in a cell or file.
// The Col is given as bytes in the line expected to be encoded as UTF-8.
type Cursor struct {
	Line, Col int
}

const NoCursorLine = int(-1)

var NoCursor = Cursor{Line: NoCursorLine, Col: 0}

func (c Cursor) HasCursor() bool {
	return c.Line != NoCursorLine
}

// CursorFrom returns a new Cursor adjusted
func (c Cursor) CursorFrom(line, col int) Cursor {
	if !c.HasCursor() {
		return c
	}
	return Cursor{Line: c.Line + line, Col: c.Col + col}
}

// ClearCursor resets the cursor to an invalid state. This method is needed
// for the structs that embed Cursor.
func (c *Cursor) ClearCursor() {
	c.Line = NoCursorLine
}

// String implements the fmt.Stringer interface.
func (c Cursor) String() string {
	if c.HasCursor() {
		return fmt.Sprintf("[L:%d, Col:%d]", c.Line, c.Col)
	}
	return "[NoCursor]"
}

// CellLines identifies a cell (by its execution id) and the Lines
// corresponding to a declaration.
type CellLines struct {
	// Id of the cell where the definition comes from. It is set to -1 if the declaration was automatically
	// created (for instance by goimports).
	Id int

	// Lines has one value per line used in the declaration. The point to the cell line where it was declared.
	// Some of these numbers may be NoCursorLine (-1) indicating that they are inserted automatically and don't.
	// have corresponding Lines in any cell.
	//
	// If Id is -1, Lines will be nil, which indicates the content didn't come from any cell.
	Lines []int
}

// Append id and line numbers to fileToCellIdAndLine, a slice of `CellIdAndLine`. This is used when
// rendering a declaration to a file.
func (c CellLines) Append(fileToCellIdAndLine []CellIdAndLine) []CellIdAndLine {
	for _, lineNum := range c.Lines {
		fileToCellIdAndLine = append(fileToCellIdAndLine, CellIdAndLine{Id: c.Id, Line: lineNum})
	}
	return fileToCellIdAndLine
}

// Function definition, parsed from a notebook cell.
type Function struct {
	Cursor
	CellLines

	Key            string
	Name, Receiver string
	Definition     string // Multi-line definition, includes comments preceding definition.

}

// Variable definition, parsed from a notebook cell.
//
// There is one special case, where one variable entry will define multiple variables, when we have line like:
//
//	var a, b, c = someFuncThatReturnsATuple()
//
// For such cases, one set TupleDefinitions in order ("a", "b", "c"). And we define the following behavior:
//
//   - Only the first element of TupleDefinitions is rendered, but it renders the tuple definition.
//   - If any of the elements of the tuple is redefined or removed, all elements are removed.
//
// This means that if "a" is redefined, "b" and "c" disappear. And that if "b" or "c" are redefined, it will
// yield and error, that is subtle to track.
type Variable struct {
	Cursor
	CellLines

	CursorInName, CursorInType, CursorInValue bool
	Key, Name                                 string
	TypeDefinition, ValueDefinition           string // Type definition may be empty.

	// TupleDefinitions are present when multiple variables are tied to the same definition as in `var a, b, c = someFunc()`.
	TupleDefinitions []*Variable
}

// TypeDecl definition, parsed from a notebook cell.
type TypeDecl struct {
	Cursor
	CellLines

	Key            string // Same as the name here.
	TypeDefinition string // Type definition which includes the name.
	CursorInType   bool
}

// Constant represents the declaration of a constant. Because when appearing in block
// they inherit its definition form the previous line, we need to preserve the blocks.
// For this, we use Next/Prev links.
type Constant struct {
	Cursor
	CellLines

	Key                                      string
	TypeDefinition, ValueDefinition          string // Can be empty, if used as iota.
	CursorInKey, CursorInType, CursorInValue bool
	Next, Prev                               *Constant // Next and previous declaration in same Const block.
}

// Import represents an import to be included -- if not used it's automatically removed by
// `goimports`.
type Import struct {
	Cursor
	CellLines

	Key                         string
	Path, Alias                 string
	CursorInPath, CursorInAlias bool
}

var reDefaultImportPathAlias = regexp.MustCompile(`^.*?(\w[\w0-9_]*)\s*$`)

// Reset clears all the memorized Go declarations. It becomes as if no cells had
// been executed so far -- except for configurations and arguments that remain unchanged.
//
// It is connected to the special command `%reset`.
func (s *State) Reset() {
	s.Definitions = NewDeclarations()
}
