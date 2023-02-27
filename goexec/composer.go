package goexec

import (
	"fmt"
	"github.com/janpfeifer/gonb/kernel"
	"github.com/pkg/errors"
	"io"
	"log"
	"os"
	"strings"
)

// This file holds the various functions used to compose the go sampleCellCode that
// will be compiled, from the parsed cells.

func (s *State) writeLinesToFile(filePath string, lines <-chan string) (err error) {
	var f *os.File
	f, err = os.Create(filePath)
	if err != nil {
		return errors.Wrapf(err, "creating %q", filePath)
	}
	defer func() {
		newErr := f.Close()
		if newErr != nil && err == nil {
			err = errors.Wrapf(newErr, "closing %q", filePath)
		}
	}()
	for line := range lines {
		if err != nil {
			// If there was an error keep on reading to the end of channel, discarding the input.
			continue
		}
		_, err = fmt.Fprintf(f, "%s\n", line)
		if err != nil {
			err = errors.Wrapf(err, "writing to %q", filePath)
		}
	}
	return err
}

// createGoFileFromLines implements CreateMainGo with no extra functionality (like auto-import).
func (s *State) createGoFileFromLines(filePath string, lines []string, skipLines map[int]bool, cursorInCell Cursor) (cursorInFile Cursor, err error) {
	linesChan := make(chan string, 1)

	cursorInFile = cursorInCell
	lineInFile := 0
	go func() {
		defer close(linesChan)
		// addLine checks for the new cursorInFile position.
		addLine := func(line string, lineInCell int, deltaColumn int) {
			linesChan <- line
			lineInFile++

			if !cursorInCell.HasCursor() || lineInCell == NoCursorLine {
				return
			}
			if lineInCell == cursorInCell.Line {
				cursorInFile.Line = lineInFile - 1 // -1 because we already incremented lineInFile above.
				cursorInFile.Col = cursorInCell.Col + deltaColumn
				//var modLine string
				//if cursorInFile.Col < int32(len(line)) {
				//	modLine = line[:cursorInFile.Col] + "*" + line[cursorInFile.Col:]
				//} else {
				//	modLine = line + "*"
				//}
				//log.Printf("Cursor in parse file %+v (cell line %d): %s", cursorInFile, lineInCell, modLine)
			}
		}
		addEmptyLine := func() {
			addLine("", NoCursorLine, 0)
		}

		// Insert package.
		addLine("package main", NoCursorLine, 0)
		addEmptyLine()

		var createdFuncMain bool
		for ii, line := range lines {
			line = strings.TrimRight(line, " ")
			if line == "%main" || line == "%%" {
				addEmptyLine()
				addLine("func main() {", NoCursorLine, 0)
				addLine("\tflag.Parse()", NoCursorLine, 0)
				createdFuncMain = true
				continue
			}
			if skipLines[ii] {
				continue
			}
			if createdFuncMain {
				// Indent following lines.
				line = "\t" + line
				addLine(line, ii, 1)
			} else {
				addLine(line, ii, 0)
			}
		}
		if createdFuncMain {
			addLine("}", NoCursorLine, 0)
		}
	}()

	// Pipe linesChan to main.go file.
	err = s.writeLinesToFile(filePath, linesChan)

	// Check for any error only at the end.
	if err != nil {
		return NoCursor, err
	}
	return
}

func (s *State) createMainFileFromDecls(decls *Declarations, mainDecl *Function) (cursor Cursor, err error) {
	var f *os.File
	f, err = os.Create(s.MainPath())
	if err != nil {
		return
	}
	cursor, err = s.createMainContentsFromDecls(f, decls, mainDecl)
	err2 := f.Close()
	if err != nil {
		err = errors.Wrapf(err, "creating main.go")
		return
	}
	err = err2
	if err != nil {
		err = errors.Wrapf(err, "closing main.go")
		return
	}
	return
}

