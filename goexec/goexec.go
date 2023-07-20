// Package goexec executes cells with Go code for the gonb kernel.
//
// It defines a State object, that carries all the globals defined so far. It provides
// the ExecuteCell method, to run a new cell.
package goexec

import (
	"fmt"
	"github.com/janpfeifer/gonb/common"
	"github.com/janpfeifer/gonb/goexec/goplsclient"
	"github.com/pkg/errors"
	"k8s.io/klog/v2"
	"os"
	"os/exec"
	"path"
	"regexp"
)

// State holds information about Go code execution for this kernel. It's a singleton (for now).
// It hols the directory, ids, configuration, command line arguments to use and currently
// defined Go code.
//
// That is, if the user runs a cell that defines, let's say `func f(x int) int { return x+1 }`,
// the definition of `f` will be stored in Definitions field.
type State struct {
	// Temporary directory where Go program is build at each execution.
	UniqueID, Package, TempDir string

	// Building and executing go code configuration:
	Args    []string // Args to be passed to the program, after being executed.
	AutoGet bool     // Whether to do a "go get" before compiling, to fetch missing external modules.

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
	rawError  bool

	// goWorkUsePaths contains the paths that are marked as `use` in the `go.work` file for the kernel.
	// It is only valid if hasGoWork is true.
	//
	// This is set by State.autoTrackGoWork.
	goWorkUsePaths common.Set[string]
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
func New(uniqueID string, rawError bool) (*State, error) {
	s := &State{
		UniqueID:     uniqueID,
		Package:      "gonb_" + uniqueID,
		Definitions:  NewDeclarations(),
		AutoGet:      true,
		trackingInfo: newTrackingInfo(),
		rawError:     rawError,
	}

	// Create directory.
	s.TempDir = path.Join(os.TempDir(), s.Package)
	err := os.Mkdir(s.TempDir, 0700)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to create temporary directory %q", s.TempDir)
	}

	// Exec go mod init on given directory.
	cmd := exec.Command("go", "mod", "init", s.Package)
	cmd.Dir = s.TempDir
	var output []byte
	output, err = cmd.CombinedOutput()
	if err != nil {
		klog.Errorf("Failed to run `go mod init %s`:\n%s", s.Package, output)
		return nil, errors.Wrapf(err, "failed to run %q", cmd.String())
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

	klog.Infof("Initialized goexec.State in %s", s.TempDir)
	return s, nil
}

// Finalize stops gopls and removes temporary files and directories.
func (s *State) Finalize() error {
	if s.gopls != nil {
		s.gopls.Shutdown()
		s.gopls = nil
	}
	if s.TempDir != "" {
		err := os.RemoveAll(s.TempDir)
		if err != nil {
			return errors.Wrapf(err, "Failed to remove goexec.State temporary directory %s", s.TempDir)
		}
		s.TempDir = "/"
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
	copyMap(d.Variables, d2.Variables)
	copyMap(d.Types, d2.Types)
	copyMap(d.Constants, d2.Constants)
}

func copyMap[K comparable, V any](dst, src map[K]V) {
	for k, v := range src {
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

// CellLines identifies a cell (by its execution id) and the lines
// corresponding to a declaration.
type CellLines struct {
	// Id of the cell where the definition comes from. It is set to -1 if the declaration was automatically
	// created (for instance by goimports).
	Id int

	// Lines has one value per line used in the declaration. The point to the cell line where it was declared.
	// Some of these numbers may be NoCursorLine (-1) indicating that they are inserted automatically and don't.
	// have corresponding lines in any cell.
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
type Variable struct {
	Cursor
	CellLines

	CursorInName, CursorInType, CursorInValue bool
	Key, Name                                 string
	TypeDefinition, ValueDefinition           string // Type definition may be empty.
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
// For this we use Next/Prev links.
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
