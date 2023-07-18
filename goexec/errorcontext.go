package goexec

import (
	"fmt"
	"github.com/janpfeifer/gonb/kernel"
	"github.com/pkg/errors"
	"golang.org/x/exp/constraints"
	"html"
	"io"
	"os"
	"regexp"
	"strconv"
	"strings"
	"text/template"
)

// errorReport is the structure to feed templateErrorReport
type errorReport struct {
	Lines []errorLine
}

type errorLine struct {
	HasContext bool   // Whether this line has a contextual mouse-over content.
	Message    string // Error message, what comes after the `file:line_number:col_number`
	Location   string // `file:line_number:col_number` prefix, only if HasContext == true.
	Context    string // Context to display on a mouse-over window, only if HasContext == true.

	HasCellInfo bool
	CellInfo    string
}

func (e *errorLine) getTraceback() string {
	return e.Message
}

// To check the standard Jupyter colors to choose from, see:
// https://github.com/jupyterlab/jupyterlab/blob/master/packages/theme-light-extension/style/variables.css
var templateErrorReport = template.Must(template.New("error_report").Parse(`
<style>
.gonb-error-location {
	background: var(--jp-error-color2);  
	border-radius: 3px;
	border-style: dotted;
	border-width: 1px;
	border-color: var(--jp-border-color2);
}
.gonb-error-location:hover {
	border-width: 2px;
	border-style: solid;
	border-color: var(--jp-border-color2);
}
.gonb-error-context {
	display: none;
}
.gonb-error-location:hover + .gonb-error-context {
	background: var(--jp-dialog-background);  
	border-radius: 3px;
	border-style: solid;
	border-width: 1px;
	border-color: var(--jp-border-color2);
	display: block;
	white-space: pre;
	font-family: monospace;
}
.gonb-error-line {
	border-radius: 3px;
	border-style: dotted;
	border-width: 1px;	
	border-color: var(--jp-border-color2);
	background-color: var(--jp-rendermime-error-background);
	font-weight: bold;
}
.gonb-cell-line-info {
	background: var(--jp-layout-color2);
	color: #999;
	margin: 0.1em;
	border: 1px solid var(--jp-border-color1);
	padding-left: 0.2em;
	padding-right: 0.2em;
}
</style>
<div class="lm-Widget p-Widget lm-Panel p-Panel jp-OutputArea-child">
<div class="lm-Widget p-Widget jp-RenderedText jp-mod-trusted jp-OutputArea-output" data-mime-type="application/vnd.jupyter.stderr" style="font-family: monospace;">
{{range .Lines}}
{{if .HasContext}}{{if .HasCellInfo}}<span class="gonb-cell-line-info">{{.CellInfo}}</span>
{{end}}<span class="gonb-error-location">{{.Location}}</span> {{.Message}}
<div class="gonb-error-context">
{{.Context}}
</div>
{{else}}
<span style="white-space: pre;">{{.Location}} {{.Message}}</span>
{{end}}
<br/>
{{end}}
</div>
`))

// Example type of error message:
// /tmp/gonb_4e5ea2e7/main.go:3:1: expected declaration, found fmt

// DisplayErrorWithContext in an HTML div, with a mouse-over pop-up window
// listing the lines with the error, and highlighting the exact position.
//
// Any errors within here are logged and simply ignored, since this is already
// used to report errors
func (s *State) DisplayErrorWithContext(msg kernel.Message, fileToCellIdAndLine []CellIdAndLine, errorMsg string) *GonbError {
	nbErr := newError(s, fileToCellIdAndLine, errorMsg)
	nbErr.reportHtml(msg)
	return nbErr
}

var reFileLinePrefix = regexp.MustCompile(`(^.*main\.go:(\d+):(\d+): )(.+)$`)

const LinesForErrorContext = 3

func min[T constraints.Ordered](a, b T) T {
	if a < b {
		return a
	}
	return b
}

func max[T constraints.Ordered](a, b T) T {
	if a > b {
		return a
	}
	return b
}

func inBetween[T constraints.Ordered](x, from, to T) T {
	return min(max(x, from), to)
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

// readMainGo reads the contents of main.go file.
func (s *State) readMainGo() (string, error) {
	f, err := os.Open(s.MainPath())
	if err != nil {
		return "", errors.Wrapf(err, "failed readMainGo()")
	}
	defer func() {
		_ = f.Close() // Ignoring error on closing file for reading.
	}()
	content, err := io.ReadAll(f)
	if err != nil {
		return "", errors.Wrapf(err, "failed readMainGo()")
	}
	return string(content), nil
}
