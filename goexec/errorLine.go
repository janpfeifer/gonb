package goexec

import (
	"fmt"
	"github.com/fatih/color"
	"html"
	"strconv"
	"strings"
)

type errorLine struct {
	HasContext  bool   // Whether this line has a contextual mouse-over content.
	Message     string // Error message, what comes after the `file:line_number:col_number`
	Location    string // `file:line_number:col_number` prefix, only if HasContext == true.
	HtmlContext string // HtmlContext to display on a mouse-over window, only if HasContext == true.
	rawContext  string // rawContext to display on a traceback, only if HasContext == true.

	HasCellInfo bool
	CellInfo    string
}

func (e *errorLine) getTraceback() (message string) {

	if e.HasCellInfo {
		message += e.CellInfo + "\n"
	}
	if e.HasContext {
		message += e.rawContext + "\n"
	}
	message += color.New(color.FgRed).Sprint(e.Message)
	return message
}
func (e *errorLine) getCol() int {
	split := strings.Split(e.Location, ":")
	if split[0] != "" {
		col, _ := strconv.Atoi(split[2])
		return col
	}
	return -1
}
func (e *errorLine) getColLine() string {
	col := e.getCol()
	if col == -1 {
		return ""
	}
	line := strings.Repeat(" ", col-1)
	line += "^"
	return line + "\n"
}
func (s *State) parseErrorLine(lineStr string, codeLines []string, fileToCellIdAndLine []CellIdAndLine) (l errorLine) {
	l.HasContext = false
	matches := reFileLinePrefix.FindStringSubmatch(lineStr)
	if len(codeLines) == 0 || len(matches) != 5 {
		l.HasContext = false
		l.Message = lineStr
		return
	}

	l.HasContext = true
	l.Message = matches[4]
	l.Location = matches[1]

	lineNum, _ := strconv.Atoi(matches[2])
	lineNum -= 1 // Error messages start at line 1 (as opposed to 0)
	//colNum, _ := strconv.Atoi(matches[3])
	fromLines := lineNum - LinesForErrorContext
	fromLines = inBetween(fromLines, 0, len(codeLines)-1)
	toLines := lineNum + LinesForErrorContext
	toLines = inBetween(toLines, 0, len(codeLines))

	partsHtml := make([]string, 0, toLines-fromLines)
	partsRaw := make([]string, 0, toLines-fromLines)
	for ii := fromLines; ii < toLines; ii++ {
		partRaw := codeLines[ii] + "\n"
		partHtml := html.EscapeString(codeLines[ii]) + "\n"
		if ii == lineNum {
			partHtml = fmt.Sprintf(`<div class="gonb-error-line">%s</div>`, partHtml)
			partRaw += l.getColLine()
		}
		partsHtml = append(partsHtml, partHtml)
		partsRaw = append(partsRaw, partRaw)

	}
	l.HtmlContext = strings.Join(partsHtml, "")
	l.rawContext = strings.Join(partsRaw, "")

	// Gather CellInfo
	if lineNum > 0 && lineNum < len(fileToCellIdAndLine) && fileToCellIdAndLine[lineNum].Line != NoCursorLine {
		cell := fileToCellIdAndLine[lineNum]
		l.HasCellInfo = true
		// Notice GoNB store lines starting at 0, but Jupyter display lines starting at 1, so we add 1 here.
		if cell.Id != -1 {
			l.CellInfo = fmt.Sprintf("Cell[%d]: Line %d", cell.Id, cell.Line+1)
		} else {
			l.CellInfo = fmt.Sprintf("Cell Line %d", cell.Line+1)
		}
	}
	return
}
