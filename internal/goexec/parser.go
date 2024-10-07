package goexec

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"io/fs"
	"math/rand"
	"os"
	"regexp"
	"strings"

	. "github.com/janpfeifer/gonb/common"
	"github.com/janpfeifer/gonb/internal/kernel"
	"github.com/pkg/errors"
	"k8s.io/klog/v2"
)

// This file implements functions related to the parsing of the Go code.
// It is used to properly merge code coming from the execution of different cells.

// parseInfo holds the information needed for parsing Go code and some key helper methods.
type parseInfo struct {
	cursor        Cursor
	cellId        int
	fileSet       *token.FileSet
	filesContents map[string]string

	// fileToCellIdAndLine holds for each line in the `main.go` file, the corresponding cell id and line number in the
	// cell. This is used when reporting back errors with a file number. Values of -1 (NoCursorLine) are injected Lines
	// that have no correspondent value in the cell code.
	fileToCellIdAndLine []CellIdAndLine
}

// getCursor returns the cursor position within this declaration, if the original cursor falls in there.
func (pi *parseInfo) getCursor(node ast.Node) Cursor {
	from, to := node.Pos(), node.End()
	if !pi.cursor.HasCursor() {
		return NoCursor
	}
	fromPos, toPos := pi.fileSet.Position(from), pi.fileSet.Position(to)
	for lineNum := fromPos.Line; lineNum <= toPos.Line; lineNum++ {
		// Notice that parser Lines are 1-based, we keep them 0-based in the cursor.
		if lineNum-1 == pi.cursor.Line {
			// Column is set either relative to the start of the definition, if in the
			// same start lineNum, or the definition column.
			col := pi.cursor.Col
			if lineNum == fromPos.Line && col < fromPos.Column-1 {
				// Cursor before node.
				//log.Printf("Cursor before node: %q", pi.extractContentOfNode(node))
				return NoCursor
			}
			if lineNum == toPos.Line && col >= toPos.Column-1 {
				// Cursor after node.
				//log.Printf("Cursor after node: %q", pi.extractContentOfNode(node))
				return NoCursor
			}
			if lineNum == fromPos.Line {
				col = col - (fromPos.Column - 1) // fromPos column is 1-based.
			}
			c := Cursor{lineNum - fromPos.Line, col}
			//log.Printf("Found cursor at %v in definition, (%d, %d):\n%s", c, fromPos.Line, fromPos.Column,
			//	pi.extractContentOfNode(node))
			return c
		}
	}
	return NoCursor
}

// calculateCellLines returns the CellLines information for the corresponding ast.Node.
func (pi *parseInfo) calculateCellLines(node ast.Node) (c CellLines) {
	c.Id = pi.cellId
	from, to := node.Pos(), node.End()
	fromPos, toPos := pi.fileSet.Position(from), pi.fileSet.Position(to)
	numLines := (toPos.Line - fromPos.Line) + 1
	c.Lines = make([]int, 0, numLines)
	for lineNum := fromPos.Line; lineNum <= toPos.Line; lineNum++ {
		if pi.fileToCellIdAndLine != nil {
			c.Lines = append(c.Lines, pi.fileToCellIdAndLine[lineNum-1].Line)
		} else {
			c.Lines = append(c.Lines, NoCursorLine)
		}
	}
	return
}

// extractContentOfNode from files, given the golang parser's tokens.
//
// Currently, we generate all the content into the file `main.go`, so fileContents will only have
// one entry.
//
// This is used to get the exact definition (string) of an element (function, variable, const, import, type, etc.)
func (pi *parseInfo) extractContentOfNode(node ast.Node) string {
	f := pi.fileSet.File(node.Pos())
	from, to := f.Offset(node.Pos()), f.Offset(node.End())
	contents, found := pi.filesContents[f.Name()]
	if !found {
		return fmt.Sprintf("Didn't find file %q", f.Name())
	}
	return contents[from:to]
}

