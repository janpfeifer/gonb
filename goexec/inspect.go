package goexec

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/janpfeifer/gonb/gonbui/protocol"
	"github.com/janpfeifer/gonb/kernel"
	"github.com/pkg/errors"
	"log"
	"os/exec"
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
		// If cell is in an un-parseable state, just returns empty context. User can try to
		// run cell to get an error.
		return make(kernel.MIMEMap), nil
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
	if !cursorInFile.HasCursor() {
		// Returns empty data, which returns a "not found".
		return make(kernel.MIMEMap), nil
	}
	log.Printf("CursorInFile: %+v", cursorInFile)

	// Execute `gopls` with the given path.
	var jsonData map[string]any
	jsonData, err = goplsQuery(s.TempDir, "definition", s.MainPath(), cursorInFile)
	if err != nil {
		log.Printf("Failed to find definition with `gopls` for symbol under cursor: %v", err)
		// If gopls fails, just returns empty data, which returns a "not found".
		return make(kernel.MIMEMap), nil
	}
	descAny, found := jsonData["description"]
	if !found {
		log.Printf("gopls without description, returned %q", jsonData)
		// Returns empty data, which returns a "not found".
		return make(kernel.MIMEMap), nil
	}
	desc, ok := descAny.(string)
	if !ok {
		log.Printf("gopls description not a string: %q", desc)
		// Returns empty data, which returns a "not found".
		return make(kernel.MIMEMap), nil
	}

	// Return MIMEMap with markdown.
	return kernel.MIMEMap{protocol.MIMETextMarkdown: desc}, nil
}

// goplsQuery invokes gopls to find the definition of a function.
// TODO: run gopls as a service, as opposed to invoking it every time.
func goplsQuery(dir, command, filePath string, cursor Cursor) (map[string]any, error) {
	goplsPath, err := exec.LookPath("gopls")
	if err != nil {
		msg := `
Program gopls is not installed. It is used to inspect into code
and provide contextual information and autocompletion. It is a 
standard Go toolkit package. You can install it from the notebook
with:

` + "```" + `
!go install golang.org/x/tools/gopls@latest
` + "```\n"
		return map[string]any{"description": any(msg)}, nil
	}
	log.Printf("gopls path=%q", goplsPath)
	location := fmt.Sprintf("%s:%d:%d", filePath, cursor.Line+1, cursor.Col+1)
	cmd := exec.Command(goplsPath, command, "-json", "-markdown", location)
	cmd.Dir = dir
	var output []byte
	output, err = cmd.CombinedOutput()
	if err != nil {
		return nil, errors.Wrapf(err, "failed to run %q: %q", cmd.String(), output)
	}

	jsonData := make(map[string]any)
	dec := json.NewDecoder(bytes.NewReader(output))
	err = dec.Decode(&jsonData)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to decode json from gopls output: %q", output)
	}
	return jsonData, nil
}
