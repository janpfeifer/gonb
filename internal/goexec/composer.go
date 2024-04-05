package goexec

import (
	"fmt"
	. "github.com/janpfeifer/gonb/common"
	"github.com/pkg/errors"
	"io"
	"k8s.io/klog/v2"
	"os"
	"sort"
	"strings"
)

// This file holds the various functions used to compose and render the go code that
// will be compiled, from the parsed cells.

// CellIdAndLine points to a line within a cell. Id is the execution number of the cell,
// as given to ExecuteCell.
type CellIdAndLine struct {
	Id, Line int
}

// MakeFileToCellIdAndLine converts a cellId slice of cell line numbers for a file to a slice of CellIdAndLine.
func MakeFileToCellIdAndLine(cellId int, fileToCellLine []int) (fileToCellIdAndLine []CellIdAndLine) {
	fileToCellIdAndLine = make([]CellIdAndLine, len(fileToCellLine))
	for ii, line := range fileToCellLine {
		fileToCellIdAndLine[ii] = CellIdAndLine{cellId, line}
	}
	return
}

// WriterWithCursor keep tabs of the current line/col of the file (presumably)
// being written.
type WriterWithCursor struct {
	w         io.Writer
	err       error // If err != nil, nothing is written anymore.
	Line, Col int
}

// NewWriterWithCursor that keeps tabs of current line/col of the file (presumably)
// being written.
func NewWriterWithCursor(w io.Writer) *WriterWithCursor {
	return &WriterWithCursor{w: w}
}

// Cursor returns the current position in the file, at the end of what has been written so far.
func (w *WriterWithCursor) Cursor() Cursor {
	return Cursor{Line: w.Line, Col: w.Col}
}

// CursorPlusDelta returns the expected cursor position in the current file, assuming the original cursor
// is cursorDelta away from the current position in the file (stored in w).
//
// Semantically it's equivalent to `w.Cursor() + cursorDelta`.
func (w *WriterWithCursor) CursorPlusDelta(delta Cursor) (fileCursor Cursor) {
	fileCursor = w.Cursor()
	fileCursor.Line += delta.Line
	if delta.Line > 0 {
		fileCursor.Col = delta.Col
	} else {
		fileCursor.Col += delta.Col
	}
	return fileCursor
}

// FillLinesGap append NoCursorLine (-1) line indices to fileToCellIdAndLine slice, up to
// the current line.
func (w *WriterWithCursor) FillLinesGap(fileToCellIdAndLine []CellIdAndLine) []CellIdAndLine {
	for len(fileToCellIdAndLine) < w.Line {
		fileToCellIdAndLine = append(fileToCellIdAndLine, CellIdAndLine{NoCursorLine, NoCursorLine})
	}
	return fileToCellIdAndLine
}

// Error returns first err that happened during writing.
func (w *WriterWithCursor) Error() error { return w.err }

// Writef write with formatted text. Errors can be retrieved with Error.
func (w *WriterWithCursor) Writef(format string, args ...any) {
	if w.err != nil {
		return
	}
	text := fmt.Sprintf(format, args...)
	w.Write(text)
}

// Write writes the given content and keeps track of cursor. Errors can be retrieved with Error.
func (w *WriterWithCursor) Write(content string) {
	if w.err != nil {
		return
	}
	var n int
	n, w.err = w.w.Write([]byte(content))
	if n != len(content) {
		w.err = errors.Errorf("failed to write %q, %d bytes: wrote only %d", content, len(content), n)
	}
	if w.err != nil {
		return
	}

	// Update cursor position.
	parts := strings.Split(content, "\n")
	if len(parts) == 1 {
		w.Col += len(parts[0])
	} else {
		w.Line += len(parts) - 1
		w.Col = len(parts[len(parts)-1])
	}
}

// RenderImports writes out `import ( ... )` for all imports in Declarations.
func (d *Declarations) RenderImports(w *WriterWithCursor, fileToCellIdAndLine []CellIdAndLine) (Cursor, []CellIdAndLine) {
	cursor := NoCursor
	if len(d.Imports) == 0 {
		return cursor, fileToCellIdAndLine
	}

	w.Write("import (\n")
	for _, key := range SortedKeys(d.Imports) {
		importDecl := d.Imports[key]
		fileToCellIdAndLine = w.FillLinesGap(fileToCellIdAndLine)
		fileToCellIdAndLine = importDecl.CellLines.Append(fileToCellIdAndLine)
		w.Write("\t")
		if importDecl.Alias != "" {
			if importDecl.CursorInAlias {
				cursor = w.CursorPlusDelta(importDecl.Cursor)
			}
			w.Writef("%s ", importDecl.Alias)
		}
		if importDecl.CursorInPath {
			cursor = w.CursorPlusDelta(importDecl.Cursor)
		}
		w.Writef("%q\n", importDecl.Path)
	}
	w.Write(")\n\n")
	return cursor, fileToCellIdAndLine
}