// parseFromGoCode reads the Go code written in `s.TempDir` and parses its declarations.
// See object Declarations.
//
// Only the files "main.go" or "main_test.go" are parsed. If the user created separate Go files
// in `s.TempDir`, those are left as is.
//
// This is called by parseLinesAndComposeMain, after the Go code is written.
//
// Parameters:
//   - `msg`: connection to notebook, to report errors. If nil, errors are not reported.
//   - `cellId`: execution id of the cell being processed. Set to -1 if later this cell will be discarded (for
//     instance, when parsing for auto-complete).
//   - `Cursor`: where it is in the file. If set (that is, `cursor != NoCursor`), it will record the position
//     of the cursor in the corresponding declaration.
//   - `fileToCellLine`: for each line in the `main.go` file, the corresponding line number in the cell. This
//     is used when reporting back errors with a file number. Values of -1 (NoCursorLine) are injected Lines
//     that have no correspondent value in the cell code. It can be nil if there is no information mapping
//     file Lines to cell Lines.
//   - `noPkg`: when parsing contents directly from the user, no `package ` line
func (s *State) parseFromGoCode(msg kernel.Message,
	cellId int, cursor Cursor, fileToCellIdAndLine []CellIdAndLine) (decls *Declarations, err error) {
	decls = NewDeclarations()
	pi := &parseInfo{
		cursor:  cursor,
		fileSet: token.NewFileSet(),

		cellId:              cellId,
		fileToCellIdAndLine: fileToCellIdAndLine,
	}
	var packages map[string]*ast.Package
	// Parse "main.go" or "main_test.go".
	packages, err = parser.ParseDir(pi.fileSet, s.TempDir, func(info fs.FileInfo) bool {
		name := info.Name()
		keep := name == "main.go" || name == "main_test.go"
		klog.V(2).Infof("parser.ParseDir().filter(%q) -> keep=%v", name, keep)
		return keep
	}, parser.SkipObjectResolution) // |parser.AllErrors
	if err != nil {
		if msg != nil {
			err = s.DisplayErrorWithContext(msg, fileToCellIdAndLine, err.Error(), err)
		}
		err = errors.Wrapf(err, "parsing go files in TempDir %q", s.TempDir)
		return
	}

	pi.filesContents = make(map[string]string)
	for name, pkgAst := range packages {
		klog.V(2).Infof("Parsed package %q:\n", pkgAst.Name)
		if name != "main" {
			err = errors.New("Invalid package %q declared: there should be no `package` declaration, " +
				"GoNB will automatically create `package main` when combining cell code.")
			return
		}
		for fileName, fileObj := range pkgAst.Files {
			// Currently, there is only `main.go`, and potentially `main_test.go` files.
			klog.V(2).Infof("> Parsed file %q: %d declarations\n", fileName, len(fileObj.Decls))
			content, err := os.ReadFile(fileName)
			if err != nil {
				return nil, errors.Wrapf(err, "Failed to read %q", fileObj.Name)
			}
			pi.filesContents[fileName] = string(content)
			// Incorporate Imports
			for _, entry := range fileObj.Imports {
				pi.ParseImportEntry(decls, entry)
			}

			// Enumerate various declarations.
			for _, decl := range fileObj.Decls {
				switch typedDecl := decl.(type) {
				case *ast.FuncDecl:
					klog.V(2).Infof("> Declaration %T: %+v", typedDecl, typedDecl.Name)
					pi.ParseFuncEntry(decls, typedDecl)
				case *ast.GenDecl:
					klog.V(2).Infof("> Declaration %T: %s", typedDecl, typedDecl.Tok)
					if typedDecl.Tok == token.IMPORT {
						// Imports are handled above.
						continue
					} else if typedDecl.Tok == token.VAR {
						pi.ParseVarEntry(decls, typedDecl)
					} else if typedDecl.Tok == token.CONST {
						pi.ParseConstEntry(decls, typedDecl)
					} else if typedDecl.Tok == token.TYPE {
						pi.ParseTypeEntry(decls, typedDecl)
					} else {
						klog.Warningf("Dropped unknown generic declaration of type %s\n", typedDecl.Tok)
					}
				default:
					klog.Warningf("Dropped unknown declaration type\n")
				}
			}
		}
	}
	return
}

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

