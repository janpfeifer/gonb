package goexec

import (
	"bytes"
	"fmt"
	"github.com/janpfeifer/gonb/kernel"
	"github.com/pkg/errors"
	"golang.org/x/exp/constraints"
	"html"
	"io"
	"log"
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
}

var templateErrorReport = template.Must(template.New("error_report").Parse(`
<style>
.gonb-error-location {
	background: #f8f8e0; 
	border-radius: 3px;
	border-style: dotted;
	border-width: 1px;
	border-color: black;
}
.gonb-error-location:hover {
	border-width: 2px;
	border-style: solid;
}
.gonb-error-context {
	display: none;
}
.gonb-error-location:hover + .gonb-error-context {
	border-radius: 3px;
	border-style: solid;
	border-width: 1px;
	border-color: black;
	display: block;
	background: #e0e0e0;
	white-space: pre;
	font-family: monospace;
}
.gonb-error-line {
	border-radius: 3px;
	border-style: dotted;
	border-width: 1px;	
	font-weight: bold;
}
</style>
<div class="lm-Widget p-Widget lm-Panel p-Panel jp-OutputArea-child">
<div class="lm-Widget p-Widget jp-RenderedText jp-mod-trusted jp-OutputArea-output" data-mime-type="application/vnd.jupyter.stderr" style="font-family: monospace;">
{{range .Lines}}
{{if .HasContext}}
<span class="gonb-error-location">{{.Location}}</span> {{.Message}}
<div class="gonb-error-context">{{.Context}}</div>
{{else}}
<pre>{{.Message}}</pre>
{{end}}
<br/>
{{end}}
</div>
`))

// /tmp/gonb_4e5ea2e7/main.go:3:1: expected declaration, found fmt

// DisplayErrorWithContext in an HTML div, with a mouse-over pop-up window
// listing the lines with the error, and highlighting the exact position.
//
// Any errors within here are logged and simply ignored, since this is already
// used to report errors
func (s *State) DisplayErrorWithContext(msg kernel.Message, errorMsg string) {
	// Default report, and makes sure display is called at the end.
	reportHTML := "<pre>" + errorMsg + "</pre>" // If anything goes wrong, simply display the error message.
	defer func() {
		// Display HTML report on exit.
		err := kernel.PublishDisplayDataWithHTML(msg, reportHTML)
		if err != nil {
			log.Printf("Failed to publish data in DisplayErrorWithContext: %+v", err)
		}
	}()

	// Read main.go into lines.
	mainGo, err := s.readMainGo()
	if err != nil {
		log.Printf("DisplayErrorWithContext: %+v", err)
		return
	}
	codeLines := strings.Split(mainGo, "\n")

	// Parse error lines.
	lines := strings.Split(errorMsg, "\n")
	report := &errorReport{Lines: make([]errorLine, len(lines))}
	for ii, line := range lines {
		report.Lines[ii] = s.parseErrorLine(line, codeLines)
	}

	// Render error block.
	buf := bytes.NewBuffer(make([]byte, 0, 512*len(lines)))
	if err := templateErrorReport.Execute(buf, report); err != nil {
		log.Printf("Failed to execute template in DisplayErrorWithContext: %+v", err)
		return
	}
	reportHTML = buf.String()
	// reportHTML will be displayed on the deferred function above.
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

func (s *State) parseErrorLine(lineStr string, codeLines []string) (l errorLine) {
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
	return
}

// readMainGo reads the contents of main.go file.
func (s *State) readMainGo() (string, error) {
	f, err := os.Open(s.MainPath())
	if err != nil {
		return "", errors.Wrapf(err, "failed readMainGo()")
	}
	defer f.Close()
	content, err := io.ReadAll(f)
	if err != nil {
		return "", errors.Wrapf(err, "failed readMainGo()")
	}
	return string(content), nil
}
