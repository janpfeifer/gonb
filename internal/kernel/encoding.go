package kernel

import (
	"strings"
	"unicode/utf16"
)

// JupyterCursorPosToBytePos converts a `cursor_pos` in a cell, encoded in UTF-16, to a
// byte position in the cell text, encoded in UTF-8 by Go.
//
// Documentation is a bit confusing, according to this:
// https://jupyter-client.readthedocs.io/en/stable/messaging.html#cursor-pos-and-unicode-offsets
// For version >= 5.2 cursor pos should be given as number of unicode runes
// (?). But in practice Jupyter is sending UTF-16
func JupyterCursorPosToBytePos(cellContent string, cursorPosUTF16 int) int {
	utf16Pos := 0
	for bytePos, r := range cellContent {
		if utf16Pos >= cursorPosUTF16 {
			return bytePos
		}
		utf16len := len(utf16.Encode([]rune{r}))
		utf16Pos += utf16len
	}
	return len(cellContent)
}

// JupyterToLinesAndCursor takes as input the cell contents and a cursor (as a position in UTF16 points)
// and returns a slice with the individual lines and a cursor split into cursorLine and cursorCol,
// bytes based (meaning cursorCol is the position in the line in number of bytes).
func JupyterToLinesAndCursor(cellContent string, cursorPosUTF16 int) (lines []string, cursorLine, cursorCol int) {
	cursorPos := JupyterCursorPosToBytePos(cellContent, cursorPosUTF16) // UTF16 pos to byte pos
	lines = strings.Split(cellContent, "\n")
	for pos := 0; cursorLine < len(lines) && pos < cursorPos; {
		nextLinePos := pos + len(lines[cursorLine]) + 1
		if cursorPos < nextLinePos {
			cursorCol = cursorPos - pos
			break
		}
		pos = nextLinePos
		cursorLine++
	}
	return
}