// ParseImportEntry registers a new Import declaration based on the ast.ImportSpec. See State.parseFromGoCode
func (pi *parseInfo) ParseImportEntry(decls *Declarations, entry *ast.ImportSpec) {
	var alias string
	if entry.Name != nil {
		alias = entry.Name.Name
	}
	value := entry.Path.Value
	value = value[1 : len(value)-1] // Remove quotes.
	importEntry := NewImport(value, alias)
	importEntry.CellLines = pi.calculateCellLines(entry.Path)

	// Find if and where the cursor may be.
	if c := pi.getCursor(entry.Path); c.HasCursor() {
		importEntry.CursorInPath = true
		importEntry.Cursor = c
	} else if entry.Name != nil {
		if c := pi.getCursor(entry.Name); c.HasCursor() {
			importEntry.CursorInAlias = true
			importEntry.Cursor = c
		}
	}
	decls.Imports[importEntry.Key] = importEntry
}

// ParseFuncEntry registers a new `func` declaration based on the ast.FuncDecl. See State.parseFromGoCode
func (pi *parseInfo) ParseFuncEntry(decls *Declarations, funcDecl *ast.FuncDecl) {
	// Incorporate functions.
	key := funcDecl.Name.Name
	if funcDecl.Recv != nil && len(funcDecl.Recv.List) > 0 {
		typeName := "unknown"
		switch fieldType := funcDecl.Recv.List[0].Type.(type) {
		case *ast.Ident:
			typeName = fieldType.Name
		case *ast.StarExpr:
			typeName = pi.extractContentOfNode(fieldType.X)
		}
		key = fmt.Sprintf("%s~%s", typeName, key)
	}
	f := &Function{Key: key, Definition: pi.extractContentOfNode(funcDecl)}
	f.CellLines = pi.calculateCellLines(funcDecl)
	f.Cursor = pi.getCursor(funcDecl)
	decls.Functions[f.Key] = f
}

// ParseVarEntry registers a new `var` declaration based on the ast.GenDecl. See State.parseFromGoCode
//
// There are a many variations to consider:
//
// (1) var v Type            -> One variable, one type
// (2) var v1, v2 Type       -> N variables, 1 type
// (3) var v1, v2 = 0, 1     -> N variables, N values
// (4) var v1, v2 int = 0, 1 -> N variables, 1 type, N values
// (5) var c, _ = os.ReadFile(...)  -> N variables, one function that returns tuple.
//
// Case (5) is the most complicated, because the N variables are tied to one definition.
// We key it on the first variable, but this may cause issues with some code like `var a, b = someFunc()`
// and later the user decides to redefine `var b = 10`. It will require manual removal of the definition of `a`.
func (pi *parseInfo) ParseVarEntry(decls *Declarations, genDecl *ast.GenDecl) {
	// Multiple declarations in the same line may share the cursor (e.g: `var a, b int` if the cursor
	// is in the `int` token). Only the first definition ('a' in the example) takes the cursor.
	cursorFound := false
	for _, spec := range genDecl.Specs {
		vSpec := spec.(*ast.ValueSpec)
		vType := vSpec.Type
		var typeDefinition string
		cursorInType := NoCursor
		if vType != nil {
			typeDefinition = pi.extractContentOfNode(vType)
			cursorInType = pi.getCursor(vType)
		}

		isTuple := len(vSpec.Names) > 0 && len(vSpec.Values) == 1
		var tupleDefinitions []*Variable
		if isTuple {
			tupleDefinitions = make([]*Variable, len(vSpec.Names))
		}

		// Each spec may be a list of variables (comma separated).
		for nameIdx, name := range vSpec.Names {
			v := &Variable{Name: name.Name, TypeDefinition: typeDefinition}
			if isTuple {
				v.TupleDefinitions = tupleDefinitions
				tupleDefinitions[nameIdx] = v
			}
			if !cursorFound {
				if c := pi.getCursor(name); c.HasCursor() {
					v.CursorInName = true
					v.Cursor = c
					cursorFound = true
				}
			}
			if !cursorFound && cursorInType.HasCursor() {
				v.CursorInType = true
				v.Cursor = cursorInType
				cursorFound = true
			}
			if len(vSpec.Values) > nameIdx {
				v.ValueDefinition = pi.extractContentOfNode(vSpec.Values[nameIdx])
				if !cursorFound {
					if c := pi.getCursor(vSpec.Values[nameIdx]); c.HasCursor() {
						v.CursorInValue = true
						v.Cursor = c
						cursorFound = true
					}
				}
			}

			v.Key = v.Name
			v.CellLines = pi.calculateCellLines(vSpec)
			if v.Name == "_" {
				// Each un-named reference has a unique key.
				v.Key = "_~" + fmt.Sprintf("%06d", rand.Int63n(1_000_000))
			}
			decls.Variables[v.Key] = v
		}
	}
}

