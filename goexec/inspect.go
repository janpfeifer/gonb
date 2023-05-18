package goexec

import (
	"context"
	"github.com/golang/glog"
	"github.com/janpfeifer/gonb/gonbui/protocol"
	"github.com/janpfeifer/gonb/kernel"
	"github.com/pkg/errors"
	"os"
	"strings"
	"unicode/utf16"
)

// This file implements inspecting an identifier in a Cell and auto-complete functionalities.

// InspectIdentifierInCell implements an `inspect_request` from Jupyter, using `gopls`.
// It updates `main.go` with the cell contents (given as lines)
func (s *State) InspectIdentifierInCell(lines []string, skipLines map[int]struct{}, cursorLine, cursorCol int) (mimeMap kernel.MIMEMap, err error) {
	if s.gopls == nil {
		// gopls not installed.
		return make(kernel.MIMEMap), nil
	}
	if _, found := skipLines[cursorLine]; found {
		// Only Go code can be inspected here.
		err = errors.Errorf("goexec.InspectIdentifierInCell() can only inspect Go code, line %d is a secial command line: %q", cursorLine, lines[cursorLine])
		return
	}

	glog.V(2).Infof("InspectIdentifierInCell: ")
	// Generate `main.go` with contents of current cell.
	cursorInCell := Cursor{cursorLine, cursorCol}
	cellId := -1 // Inspect doesn't actually execute it, so parsed contents of cell are not kept.
	updatedDecls, mainDecl, cursorInFile, fileToCellIdAndLine, err := s.parseLinesAndComposeMain(nil, cellId, lines, skipLines, cursorInCell)
	if err != nil {
		if errors.Is(err, ParseError) || errors.Is(err, CursorLost) {
			mimeMap = make(kernel.MIMEMap)
			err = nil
			return
		}
	}

	// Exec `goimports`: we just want to make sure that "go get" is executed for the needed packages.
	cursorInFile, _, err = s.GoImports(nil, updatedDecls, mainDecl, fileToCellIdAndLine)
	if err != nil {
		err = errors.WithMessagef(err, "goimports failed")
		return
	}

	// Query `gopls`.
	ctx := context.Background()
	var desc string
	glog.V(2).Infof("InspectIdentifierInCell: gopls.Definition(ctx, %s, %d, %d)",
		s.MainPath(), cursorInFile.Line, cursorInFile.Col)
	desc, err = s.gopls.Definition(ctx, s.MainPath(), cursorInFile.Line, cursorInFile.Col)
	messages := s.gopls.ConsumeMessages()
	if err != nil {
		parts := []string{errors.Cause(err).Error()}
		if len(messages) > 0 {
			parts = append(parts, messages...)
		}
		return kernel.MIMEMap{protocol.MIMETextPlain: strings.Join(parts, "\n\n")}, nil
	}

	// Return MIMEMap with markdown.
	mimeMap = kernel.MIMEMap{protocol.MIMETextMarkdown: desc}
	return
}

// AutoCompleteOptionsInCell implements an `complete_request` from Jupyter, using `gopls`.
// It updates `main.go` with the cell contents (given as lines)
func (s *State) AutoCompleteOptionsInCell(cellLines []string, skipLines map[int]struct{},
	cursorLine, cursorCol int, reply *kernel.CompleteReply) (err error) {
	if s.gopls == nil {
		// gopls not installed.
		return
	}
	if _, found := skipLines[cursorLine]; found {
		// Only Go code can be inspected here.
		err = errors.Errorf("goexec.AutoCompleteOptionsInCell() can only auto-complete Go code, line %d is a secial command line: %q", cursorLine, cellLines[cursorLine])
		return
	}

	// Generate `main.go` (and maybe `other.go`) with contents of current cell.
	cellId := -1 // AutoComplete doesn't actually execute it, so parsed contents of cell are not kept.
	cursorInCell := Cursor{cursorLine, cursorCol}
	updatedDecls, mainDecl, cursorInFile, fileToCellIdAndLine, err := s.parseLinesAndComposeMain(nil, cellId, cellLines, skipLines, cursorInCell)
	if err != nil {
		glog.V(1).Infof("Non-ParseError during parsing: %+v", err)
		err = nil
		// Render memorized definitions on a side file, so `gopls` can pick those definitions if needed for
		// auto-complete.
		err = s.createAlternativeFileFromDecls(s.Decls)
		glog.V(2).Infof(". Alternative file %q with memorized definitions created", s.AlternativeDefinitionsPath())
		if err != nil {
			return
		}
		defer func() {
			// Remove alternative file after
			err2 := os.Remove(s.AlternativeDefinitionsPath())
			if err2 != nil && !os.IsNotExist(err2) {
				glog.Errorf("Failed to remove alternative definitions: %+v", err2)
			}
			glog.V(2).Infof(". Alternative file %q with memorized definitions removed", s.AlternativeDefinitionsPath())
		}()
	} else {
		// If parsing succeeded, execute `goimports`: we just want to make sure that "go get" is executed for the
		// needed packages.
		cursorInFile, _, err = s.GoImports(nil, updatedDecls, mainDecl, fileToCellIdAndLine)
		if err != nil {
			err = errors.WithMessagef(err, "goimports failed")
			return
		}
	}
	if glog.V(1) {
		s.logCursor(cursorInFile)
	}

	// Query `gopls`.
	ctx := context.Background()
	_ = cursorInFile
	var matches []string
	var replaceLength int
	matches, replaceLength, err = s.gopls.Complete(ctx, s.MainPath(), cursorInFile.Line, cursorInFile.Col)
	if err != nil {
		err = errors.Cause(err)
		return
	}
	if replaceLength > 0 {
		replaceStr := cellLines[cursorLine][cursorCol-replaceLength : cursorCol]
		replaceLengthUTF16 := len(utf16.Encode([]rune(replaceStr)))
		reply.CursorStart -= replaceLengthUTF16
	}
	if len(matches) > 0 {
		reply.Matches = matches
	}
	return
}