// RenderVariables writes out `var ( ... )` for all variables in Declarations.
func (d *Declarations) RenderVariables(w *WriterWithCursor, fileToCellIdAndLine []CellIdAndLine) (Cursor, []CellIdAndLine) {
	cursor := NoCursor
	if len(d.Variables) == 0 {
		return cursor, fileToCellIdAndLine
	}

	w.Write("var (\n")
	for _, key := range SortedKeys(d.Variables) {
		varDecl := d.Variables[key]
		w.Write("\t")
		fileToCellIdAndLine = w.FillLinesGap(fileToCellIdAndLine)
		fileToCellIdAndLine = varDecl.CellLines.Append(fileToCellIdAndLine)
		if varDecl.CursorInName {
			cursor = w.CursorPlusDelta(varDecl.Cursor)
		}
		w.Write(varDecl.Name)
		if varDecl.TypeDefinition != "" {
			w.Write(" ")
			if varDecl.CursorInType {
				cursor = w.CursorPlusDelta(varDecl.Cursor)
			}
			w.Write(varDecl.TypeDefinition)
		}
		if varDecl.ValueDefinition != "" {
			w.Write(" = ")
			if varDecl.CursorInValue {
				cursor = w.CursorPlusDelta(varDecl.Cursor)
			}
			w.Write(varDecl.ValueDefinition)
		}
		w.Write("\n")
	}
	w.Write(")\n\n")
	return cursor, fileToCellIdAndLine
}

// RenderFunctions without comments, for all functions in Declarations.
func (d *Declarations) RenderFunctions(w *WriterWithCursor, fileToCellIdAndLine []CellIdAndLine) (Cursor, []CellIdAndLine) {
	cursor := NoCursor
	if len(d.Functions) == 0 {
		return cursor, fileToCellIdAndLine
	}

	for _, key := range SortedKeys(d.Functions) {
		funcDecl := d.Functions[key]
		fileToCellIdAndLine = w.FillLinesGap(fileToCellIdAndLine)
		fileToCellIdAndLine = funcDecl.CellLines.Append(fileToCellIdAndLine)
		def := funcDecl.Definition
		if funcDecl.HasCursor() {
			cursor = w.CursorPlusDelta(funcDecl.Cursor)
		}
		if strings.HasPrefix(key, InitFunctionPrefix) {
			def = strings.Replace(def, key, "init", 1)
		}
		w.Writef("%s\n\n", def)
	}
	return cursor, fileToCellIdAndLine
}

// RenderTypes without comments.
func (d *Declarations) RenderTypes(w *WriterWithCursor, fileToCellIdAndLine []CellIdAndLine) (Cursor, []CellIdAndLine) {
	cursor := NoCursor
	if len(d.Types) == 0 {
		return cursor, fileToCellIdAndLine
	}

	for _, key := range SortedKeys(d.Types) {
		typeDecl := d.Types[key]
		fileToCellIdAndLine = w.FillLinesGap(fileToCellIdAndLine)
		fileToCellIdAndLine = typeDecl.CellLines.Append(fileToCellIdAndLine)
		w.Write("type ")
		if typeDecl.CursorInType {
			cursor = w.CursorPlusDelta(typeDecl.Cursor)
		}
		w.Writef("%s\n", typeDecl.TypeDefinition)
	}
	w.Write("\n")
	return cursor, fileToCellIdAndLine
}

// RenderConstants without comments for all constants in Declarations.
//
// Constants are trickier to render because when they are defined in a block,
// using `iota`, their ordering matters. So we re-render them in the same order
// and blocks as they were originally parsed.
//
// The ordering is given by the sort order of the first element of each `const` block.
func (d *Declarations) RenderConstants(w *WriterWithCursor, fileToCellIdAndLine []CellIdAndLine) (Cursor, []CellIdAndLine) {
	cursor := NoCursor
	if len(d.Constants) == 0 {
		return cursor, fileToCellIdAndLine
	}

	// Enumerate heads of const blocks.
	headKeys := make([]string, 0, len(d.Constants))
	for key, constDecl := range d.Constants {
		if constDecl.Prev == nil {
			// Head of the const block.
			headKeys = append(headKeys, key)
		}
	}
	sort.Strings(headKeys)

	for _, headKey := range headKeys {
		constDecl := d.Constants[headKey]
		if constDecl.Next == nil {
			// Render individual const declaration.
			w.Write("const ")
			fileToCellIdAndLine = constDecl.Render(w, &cursor, fileToCellIdAndLine)
			w.Write("\n\n")
			continue
		}
		// Render block of constants.
		w.Write("const (\n")
		for constDecl != nil {
			w.Write("\t")
			fileToCellIdAndLine = constDecl.Render(w, &cursor, fileToCellIdAndLine)
			w.Write("\n")
			constDecl = constDecl.Next
		}
		w.Write(")\n\n")
	}
	return cursor, fileToCellIdAndLine
}