// ParseConstEntry registers a new `const` declaration based on the ast.GenDecl. See State.parseFromGoCode
func (pi *parseInfo) ParseConstEntry(decls *Declarations, typedDecl *ast.GenDecl) {
	var prevConstDecl *Constant
	// Multiple declarations in the same line may share the cursor (e.g: `var a, b int` if the cursor
	// is in the `int` token). Only the first definition ('a' in the example) takes the cursor.
	cursorFound := false
	for _, spec := range typedDecl.Specs {
		vSpec := spec.(*ast.ValueSpec)
		vType := vSpec.Type
		var typeDefinition string
		cursorInType := NoCursor
		if vType != nil {
			typeDefinition = pi.extractContentOfNode(vType)
			cursorInType = pi.getCursor(vType)
		}
		// Each spec may be a list of variables (comma separated).
		for nameIdx, name := range vSpec.Names {
			c := &Constant{Cursor: NoCursor, Key: name.Name, TypeDefinition: typeDefinition}
			c.Prev = prevConstDecl
			if c.Prev != nil {
				c.Prev.Next = c
			}
			prevConstDecl = c

			if !cursorFound {
				if cursor := pi.getCursor(name); cursor.HasCursor() {
					c.CursorInKey = true
					c.Cursor = cursor
					cursorFound = true
				}
			}
			if !cursorFound && cursorInType.HasCursor() {
				c.CursorInType = true
				c.Cursor = cursorInType
				cursorFound = true
			}
			if len(vSpec.Values) > nameIdx {
				c.ValueDefinition = pi.extractContentOfNode(vSpec.Values[nameIdx])
				if !cursorFound {
					if cursor := pi.getCursor(vSpec.Values[nameIdx]); cursor.HasCursor() {
						c.CursorInValue = true
						c.Cursor = cursor
						cursorFound = true
					}
				}
			}
			c.CellLines = pi.calculateCellLines(vSpec)
			decls.Constants[c.Key] = c
		}
	}
}

func (pi *parseInfo) ParseTypeEntry(decls *Declarations, typedDecl *ast.GenDecl) {
	// There is usually only one spec for a TYPE declaration:
	for _, spec := range typedDecl.Specs {
		tSpec := spec.(*ast.TypeSpec)
		name := tSpec.Name.Name
		tDef := pi.extractContentOfNode(tSpec)
		tDecl := &TypeDecl{Key: name, TypeDefinition: tDef}
		if c := pi.getCursor(tSpec); c.HasCursor() {
			tDecl.Cursor = c
			tDecl.CursorInType = true
		}
		tDecl.CellLines = pi.calculateCellLines(tSpec)
		decls.Types[name] = tDecl
	}
}

