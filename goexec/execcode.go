package goexec

import (
	. "github.com/janpfeifer/gonb/common"
	"github.com/janpfeifer/gonb/kernel"
	"github.com/pkg/errors"
	"os/exec"
	"path"
)

// ExecuteCell takes the contents of a cell, parses it, merges new declarations with the ones
// from previous definitions, render a final main.go code with the whole content,
// compiles and runs it.
//
// skipLines are lines that should not be considered as Go code. Typically, these are the special
// commands (like `%%`, `%args`, `%reset`, or bash lines starting with `!`).
func (s *State) ExecuteCell(msg kernel.Message, cellId int, lines []string, skipLines Set[int]) error {
	updatedDecls, _, err := s.parseLinesAndComposeMain(msg, cellId, lines, skipLines, NoCursor)
	if err != nil {
		return errors.WithMessagef(err, "in goexec.ExecuteCell()")
	}

	// Run `goimports` (or the code that implements it)
	if err = s.GoImports(msg); err != nil {
		return errors.WithMessagef(err, "goimports failed")
	}

	// And then compile it.
	if err := s.Compile(msg); err != nil {
		return err
	}

	// Compilation successful: save merged declarations into current State.
	s.Decls = updatedDecls

	// Execute compiled code.
	return s.Execute(msg)
}

func (s *State) BinaryPath() string {
	return path.Join(s.TempDir, s.Package)
}

func (s *State) MainPath() string {
	return path.Join(s.TempDir, "main.go")
}

func (s *State) Execute(msg kernel.Message) error {
	return kernel.PipeExecToJupyter(msg, "", s.BinaryPath(), s.Args...)
}

// Compile compiles the currently generate go files in State.TempDir to a binary named State.Package.
//
// If errors in compilation happen, linesPos is used to adjust line numbers to their content in the
// current cell.
func (s *State) Compile(msg kernel.Message) error {
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
func (s *State) GoImports(msg kernel.Message) error {
	goimportsPath, err := exec.LookPath("goimports")
	if err != nil {
		_ = kernel.PublishWriteStream(msg, kernel.StreamStderr, `
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