// Render Constant declaration (without the `const` keyword).
func (c *Constant) Render(w *WriterWithCursor, cursor *Cursor, fileToCellIdAndLine []CellIdAndLine) []CellIdAndLine {
	fileToCellIdAndLine = w.FillLinesGap(fileToCellIdAndLine)
	fileToCellIdAndLine = c.CellLines.Append(fileToCellIdAndLine)
	if c.CursorInKey {
		*cursor = w.CursorPlusDelta(c.Cursor)
	}
	w.Write(c.Key)
	if c.TypeDefinition != "" {
		w.Write(" ")
		if c.CursorInType {
			*cursor = w.CursorPlusDelta(c.Cursor)
		}
		w.Write(c.TypeDefinition)
	}
	if c.ValueDefinition != "" {
		w.Write(" = ")
		if c.CursorInValue {
			*cursor = w.CursorPlusDelta(c.Cursor)
		}
		w.Write(c.ValueDefinition)
	}
	return fileToCellIdAndLine
}

// createGoFileFromLines creates a Go file from the cell contents.
// It doesn't yet include previous declarations.
//
// Among the things it handles:
//   - Adding an initial `package main` line.
//   - Handle the special `%%` line, a shortcut to create a `func main()`.
//
// Parameters:
//   - filePath is the path where to write the Go code.
//   - cellId is the id of the cell being executed.
//   - Lines are the Lines in the cell.
//   - skipLines are Lines in the cell that are not Go code: Lines starting with "!" or "%" special characters.
//   - cursorInCell optionally specifies the cursor position in the cell. It can be set to NoCursor.
//
// Return:
//   - cursorInFile: the equivalent cursor position in the final file, considering the given cursorInCell.
//   - fileToCellLines: a map from the file Lines to original cell Lines. It is set to NoCursorLine (-1) for Lines
//     that don't have an equivalent in the cell (e.g: the `package main` line that inserted here).
func (s *State) createGoFileFromLines(filePath string, cellId int, lines []string, skipLines Set[int], cursorInCell Cursor) (
	cursorInFile Cursor, fileToCellLines []int, err error) {
	cursorInFile = NoCursor

	// Maximum number of extra Lines created is 5, so we create a map with that amount of line. Later we trim it
	// to the correct number.
	fileToCellLines = make([]int, len(lines)+5)
	for ii := 0; ii < len(fileToCellLines); ii++ {
		fileToCellLines[ii] = NoCursorLine
	}

	var f *os.File
	f, err = os.Create(filePath)
	if err != nil {
		err = errors.Wrapf(err, "Failed to create %q", filePath)
		return
	}
	w := NewWriterWithCursor(f)
	defer func() {
		if f != nil {
			closeErr := f.Close()
			if closeErr != nil {
				klog.Errorf("Failed to close main.go when generating it: %v", closeErr)
			}
		}
	}()

	w.Write("package main\n\n")
	var createdFuncMain bool
	isFirstLine := true
	for ii, line := range lines {
		if strings.HasPrefix(line, "%main") || strings.HasPrefix(line, "%%") {
			// Write preamble of func main() and associate to the "%%" line:
			fileToCellLines[w.Line] = ii
			fileToCellLines[w.Line+1] = ii
			w.Write("func main() {\n\tflag.Parse()\n")
			createdFuncMain = true
			isFirstLine = false
			continue
		}
		if _, found := skipLines[ii]; found {
			continue
		}
		if ii == cursorInCell.Line {
			// Use current line for cursor, but add column.
			cursorInFile = w.CursorPlusDelta(Cursor{Col: cursorInCell.Col})
		}
		if isFirstLine && strings.HasPrefix(line, "package") {
			err = errors.Errorf("Please don't set a `package` in any of your cells: GoNB will set a `package main` automatically for you when compiling your cells. Cell #%d Line %d: %q",
				cellId, ii+1, line)
			return
		}
		fileToCellLines[w.Line] = ii // Registers line mapping.
		w.Write(line)
		w.Write("\n")
		isFirstLine = false
	}
	if createdFuncMain {
		w.Write("\n}\n")
	}
	if w.Error() != nil {
		err = w.Error()
		return
	}
	fileToCellLines = fileToCellLines[:w.Line] // Truncate to Lines actually used.

	// Close file.
	err = f.Close()
	if err != nil {
		err = errors.Wrapf(err, "Failed to close %q", filePath)
		return
	}
	f = nil
	return
}