// parseLinesAndComposeMain parses the cell (given in Lines and skipLines), merges with
// memorized declarations in the State (presumably from previous Cell runs) and compose a `main.go`.
//
// On return the `main.go` file (in `s.TempDir`) has been updated, and it returns the updated merged
// declarations (`decls` is not changed) and optionally the cursor position adjusted into the newly generate
// `main.go` file.
//
// If cursorInCell defines a cursor (it can be set to NoCursor), but the cursor position
// is not rendered in the resulting `main.go`, a CursorLost err is returned.
//
// skipLines are Lines that should not be considered as Go code. Typically, these are the special
// commands (like `%%`, `%args`, `%reset`, or bash Lines starting with `!`).
//
// Note: `func init_*()` functions are rendered as `func init()`: that means if one is parsing an
// already generated code, the original `func init_*()` will be missing (which is usually ok), but
// there will be a newly generated `func init()` that shouldn't be memorized. See
func (s *State) parseLinesAndComposeMain(
	msg kernel.Message,
	cellId int, lines []string, skipLines Set[int], cursorInCell Cursor) (
	updatedDecls *Declarations, mainDecl *Function, cursorInFile Cursor, fileToCellIdAndLine []CellIdAndLine, err error) {
	cursorInFile = NoCursor

	var fileToCellLine []int
	if err = s.RemoveCode(); err != nil {
		return
	}
	cursorInFile, fileToCellLine, err = s.createGoFileFromLines(s.CodePath(), cellId, lines, skipLines, cursorInCell)
	if err != nil {
		return
	}
	fileToCellIdAndLine = MakeFileToCellIdAndLine(cellId, fileToCellLine)

	data, _ := os.ReadFile(s.CodePath())
	klog.V(2).Infof("File: %s\n%s", s.CodePath(), string(data))

	// Parse declarations in created `main.go` file.
	var newDecls *Declarations
	newDecls, err = s.parseFromGoCode(msg, cellId, cursorInFile, fileToCellIdAndLine)
	if s.CellIsTest {
		s.SetCellTests(newDecls)
	}

	if err != nil {
		return
	}

	// Checks whether there is a "main" function defined in the code.
	mainDecl, hasMain := newDecls.Functions["main"]
	if hasMain {
		// Remove "main" from newDecls: this should not be stored from one cell execution from
		// another.
		delete(newDecls.Functions, "main")
	} else {
		// Declare a stub main function, just so we can try to compile the final code.
		mainDecl = &Function{
			Cursor:     NoCursor,
			CellLines:  CellLines{},
			Key:        "main",
			Name:       "main",
			Receiver:   "",
			Definition: "func main() { flag.Parse() }",
		}
	}

	// Merge cell declarations with a copy of the current state: we don't want to commit the new
	// declarations until they compile successfully.
	updatedDecls = s.Definitions.Copy()
	updatedDecls.ClearCursor()
	updatedDecls.MergeFrom(newDecls)
	if s.CellIsWasm {
		s.ExportWasmConstants(updatedDecls)
	}

	// Render declarations to main.go.
	cursorInFile, fileToCellIdAndLine, err = s.createCodeFileFromDecls(updatedDecls, mainDecl)
	if err != nil {
		err = errors.WithMessagef(err, "while composing main.go with all declarations")
		return
	}
	if cursorInCell.HasCursor() && !cursorInFile.HasCursor() {
		// Returns empty data, which returns a "not found."
		err = errors.WithStack(CursorLost)
		return
	}
	if cursorInCell.HasCursor() && klog.V(1).Enabled() {
		s.logCursor(cursorInFile)
	}
	return
}

const cursorStr = "‸"

