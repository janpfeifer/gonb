package goexec

import (
	"github.com/janpfeifer/gonb/kernel"
	"github.com/pkg/errors"
	"log"
	"path"
)

// This file implements saving to a inspect.go file, and then using `gopls` to
// inspect a requested token.

// InspectPath returns the path of the file saved to be used for inspection (`inspect_request
// message from Jupyter).
func (s *State) InspectPath() string {
	return path.Join(s.TempDir, "inspect.go")
}

func (s *State) InspectCell(lines []string, skipLines map[int]bool, line, col int) (kernel.MIMEMap, error) {
	if skipLines[line] {
		// Only Go code can be inspected here.
		return nil, errors.Errorf("goexec.InspectCell() can only inspect Go code, line %d is a secial command line: %q", line, lines[line])
	}

	cursorInCell := Cursor{int32(line), int32(col)}
	cursorInTmpFile, err := s.createGoFileFromLines(s.MainPath(), lines, skipLines, cursorInCell)
	if err != nil {
		return nil, errors.WithMessagef(err, "in goexec.InspectCell()")
	}
	newDecls := NewDeclarations()
	if err = s.ParseImportsFromMainGo(nil, cursorInTmpFile, newDecls); err != nil {
		return nil, errors.WithMessagef(err, "in goexec.InspectCell() while parsing cell")
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
	_ = mainDecl

	// Merge cell declarations with a copy of the current state: we don't want to commit the new
	// declarations until they compile successfully.
	tmpDecls := s.Decls.Copy()
	tmpDecls.ClearCursor()
	tmpDecls.MergeFrom(newDecls)

	// Render declarations to main.go.
	var cursorInFile Cursor
	cursorInFile, err = s.createMainFromDecls(tmpDecls, mainDecl)
	if err != nil {
		return nil, errors.WithMessagef(err, "in goexec.InspectCell() while generating main.go with all declarations")
	}
	log.Printf("CursorInFile: %+v", cursorInFile)
	return nil, errors.Errorf("Unable to inspect: line %d, col %d", line, col)
}
