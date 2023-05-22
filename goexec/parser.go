package goexec

import (
	"fmt"
	. "github.com/janpfeifer/gonb/common"
	"github.com/janpfeifer/gonb/kernel"
	"github.com/pkg/errors"
	"go/ast"
	"go/parser"
	"go/token"
	"k8s.io/klog/v2"
	"math/rand"
	"os"
	"path"
	"strconv"
	"strings"
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
	// cell. This is used when reporting back errors with a file number. Values of -1 (NoCursorLine) are injected lines
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
		// Notice that parser lines are 1-based, we keep them 0-based in the cursor.
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

// parseFromMainGo reads main.go and parses its declarations -- see object Declarations.
//
// This will be called by parseLinesAndComposeMain, after `main.go` is written.
//
// Parameters:
//   - msg: connection to notebook, to report errors. If nil, errors are not reported.
//   - cellId: execution id of the cell being processed. Set to -1 if later this cell will be discarded (for
//     instance when parsing for auto-complete).
//   - cursor: where it is in the file. If set (that is, `cursor != NoCursor`), it will record the position
//     of the cursor in the corresponding declaration.
//   - fileToCellLine: for each line in the `main.go` file, the corresponding line number in the cell. This
//     is used when reporting back errors with a file number. Values of -1 (NoCursorLine) are injected lines
//     that have no correspondent value in the cell code. It can be nil if there is no information mapping
//     file lines to cell lines.
func (s *State) parseFromMainGo(msg kernel.Message, cellId int, cursor Cursor, fileToCellIdAndLine []CellIdAndLine) (decls *Declarations, err error) {
	decls = NewDeclarations()
	pi := &parseInfo{
		cursor:  cursor,
		fileSet: token.NewFileSet(),

		cellId:              cellId,
		fileToCellIdAndLine: fileToCellIdAndLine,
	}
	var packages map[string]*ast.Package
	packages, err = parser.ParseDir(pi.fileSet, s.TempDir, nil, parser.SkipObjectResolution) // |parser.AllErrors
	if err != nil {
		if msg != nil {
			s.DisplayErrorWithContext(msg, fileToCellIdAndLine, err.Error())
		}
		err = errors.Wrapf(err, "parsing go files in TempDir %q", s.TempDir)
		return
	}
	pi.filesContents = make(map[string]string)

	// Debugging new types of parsing:
	//  fmt.Printf("Parsing results:\n")
	//  _ = ast.Print(fileSet, packages)

	for name, pkgAst := range packages {
		if name != "main" {
			err = errors.New("Invalid package %q declared: there should be no `package` declaration, " +
				"GoNB will automatically create `package main` when combining cell code.")
			return
		}
		for _, fileObj := range pkgAst.Files {
			// Currently, there is only `main.go` file.
			//fmt.Printf("File: %q\n", fileObj.Name.Name)
			filePath := path.Join(s.TempDir, fileObj.Name.Name) + ".go"
			content, err := os.ReadFile(filePath)
			if err != nil {
				return nil, errors.Wrapf(err, "Failed to read %q", fileObj.Name)
			}
			pi.filesContents[filePath] = string(content)
			// Incorporate Imports
			for _, entry := range fileObj.Imports {
				pi.ParseImportEntry(decls, entry)
			}

			// Enumerate various declarations.
			for _, decl := range fileObj.Decls {
				switch typedDecl := decl.(type) {
				case *ast.FuncDecl:
					pi.ParseFuncEntry(decls, typedDecl)
				case *ast.GenDecl:
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
						fmt.Printf("Dropped unknown generic declaration of type %s\n", typedDecl.Tok)
					}
				default:
					fmt.Printf("Dropped unknown declaration type\n")
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

// ParseImportEntry registers a new Import declaration based on the ast.ImportSpec. See State.parseFromMainGo
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

// ParseFuncEntry registers a new `func` declaration based on the ast.FuncDecl. See State.parseFromMainGo
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

// ParseVarEntry registers a new `var` declaration based on the ast.GenDecl. See State.parseFromMainGo
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
		// Each spec may be a list of variables (comma separated).
		for nameIdx, name := range vSpec.Names {
			v := &Variable{Name: name.Name, TypeDefinition: typeDefinition}
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
				v.Key = "_~" + strconv.Itoa(rand.Int()%0xFFFF)
			}
			decls.Variables[v.Key] = v
		}
	}
}

// ParseConstEntry registers a new `const` declaration based on the ast.GenDecl. See State.parseFromMainGo
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

// parseLinesAndComposeMain parses the cell (given in lines and skipLines), merges with
// memorized declarations in the State (presumably from previous Cell runs) and compose a `main.go`.
//
// On return the `main.go` file (in `s.TempDir`) has been updated, and it returns the updated merged
// declarations (`decls` is not changed) and optionally the cursor position adjusted into the newly generate
// `main.go` file.
//
// If cursorInCell defines a cursor (it can be set to NoCursor), but the cursor position
// is not rendered in the resulting `main.go`, a CursorLost error is returned.
//
// skipLines are lines that should not be considered as Go code. Typically, these are the special
// commands (like `%%`, `%args`, `%reset`, or bash lines starting with `!`).
func (s *State) parseLinesAndComposeMain(msg kernel.Message, cellId int, lines []string, skipLines Set[int], cursorInCell Cursor) (
	updatedDecls *Declarations, mainDecl *Function, cursorInFile Cursor, fileToCellIdAndLine []CellIdAndLine, err error) {
	cursorInFile = NoCursor

	var fileToCellLine []int
	cursorInFile, fileToCellLine, err = s.createGoFileFromLines(s.MainPath(), lines, skipLines, cursorInCell)
	if err != nil {
		return
	}
	fileToCellIdAndLine = MakeFileToCellIdAndLine(cellId, fileToCellLine)

	// Parse declarations in created `main.go` file.
	var newDecls *Declarations
	newDecls, err = s.parseFromMainGo(msg, cellId, cursorInFile, fileToCellIdAndLine)
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
		mainDecl = &Function{Key: "main", Name: "main", Definition: "func main() { flag.Parse() }"}
	}

	// Merge cell declarations with a copy of the current state: we don't want to commit the new
	// declarations until they compile successfully.
	updatedDecls = s.Decls.Copy()
	updatedDecls.ClearCursor()
	updatedDecls.MergeFrom(newDecls)

	// Render declarations to main.go.
	cursorInFile, fileToCellIdAndLine, err = s.createMainFileFromDecls(updatedDecls, mainDecl)
	if err != nil {
		err = errors.WithMessagef(err, "while composing main.go with all declarations")
		return
	}
	if cursorInCell.HasCursor() && !cursorInFile.HasCursor() {
		// Returns empty data, which returns a "not found".
		err = errors.WithStack(CursorLost)
		return
	}
	if cursorInCell.HasCursor() {
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