// createCodeFileFromDecls creates `main.go` (or `main_test.go`) and writes all declarations.
//
// It returns the cursor position in the file as well as a mapping from the file Lines to the original cell ids and Lines.
func (s *State) createCodeFileFromDecls(decls *Declarations, mainDecl *Function) (
	cursor Cursor, fileToCellIdAndLine []CellIdAndLine, err error) {
	if err = s.RemoveCode(); err != nil {
		return
	}
	var f *os.File
	f, err = os.Create(s.CodePath())
	if err != nil {
		err = errors.Wrapf(err, "Failed to create %q", s.CodePath())
		return
	}
	cursor, fileToCellIdAndLine, err = s.createCodeFromDecls(f, decls, mainDecl)
	err2 := f.Close()
	if err != nil {
		err = errors.Wrapf(err, "creating %q", s.CodePath())
		return
	}
	err = err2
	if err != nil {
		err = errors.Wrapf(err, "closing %q", s.CodePath())
		return
	}
	return
}

// createAlternativeFileFromDecls creates `other.go` and writes all memorized definitions.
func (s *State) createAlternativeFileFromDecls(decls *Declarations) (err error) {
	var f *os.File
	fPath := s.AlternativeDefinitionsPath()
	f, err = os.Create(fPath)
	if err != nil {
		err = errors.Wrapf(err, "Failed to create %q", fPath)
		return
	}
	_, _, err = s.createCodeFromDecls(f, decls, nil)
	err2 := f.Close()
	if err != nil {
		err = errors.Wrapf(err, "creating %q", fPath)
		return
	}
	err = err2
	if err != nil {
		err = errors.Wrapf(err, "closing %q", fPath)
		return
	}
	return
}

// createCodeFromDecls writes to the given file all the declarations.
//
// mainDecl is optional, and if not given, no `main` function is created.
//
// It returns the cursor position in the file as well as a mapping from the file Lines to the original cell ids and Lines.
func (s *State) createCodeFromDecls(writer io.Writer, decls *Declarations, mainDecl *Function) (cursor Cursor, fileToCellIdAndLine []CellIdAndLine, err error) {
	cursor = NoCursor
	w := NewWriterWithCursor(writer)
	w.Writef("package main\n\n")
	if err != nil {
		return
	}

	mergeCursorAndReportError := func(w *WriterWithCursor, renderer func(w *WriterWithCursor, fileToCellIdAndLine []CellIdAndLine) (Cursor, []CellIdAndLine), name string) bool {
		var cursorInFile Cursor
		cursorInFile, fileToCellIdAndLine = renderer(w, fileToCellIdAndLine)
		if w.Error() != nil {
			err = errors.WithMessagef(err, "in block %q", name)
			return true
		}
		if cursorInFile.HasCursor() {
			cursor = cursorInFile
		}
		return false
	}

	if mergeCursorAndReportError(w, decls.RenderImports, "imports") {
		return
	}
	if mergeCursorAndReportError(w, decls.RenderTypes, "types") {
		return
	}
	if mergeCursorAndReportError(w, decls.RenderConstants, "constants") {
		return
	}
	if mergeCursorAndReportError(w, decls.RenderVariables, "variables") {
		return
	}
	if mergeCursorAndReportError(w, decls.RenderFunctions, "functions") {
		return
	}

	if mainDecl != nil {
		w.Writef("\n")
		if mainDecl.HasCursor() {
			cursor = w.CursorPlusDelta(mainDecl.Cursor)
		}
		fileToCellIdAndLine = w.FillLinesGap(fileToCellIdAndLine)
		fileToCellIdAndLine = mainDecl.CellLines.Append(fileToCellIdAndLine)
		w.Writef("%s\n", mainDecl.Definition)
	}
	return
}

var (
	ParseError = fmt.Errorf("failed to parse cell contents")
	CursorLost = fmt.Errorf("cursor position not rendered in main.go")
)