// logCursor will log the line in `main.go` the cursor is pointing to, and puts a
// "*" where the
func (s *State) logCursor(cursor Cursor) {
	if !cursor.HasCursor() {
		klog.Infof("Cursor not defined.")
		return
	}
	content, err := s.readMainGo()
	if err != nil {
		klog.Errorf("Failed to read main.go, for debugging.")
		return
	}
	klog.Infof("Cursor in main.go (%+v): %s", cursor, lineWithCursor(content, cursor))
}

func lineWithCursor(content string, cursor Cursor) string {
	if !cursor.HasCursor() {
		return "cursor position non-existent"
	}
	var modLine string
	lines := strings.Split(content, "\n")
	if cursor.Line < len(lines) {
		line := lines[cursor.Line]
		if cursor.Col < len(line) {
			modLine = line[:cursor.Col] + cursorStr + line[cursor.Col:]
		} else {
			modLine = line + cursorStr
		}
	}
	return modLine
}

// readMainGo reads the contents of main.go file.
func (s *State) readMainGo() (string, error) {
	f, err := os.Open(s.CodePath())
	if err != nil {
		return "", errors.Wrapf(err, "failed readMainGo()")
	}
	defer func() {
		_ = f.Close() // Ignoring err on closing file for reading.
	}()
	content, err := io.ReadAll(f)
	if err != nil {
		return "", errors.Wrapf(err, "failed readMainGo()")
	}
	return string(content), nil
}

// SetCellTests sets the test functions (Test...) defined in this cell.
// The default for `%test` is to run only the current tests, this is the
// function that given the new declarations created in this cells, figures
// out which are those tests.
func (s *State) SetCellTests(decls *Declarations) {
	s.CellTests = nil
	for fName := range decls.Functions {
		if strings.HasPrefix(fName, "Test") && fName != "TestMain" && !strings.Contains(fName, "~") {
			s.CellTests = append(s.CellTests, fName)
		} else if strings.HasPrefix(fName, "Benchmark") && !strings.Contains(fName, "~") {
			s.CellTests = append(s.CellTests, fName)
			s.CellHasBenchmarks = true
		}
	}
	klog.V(2).Infof("SetCellTests: %v", s.CellTests)
}

// DefaultCellTestArgs generate the default `go test` arguments, if none is
// given.
// It includes `-test.v` and `-test.run` matching the tests defined in the
// current cell.
func (s *State) DefaultCellTestArgs() (args []string) {
	args = append(args, "-test.v")
	if s.CellHasBenchmarks {
		args = append(args, "-test.bench=.")
	}
	if len(s.CellTests) > 0 {
		parts := make([]string, 0, len(s.CellTests))
		for _, testName := range s.CellTests {
			parts = append(parts, fmt.Sprintf("^%s$", testName))
		}
		args = append(args, "-test.run="+strings.Join(parts, "|"))
	}
	klog.V(2).Infof("DefaultCellTestArgs: %v", args)
	return
}

var regexpAllSpaces = regexp.MustCompile(`^\s*$`)

// IsEmptyLines returns true is all lines are marked to skip, or if all lines not marked as skip are empty.
func IsEmptyLines(lines []string, skipLines Set[int]) bool {
	if len(skipLines) >= len(lines) {
		return true
	}
	for ii, line := range lines {
		if skipLines.Has(ii) {
			continue
		}
		if len(line) == 0 || regexpAllSpaces.MatchString(line) {
			continue
		}
		return false
	}
	return true
}

// GonbCommentPrefix allows one to enter the special commands (`%%`, `!`) prefixed as a Go comment, so
// it doesn't conflict with Go IDEs.
// Particularly useful if using Jupytext.
const GonbCommentPrefix = "//gonb:"

// TrimGonbCommentPrefix removes a prefixing "//gonb:" (GonbCommentPrefix) from line, if there is such a prefix.
// This is optionally used to escape special commands.
func TrimGonbCommentPrefix(line string) string {
	if strings.HasPrefix(line, GonbCommentPrefix) {
		line = line[len(GonbCommentPrefix):]
	}
	return line
}
