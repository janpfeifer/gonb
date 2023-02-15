// Package goexec executes cells with Go code for the gonb kernel.
//
// It defines a State object, that carries all the globals defined so far. It provides
// the ExecuteCell method, to run a new cell.
package goexec

import (
	"github.com/janpfeifer/gonb/goexec/goplsclient"
	"github.com/pkg/errors"
	"log"
	"os"
	"os/exec"
	"path"
	"regexp"
)

type State struct {
	// Temporary directory where Go program is build at each execution.
	UniqueID, Package, TempDir string

	// Building and executing go code configuration:
	Args    []string // Args to be passed to the program, after being executed.
	AutoGet bool     // Whether to do a "go get" before compiling, to fetch missing external modules.

	// Global elements defined mapped by their keys.
	Decls *Declarations

	// gopls client
	gopls *goplsclient.Client
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
func New(uniqueID string) (*State, error) {
	s := &State{
		UniqueID: uniqueID,
		Package:  "gonb_" + uniqueID,
		Decls:    NewDeclarations(),
		AutoGet:  true,
	}

	// Create directory.
	s.TempDir = path.Join(os.TempDir(), s.Package)
	err := os.Mkdir(s.TempDir, 0700)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to create temporary directory %q", s.TempDir)
	}

	// Run go mod init on given directory.
	cmd := exec.Command("go", "mod", "init", s.Package)
	cmd.Dir = s.TempDir
	var output []byte
	output, err = cmd.CombinedOutput()
	if err != nil {
		log.Printf("Failed to run `go mod init %s`:\n%s", s.Package, output)
		return nil, errors.Wrapf(err, "failed to run %q", cmd.String())
	}

	if _, err = exec.LookPath("gopls"); err == nil {
		s.gopls = goplsclient.New(s.TempDir)
		err = s.gopls.Start()
		if err != nil {
			log.Printf("Failed to start `gopls`: %v", err)
		}
		log.Printf("Started `gopls`.")
		//s.gopls.SetAddress("/tmp/gopls-daemon-socket")
		//err = s.gopls.Connect(context.Background())
		//if err != nil {
		//	log.Printf("Failed to connect to `gopls`: %v", err)
		//}
		//log.Printf("Connected to `gopls`.")

	} else {
		msg := `
Program gopls is not installed. It is used to inspect into code
and provide contextual information and autocompletion. It is a 
standard Go toolkit package. You can install it from the notebook
with:

` + "```" + `
!go install golang.org/x/tools/gopls@latest
` + "```\n"
		log.Printf(msg)
		err = nil
	}

	log.Printf("Initialized goexec.State in %s", s.TempDir)
	return s, nil
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

func (c *Cursor) HasCursor() bool {
	return c.Line != NoCursorLine
}

// CursorFrom returns a new Cursor adjusted
func (c *Cursor) CursorFrom(line int) Cursor {
	if !c.HasCursor() {
		return *c
	}
	return Cursor{Line: c.Line + line, Col: c.Col}
}

func (c *Cursor) ClearCursor() {
	c.Line = -1
}

// Function definition.
type Function struct {
	Cursor
	Key            string
	Name, Receiver string
	Definition     string // Multi-line definition, includes comments preceding definition.

}

type Variable struct {
	Cursor
	Key, Name                       string
	TypeDefinition, ValueDefinition string // Type definition may be empty.
}

type TypeDecl struct {
	Cursor
	Key            string // Same as the name here.
	TypeDefinition string // Type definition may be empty.
}

// Constant represents the declaration of a constant. Because when appearing in block
// they inherit its definition form the previous line, we need to preserve the blocks.
// For this we use Next/Prev links.
type Constant struct {
	Cursor
	Key                             string
	TypeDefinition, ValueDefinition string    // Can be empty, if used as iota.
	Next, Prev                      *Constant // Next and previous declaration in same Const block.
}

// Import represents an import to be included -- if not used it's automatically removed by
// `goimports`.
type Import struct {
	Cursor
	Key         string
	Path, Alias string
}

var reDefaultImportPathAlias = regexp.MustCompile(`^.*?(\w[\w0-9_]*)\s*$`)

// NewImport from the importPath and it's alias. If alias is empty or "<nil>", it will default to the
// last name part of the importPath.
func NewImport(importPath, alias string) *Import {
	key := alias
	if key == "" {
		parts := reDefaultImportPathAlias.FindStringSubmatch(importPath)
		if len(parts) < 2 {
			key = importPath
		} else {
			key = parts[1]
		}
	} else if key == "." {
		// More than one import can be moved to the current namespace.
		key = ".~" + importPath
	}
	return &Import{Key: key, Path: importPath, Alias: alias}
}

func (s *State) Reset() {
	s.Decls = NewDeclarations()
}