func (s *State) createMainContentsFromDecls(writer io.Writer, decls *Declarations, mainDecl *Function) (cursor Cursor, err error) {
	cursor = NoCursor
	w := NewWriterWithCursor(writer)
	w.Format("package main\n\n")
	if err != nil {
		return
	}

	mergeCursorAndReportError := func(w *WriterWithCursor, cursorInFile Cursor, name string) bool {
		if w.Error() != nil {
			err = errors.WithMessagef(err, "in block %q", name)
			return true
		}
		if cursorInFile.HasCursor() {
			cursor = cursorInFile
		}
		return false
	}
	if mergeCursorAndReportError(w, decls.RenderImports(w), "imports") {
		return
	}
	if mergeCursorAndReportError(w, decls.RenderTypes(w), "types") {
		return
	}
	if mergeCursorAndReportError(w, decls.RenderConstants(w), "constants") {
		return
	}
	if mergeCursorAndReportError(w, decls.RenderVariables(w), "variables") {
		return
	}
	if mergeCursorAndReportError(w, decls.RenderFunctions(w), "functions") {
		return
	}

	if mainDecl != nil {
		w.Format("\n")
		if mainDecl.HasCursor() {
			cursor = mainDecl.Cursor
			cursor.Line += w.Line
			//log.Printf("Cursor in \"main\": %v", cursor)
		}
		w.Format("%s\n", mainDecl.Definition)
	}
	return
}

var (
	ParseError = fmt.Errorf("failed to parse cell contents")
	CursorLost = fmt.Errorf("cursor position not rendered in main.go")
)

// parseLinesAndComposeMain parses the cell (given in lines and skipLines), merges with
// currently memorized declarations (from previous Cell runs) and compose a `main.go`.
//
// On return the `main.go` file has been updated, and it returns the updated merged
// declarations, the cursor position adjusted into the newly generate `main.go` file.
//
// If cursorInCell defines a cursor (it can be set to NoCursor), but the cursor position
// is not rendered in the resulting `main.go`, a CursorLost error is returned.
func (s *State) parseLinesAndComposeMain(msg kernel.Message, lines []string, skipLines map[int]bool, cursorInCell Cursor) (
	updatedDecls *Declarations, cursorInFile Cursor, err error) {
	if cursorInCell.HasCursor() {
		log.Printf("Cursor in cell (%+v)", cursorInCell)
	}
	var cursorInTmpFile Cursor
	cursorInTmpFile, err = s.createGoFileFromLines(s.MainPath(), lines, skipLines, cursorInCell)
	if err != nil {
		return nil, NoCursor, errors.WithMessagef(err, "in goexec.parseLinesAndComposeMain()")
	}
	newDecls := NewDeclarations()
	if err = s.ParseFromMainGo(msg, cursorInTmpFile, newDecls); err != nil {
		// If cell is in an un-parseable state, just returns empty context. User can try to
		// run cell to get an error.
		return nil, NoCursor, errors.WithStack(ParseError)
	}

	// Checks whether there is a "main" function defined in the sampleCellCode.
	mainDecl, hasMain := newDecls.Functions["main"]
	if hasMain {
		// Remove "main" from newDecls: this should not be stored from one cell execution from
		// another.
		delete(newDecls.Functions, "main")
	} else {
		// Declare a stub main function, just so we can try to compile the final sampleCellCode.
		mainDecl = &Function{Key: "main", Name: "main", Definition: "func main() { flag.Parse() }"}
	}
	_ = mainDecl

	// Merge cell declarations with a copy of the current state: we don't want to commit the new
	// declarations until they compile successfully.
	updatedDecls = s.Decls.Copy()
	updatedDecls.ClearCursor()
	updatedDecls.MergeFrom(newDecls)

	// Render declarations to main.go.
	cursorInFile, err = s.createMainFileFromDecls(updatedDecls, mainDecl)
	if err != nil {
		return nil, NoCursor, errors.WithMessagef(err, "in goexec.InspectIdentifierInCell() while generating main.go with all declarations")
	}
	if cursorInCell.HasCursor() && !cursorInFile.HasCursor() {
		// Returns empty data, which returns a "not found".
		return nil, NoCursor, errors.WithStack(CursorLost)
	}
	if cursorInCell.HasCursor() {
		s.logCursor(cursorInFile)
	}
	return updatedDecls, cursorInFile, nil
}

const cursorStr = "‸"

// logCursor will log the line in `main.go` the cursor is pointing to, and puts a
// "*" where the
func (s *State) logCursor(cursor Cursor) {
	content, err := s.readMainGo()
	if err != nil {
		log.Printf("Failed to read main.go, for debugging.")
	} else {
		log.Printf("Cursor in main.go (%+v): %s", cursor, lineWithCursor(content, cursor))
	}
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
