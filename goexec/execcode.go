package goexec

import (
	"fmt"
	"github.com/janpfeifer/gonb/kernel"
	"github.com/pkg/errors"
	"os"
	"os/exec"
	"path"
	"strings"
)

// PosInJupyter is the position on the original Jupyter cell.
type PosInJupyter struct {
	// Line in original Jupyter cell, or -1 if line is automatically generated.
	Line int
}

var autoLine = PosInJupyter{Line: -1}

// ExecuteCell takes the contents of a cell, parses it, merges new declarations with the ones
// from previous definitions, render a final main.go code with the whole content,
// compiles and runs it.
func (s *State) ExecuteCell(msg *kernel.Message, lines []string, skipLines map[int]bool) error {
	// Find declarations on unchanged cell contents.
	linesPos, err := s.createMainFromLines(lines, skipLines)
	if err != nil {
		return errors.WithMessagef(err, "in goexec.ExecuteCell()")
	}
	newDecls := NewDeclarations()
	if err = s.ParseImportsFromMainGo(msg, newDecls); err != nil {
		return errors.WithMessagef(err, "in goexec.ExecuteCell() while parsing cell")
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
	tmpDecls.MergeFrom(newDecls)

	// Render declarations to main.go.
	if err = s.createMainFromDecls(tmpDecls, mainDecl); err != nil {
		return errors.WithMessagef(err, "in goexec.ExecuteCell() while generating main.go with all declarations")
	}
	// Run goimports (or the code that implements it)
	if err = s.GoImports(msg); err != nil {
		return errors.WithMessagef(err, "goimports failed")
	}

	// And then compile it.
	if err := s.Compile(msg, linesPos); err != nil {
		return err
	}

	// Compilation successful: save merged declarations into current State.
	s.Decls = tmpDecls

	// Execute compiled code.
	return s.Execute(msg)
}

func (s *State) BinaryPath() string {
	return path.Join(s.TempDir, s.Package)
}

func (s *State) MainPath() string {
	return path.Join(s.TempDir, "main.go")
}

func (s *State) Execute(msg *kernel.Message) error {
	return msg.PipeExecToJupyter("", s.BinaryPath(), s.Args...)
}

// Compile compiles the currently generate go files in State.TempDir to a binary named State.Package.
//
// If errors in compilation happen, linesPos is used to adjust line numbers to their content in the
// current cell.
func (s *State) Compile(msg *kernel.Message, linesPos []PosInJupyter) error {
	_ = linesPos
	cmd := exec.Command("go", "build", "-o", s.BinaryPath())
	cmd.Dir = s.TempDir
	var output []byte
	output, err := cmd.CombinedOutput()
	if err != nil {
		s.DisplayErrorWithContext(msg, string(output))
		return errors.Wrapf(err, "failed to run %q", cmd.String())
	}
	return nil
}

// GoImports execute `goimports` which adds imports to non-declared imports automatically.
// It also runs "go get" to download any missing dependencies.
func (s *State) GoImports(msg *kernel.Message) error {
	goimportsPath, err := exec.LookPath("goimports")
	if err != nil {
		_ = msg.PublishWriteStream(kernel.StreamStderr, `
Program goimports is not installed. It is used to automatically import
missing standard packages, and is a standard Go toolkit package. You
can install it from the notebook with:

!go install golang.org/x/tools/cmd/goimports@latest

`)
		return errors.WithMessagef(err, "while trying to run goimports\n")
	}
	cmd := exec.Command(goimportsPath, "-w", s.MainPath())
	cmd.Dir = s.TempDir
	var output []byte
	output, err = cmd.CombinedOutput()
	if err != nil {
		s.DisplayErrorWithContext(msg, string(output)+"\n"+err.Error())
		return errors.Wrapf(err, "failed to run %q", cmd.String())
	}

	// Download missing dependencies.
	if !s.AutoGet {
		return nil
	}
	cmd = exec.Command("go", "get")
	cmd.Dir = s.TempDir
	output, err = cmd.CombinedOutput()
	if err != nil {
		s.DisplayErrorWithContext(msg, string(output)+"\n"+err.Error())
		return errors.Wrapf(err, "failed to run %q", cmd.String())
	}
	return nil
}

func (s *State) createMain(lines <-chan string) (err error) {
	var f *os.File
	f, err = os.Create(s.MainPath())
	if err != nil {
		return errors.Wrapf(err, "creating main.go")
	}
	defer func() {
		newErr := f.Close()
		if newErr != nil && err == nil {
			err = errors.Wrapf(newErr, "closing main.go")
		}
	}()
	for line := range lines {
		if err != nil {
			// If there was an error keep on reading to the end of channel, discarding the input.
			continue
		}
		_, err = fmt.Fprintf(f, "%s\n", line)
		if err != nil {
			err = errors.Wrap(err, "writing to main.go")
		}
	}
	return err
}

// createMainFromLines implements CreateMainGo with no extra functionality (like auto-import).
func (s *State) createMainFromLines(lines []string, skipLines map[int]bool) (linesPos []PosInJupyter, err error) {
	linesChan := make(chan string, 1)

	go func() {
		defer close(linesChan)
		// Reserve space for original lines + package + func main().
		linesPos = make([]PosInJupyter, 0, 5+len(lines))
		addLine := func(line string, pos PosInJupyter) {
			linesChan <- line
			linesPos = append(linesPos, pos)
		}
		addEmptyLine := func() {
			addLine("", autoLine)
		}

		// Insert package.
		addLine("package main", autoLine)
		addEmptyLine()

		var createdFuncMain bool
		for ii, line := range lines {
			line = strings.TrimRight(line, " ")
			if line == "%main" || line == "%%" {
				addEmptyLine()
				addLine("func main() {", autoLine)
				addLine("\tflag.Parse()", autoLine)
				createdFuncMain = true
				continue
			}
			if skipLines[ii] {
				continue
			}
			if createdFuncMain {
				// Indent following lines.
				line = "\t" + line
			}
			addLine(line, PosInJupyter{Line: ii})
		}
		if createdFuncMain {
			addLine("}", autoLine)
		}
	}()

	// Pipe linesChan to main.go file.
	err = s.createMain(linesChan)

	// Check for any error only at the end.
	if err != nil {
		return nil, err
	}
	return
}

func (s *State) createMainFromDecls(decls *Declarations, mainDecl *Function) (err error) {
	var f *os.File
	f, err = os.Create(s.MainPath())
	if err != nil {
		return
	}
	defer func() {
		if err != nil {
			err = errors.Wrapf(err, "creating main.go")
			return
		}
		err = f.Close()
		if err != nil {
			err = errors.Wrapf(err, "closing main.go")
		}
	}()

	_, err = fmt.Fprintf(f, "package main\n\n")
	if err != nil {
		return
	}
	if err = decls.RenderImports(f); err != nil {
		return
	}
	if err = decls.RenderTypes(f); err != nil {
		return
	}
	if err = decls.RenderConstants(f); err != nil {
		return
	}
	if err = decls.RenderVariables(f); err != nil {
		return
	}
	if err = decls.RenderFunctions(f); err != nil {
		return
	}
	if _, err = fmt.Fprintf(f, "\n%s\n", mainDecl.Definition); err != nil {
		return
	}
	return
}
