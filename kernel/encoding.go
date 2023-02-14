package kernel

import "unicode/utf16"

// CursorPosToBytePos converts a `cursor_pos` in a cell, encoded in UTF-16, to a
// byte position in the cell text, encoded in UTF-8 by Go.
//
// Documentation is a bit confusing, according to this:
// https://jupyter-client.readthedocs.io/en/stable/messaging.html#cursor-pos-and-unicode-offsets
// For version >= 5.2 cursor pos should be given as number of unicode runes
// (?). But in practice Jupyter is sending UTF-16
func CursorPosToBytePos(cellContent string, cursorPosUTF16 int) int {
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
