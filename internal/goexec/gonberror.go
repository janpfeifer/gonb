package goexec

import (
	"fmt"
	"github.com/fatih/color"
	"golang.org/x/exp/constraints"
	"html"
	"k8s.io/klog/v2"
	"regexp"
	"strconv"
	"strings"
)

// GonbError is a special type of error that wraps a collection of errors returned by
// the Go compiler or `go get` or `go imports`.
//
// Each error line (`GonbError.Lines`) holds some context stored in its own errorLine object.
// And it is also a wrapper for the failed execution error.
//
// It can be rendered to HTML in the notebook with `GonbError.PublishWithHTML`.
type GonbError struct {
	Lines  []errorLine
	errMsg string
	err    error
}

// newGonbErrors creates a new GonbError object, translating line numbers for each of the
// included errors to cell IDs and their corresponding lines.
//
// `baseErr` is the error reported by the execution of the Go commands (`go` / `go get` / `go import` / etc.)
// And `errorMsg` has the full output of the Go command, from where to parse the individual sub-errors.
//
// Since GonbError is context dependent (currently defined cells), it must be done immediately after the errors
// were received from
func newGonbErrors(s *State, fileToCellIdAndLine []CellIdAndLine, errorMsg string, baseErr error) *GonbError {
	// Read main.go into Lines.
	mainGo, err := s.readMainGo()
	if err != nil {
		klog.Errorf("DisplayErrorWithContext: %+v", err)
		return nil
	}
	codeLines := strings.Split(mainGo, "\n")

	// Parse err Lines.
	lines := strings.Split(errorMsg, "\n")
	nbErr := &GonbError{Lines: make([]errorLine, len(lines)), errMsg: errorMsg, err: baseErr}
	for ii, line := range lines {
		parsed := s.parseErrorLine(line, codeLines, fileToCellIdAndLine)
		nbErr.Lines[ii] = parsed
	}
	return nbErr
}

// Unwrap returns the underlying error, so it can be used by `errors.Unwrap`.
func (nbErr *GonbError) Unwrap() error {
	return nbErr.err
}

// Error implements golang `error` interface.
// In Jupyter protocol, it corresponds to the "evalue" field (as in "error value").
func (nbErr *GonbError) Error() string {
	return nbErr.errMsg
}

// Traceback corresponds to field "traceback" in Jupyter.
func (nbErr *GonbError) Traceback() []string {
	traceback := make([]string, len(nbErr.Lines))
	for ii, line := range nbErr.Lines {
		traceback[ii] = line.getTraceback()
	}
	return traceback
}

// Name corresponds to field "ename" in Jupyter. Hardcoded in "ERROR" for now.
func (nbErr *GonbError) Name() string {
	return "ERROR"
}

func minT[T constraints.Ordered](a, b T) T {
	if a < b {
		return a
	}
	return b
}

func maxT[T constraints.Ordered](a, b T) T {
	if a > b {
		return a
	}
	return b
}

func inBetween[T constraints.Ordered](x, from, to T) T {
	return minT(maxT(x, from), to)
}

// errorLine describes one error (e.g: one line reported back from the Go compiler),
// optionally with context about the error.
// It is created by `State.parseErrorLine()`.
type errorLine struct {
	HasContext  bool   // Whether this line has a context, usually displayed as a mouse-over content.
	Message     string // Error message, what comes after the `file:line_number:col_number`
	Location    string // `file:line_number:col_number` prefix, only if HasContext == true.
	HtmlContext string // HtmlContext to display on a mouse-over window, only if HasContext == true.
	RawContext  string // RawContext to display on a traceback, only if HasContext == true: this is text only, sent back to Jupyter

	HasCellInfo bool
	CellInfo    string
}

// getTraceback renders the colored traceback sent to Jupyter for this errorLine.
func (e *errorLine) getTraceback() (message string) {
	if e.HasCellInfo {
		message += e.CellInfo + "\n"
	}
	if e.HasContext {
		message += e.RawContext + "\n"
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

var reFileLinePrefix = regexp.MustCompile(`(^.*main(_test)?\.go:(\d+):(\d+): )(.+)$`)

// parseErrorLine parses an err line, and given current line to cell mapping, creates context for the err
// if available.
func (s *State) parseErrorLine(lineStr string, codeLines []string, fileToCellIdAndLine []CellIdAndLine) (l errorLine) {
	l.HasContext = false
	matches := reFileLinePrefix.FindStringSubmatch(lineStr)
	if len(codeLines) == 0 || len(matches) != 6 {
		l.HasContext = false
		l.Message = lineStr
		return
	}

	l.HasContext = true
	l.Message = matches[5]
	l.Location = matches[1]

	lineNum, _ := strconv.Atoi(matches[3])
	lineNum -= 1 // Error messages start at line 1 (as opposed to 0)
	//colNum, _ := strconv.Atoi(matches[4])
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
			partHtml = fmt.Sprintf(`<div class="gonb-err-line">%s</div>`, partHtml)
			partRaw += l.getColLine()
		}
		partsHtml = append(partsHtml, partHtml)
		partsRaw = append(partsRaw, partRaw)

	}
	l.HtmlContext = strings.Join(partsHtml, "")
	l.RawContext = strings.Join(partsRaw, "")

	// Gather CellInfo
	if lineNum > 0 && lineNum < len(fileToCellIdAndLine) && fileToCellIdAndLine[lineNum].Line != NoCursorLine {
		cell := fileToCellIdAndLine[lineNum]
		l.HasCellInfo = true
		// Notice GoNB store Lines starting at 0, but Jupyter display Lines starting at 1, so we add 1 here.
		if cell.Id != -1 {
			l.CellInfo = fmt.Sprintf("Cell[%d]: Line %d", cell.Id, cell.Line+1)
		} else {
			l.CellInfo = fmt.Sprintf("Cell Line %d", cell.Line+1)
		}
	}
	return
}
