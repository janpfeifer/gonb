package goexec

import (
	"context"
	"github.com/janpfeifer/gonb/common"
	"github.com/janpfeifer/gonb/gonbui/protocol"
	"github.com/janpfeifer/gonb/internal/kernel"
	"github.com/pkg/errors"
	"k8s.io/klog/v2"
	"os"
	"path"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

// This file implements inspecting an identifier (`InspectRequest`) in a Cell and auto-complete
// functionalities.

// standardFilesForNotification for gopls.
var standardFilesForNotification = []string{
	"main.go", "go.mod", "go.sum", "go.work", "other.go",
}

func (s *State) notifyAboutStandardAndTrackedFiles(ctx context.Context) (err error) {
	for _, filePath := range standardFilesForNotification {
		err = s.gopls.NotifyDidOpenOrChange(ctx, path.Join(s.TempDir, filePath))
		if err != nil {
			return
		}
	}
	err = s.EnumerateUpdatedFiles(func(filePath string) error {
		klog.V(1).Infof("Notified of change to %q", filePath)
		return s.gopls.NotifyDidOpenOrChange(ctx, filePath)
	})
	if err != nil {
		return
	}
	return
}

// InspectIdentifierInCell implements an `inspect_request` from Jupyter, using `gopls`.
// It updates `main.go` with the cell contents (given as Lines)
func (s *State) InspectIdentifierInCell(lines []string, skipLines map[int]struct{}, cursorLine, cursorCol int) (mimeMap kernel.MIMEMap, err error) {
	klog.V(2).Infof("InspectIdentifierInCell: ")
	if s.gopls == nil {
		// gopls not installed.
		return make(kernel.MIMEMap), nil
	}
	if _, found := skipLines[cursorLine]; found {
		// Only Go code can be inspected here.
		err = errors.Errorf("goexec.InspectIdentifierInCell() can only inspect Go code, line %d is a secial command line: %q", cursorLine, lines[cursorLine])
		return
	}

	// Runs AutoTrack: makes sure redirects in go.mod and use clauses in go.work are tracked.
	err = s.AutoTrack()
	if err != nil {
		return
	}

	// Adjust cursor to identifier.
	cursorInCell := Cursor{cursorLine, cursorCol}
	cursorInCell = adjustCursorForFunctionIdentifier(lines, skipLines, cursorInCell)

	// Generate `main.go` with contents of current cell.
	cellId := -1 // Inspect doesn't actually execute it, so parsed contents of cell are not kept.
	updatedDecls, mainDecl, cursorInFile, fileToCellIdAndLine, err := s.parseLinesAndComposeMain(nil, cellId, lines, skipLines, cursorInCell)
	if err != nil {
		klog.V(2).Infof("Ignoring parse err for InspectRequest: %+v", err)
		err = nil
		// Render memorized definitions on a side file, so `gopls` can pick those definitions if needed for
		// auto-complete.
		err = s.createAlternativeFileFromDecls(s.Definitions)
		klog.V(2).Infof(". Alternative file %q with memorized definitions created", s.AlternativeDefinitionsPath())
		if err != nil {
			return
		}
		defer func() {
			// Remove alternative file after
			err2 := os.Remove(s.AlternativeDefinitionsPath())
			if err2 != nil && !os.IsNotExist(err2) {
				klog.Errorf("Failed to remove alternative definitions: %+v", err2)
			}
			klog.V(2).Infof(". Alternative file %q with memorized definitions removed", s.AlternativeDefinitionsPath())
		}()

	} else {
		// ProgramExecutor `goimports`: we just want to make sure that "go get" is executed for the needed packages.
		cursorInFile, _, err = s.GoImports(nil, updatedDecls, mainDecl, fileToCellIdAndLine)
		if err != nil {
			err = errors.WithMessagef(err, "goimports failed")
			return
		}
	}
	if klog.V(1).Enabled() {
		s.logCursor(cursorInFile)
	}

	// Query `gopls`.
	ctx := context.Background()
	var desc string
	klog.V(2).Infof("InspectIdentifierInCell: gopls.Definition(ctx, %s, %d, %d)",
		s.CodePath(), cursorInFile.Line, cursorInFile.Col)

	// Notify about standard files updates:
	err = s.notifyAboutStandardAndTrackedFiles(ctx)
	if err != nil {
		return
	}
	desc, err = s.gopls.Definition(ctx, s.CodePath(), cursorInFile.Line, cursorInFile.Col)
	messages := s.gopls.ConsumeMessages()
	if err != nil {
		parts := []string{errors.Cause(err).Error()}
		if len(messages) > 0 {
			parts = append(parts, messages...)
		}
		return kernel.MIMEMap{string(protocol.MIMETextPlain): strings.Join(parts, "\n\n")}, nil
	}

	// Return MIMEMap with markdown.
	mimeMap = kernel.MIMEMap{string(protocol.MIMETextMarkdown): desc}
	return
}

// AutoCompleteOptionsInCell implements a `complete_request` from Jupyter, using `gopls`.
// It updates `main.go` with the cell contents (given as Lines)
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

	// Runs AutoTrack: makes sure redirects in go.mod and use clauses in go.work are tracked.
	err = s.AutoTrack()
	if err != nil {
		return
	}

	// Generate `main.go` (and maybe `other.go`) with contents of current cell.
	cellId := -1 // AutoComplete doesn't actually execute it, so parsed contents of cell are not kept.
	cursorInCell := Cursor{cursorLine, cursorCol}
	updatedDecls, mainDecl, cursorInFile, fileToCellIdAndLine, err := s.parseLinesAndComposeMain(nil, cellId, cellLines, skipLines, cursorInCell)
	if err != nil {
		klog.V(2).Infof("Ignoring ParseError for auto-complete: %+v", err)
		err = nil
		// Render memorized definitions on a side file, so `gopls` can pick those definitions if needed for
		// auto-complete.
		err = s.createAlternativeFileFromDecls(s.Definitions)
		klog.V(2).Infof(". Alternative file %q with memorized definitions created", s.AlternativeDefinitionsPath())
		if err != nil {
			return
		}
		defer func() {
			// Remove alternative file after
			err2 := os.Remove(s.AlternativeDefinitionsPath())
			if err2 != nil && !os.IsNotExist(err2) {
				klog.Errorf("Failed to remove alternative definitions: %+v", err2)
			}
			klog.V(2).Infof(". Alternative file %q with memorized definitions removed", s.AlternativeDefinitionsPath())
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
	if klog.V(1).Enabled() {
		s.logCursor(cursorInFile)
	}

	// Query `gopls`.
	ctx := context.Background()
	err = s.notifyAboutStandardAndTrackedFiles(ctx)
	if err != nil {
		return
	}
	_ = cursorInFile
	var matches []string
	var replaceLength int
	matches, replaceLength, err = s.gopls.Complete(ctx, s.CodePath(), cursorInFile.Line, cursorInFile.Col)
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

// runeIndicesForLine returns the start of each rune in the line (encoded as UTF-8).
func runeIndicesForLine(line string, col int) (runeIndices []int, colIdx int) {
	runeIndices = make([]int, 0, len(line))
	for ii := range line {
		runeIndices = append(runeIndices, ii)
		if ii <= col {
			colIdx = len(runeIndices) - 1
		}
	}
	return
}

// adjustCursorForFunctionIdentifier adjusts the cursor to go to the function (or method) name
// just before the cursor position, if it is over a comma or "(".
//
// This is a very simple implementation, and not a generic parser of Go code. So it won't deal
// with nested function calls.
//
// Examples (where `‸` represents the cursor position):
//
//   - `f(‸)` -> will get mapped to `f‸()`
//   - `f(x, ‸)` -> will bet mapped to `f‸(x, )`
//
// But these examples won't work:
//
//   - `f(g(), ‸)`, won't be changed because of the nested call to `g()`
func adjustCursorForFunctionIdentifier(lines []string, skipLines common.Set[int], cursor Cursor) Cursor {
	originalCursor := cursor
	line := lines[cursor.Line]
	lineIndices, colIdx := runeIndicesForLine(line, cursor.Col)
	atCursor := func() rune {
		if cursor == NoCursor || colIdx >= len(lineIndices) {
			return rune(0)
		}
		r, _ := utf8.DecodeRuneInString(line[lineIndices[colIdx]:])
		return r
	}
	previousCursorPos := func() {
		colIdx--
		if colIdx >= 0 {
			cursor.Col = lineIndices[colIdx]
			return
		}

		// Move to previous line, skipping "skipLines"
		for cursor.Line > 0 {
			cursor.Line--
			if !skipLines.Has(cursor.Line) {
				break
			}
		}
		if cursor.Line < 0 {
			// Start of file reached.
			cursor = NoCursor
			return
		}

		// Move cursor to end of the previous line.
		line = lines[cursor.Line]
		lineIndices, colIdx = runeIndicesForLine(line, len(line)-1)
		if colIdx < len(lineIndices) {
			cursor.Col = lineIndices[colIdx]
		} else {
			cursor.Col = 0
		}
	}

	// Loop while we don't find the target identifier.
	for cursor != NoCursor {
		r := atCursor()
		//fmt.Printf("\trune@cursor(%+v): (%d)'%c'\n", cursor, int(r), r)
		if r == ',' || r == ')' {
			// Move backwards until we find function/method name, or type name of a list.
			// TODO: it won't work well with generics, when the type parameter is passed.
			for cursor != NoCursor && r != '(' && r != '{' {
				previousCursorPos()
				r = atCursor()
			}
			previousCursorPos()

		} else if r == 0 || r == ' ' || r == '\t' || r == '(' || r == '{' || r == '}' ||
			r == '[' || r == ']' || r == '"' || r == '`' || r == '\'' {
			// Skip symbols and go to previous rune.
			previousCursorPos()

		} else {
			// Otherwise, any non-space rune is considered part of an identifier, return that.
			return cursor
		}
	}
	if cursor == NoCursor {
		return originalCursor
	}
	return cursor
}
