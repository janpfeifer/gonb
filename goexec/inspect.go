package goexec

import (
	"github.com/janpfeifer/gonb/kernel"
	"path"
)

// This file implements saving to a inspect.go file, and then using `gopls` to
// inspect a requested token.

// InspectPath returns the path of the file saved to be used for inspection (`inspect_request
// message from Jupyter).
func (s *State) InspectPath() string {
	return path.Join(s.TempDir, "inspect.go")
}

func (s *State) InspectCell(msg kernel.Message, lines []string, usedLines map[int]bool, line, col int) error {
	return nil
}
