package goexec

import (
	"fmt"
	"github.com/fatih/color"
	"html"
	"strconv"
	"strings"
)

type errorLine struct {
	HasContext bool   // Whether this line has a contextual mouse-over content.
	Message    string // Error message, what comes after the `file:line_number:col_number`
	Location   string // `file:line_number:col_number` prefix, only if HasContext == true.
	Context    string // Context to display on a mouse-over window, only if HasContext == true.

	HasCellInfo bool
	CellInfo    string
}

func (e *errorLine) getTraceback() (message string) {

	split := strings.Split(e.Location, ":")
	if split[0] != "" {
		file := split[0]
		line := split[1]
		//_ := split[2]
		message += color.New(color.FgBlue).Sprint("\tFile ")
		message += color.New(color.FgGreen).Sprint("\"" + file + "\"")
		message += color.New(color.FgBlue).Sprint(", line ")
		message += color.New(color.FgGreen).Sprint(line)
		message += "\n"
	}

	message += color.New(color.FgRed).Sprint(e.Message)
	return message
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

	parts := make([]string, 0, toLines-fromLines)
	for ii := fromLines; ii < toLines; ii++ {
		part := html.EscapeString(codeLines[ii]) + "\n"
		if ii == lineNum {
			part = fmt.Sprintf(`<div class="gonb-error-line">%s</div>`, part)
		}
		parts = append(parts, part)
	}
	l.Context = strings.Join(parts, "")

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
