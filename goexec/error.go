package goexec

import (
	"bytes"
	"github.com/janpfeifer/gonb/kernel"
	"k8s.io/klog/v2"
	"strings"
	"text/template"
)

type GonbError struct {
	lines  []errorLine
	errMsg string
}

var templateTraceback = template.Must(template.New("traceback").Parse(`
An error occurred while executing the following cell:
------------------
{cell.source}
------------------
{stream_output}

{traceback}
`))

func newError(s *State, fileToCellIdAndLine []CellIdAndLine, errorMsg string) *GonbError {
	// Read main.go into lines.
	mainGo, err := s.readMainGo()
	if err != nil {
		klog.Errorf("DisplayErrorWithContext: %+v", err)
		return nil
	}
	codeLines := strings.Split(mainGo, "\n")

	// Parse error lines.
	lines := strings.Split(errorMsg, "\n")
	nberr := &GonbError{lines: make([]errorLine, len(lines)), errMsg: errorMsg}
	for ii, line := range lines {
		parsed := s.parseErrorLine(line, codeLines, fileToCellIdAndLine)
		nberr.lines[ii] = parsed
	}
	return nberr
}
func (err *GonbError) toReport() *errorReport {
	report := &errorReport{Lines: make([]errorLine, len(err.lines))}
	for ii, line := range err.lines {
		report.Lines[ii] = line
	}
	return report
}
func (err *GonbError) Traceback() []string {
	traceback := make([]string, len(err.lines))
	for ii, line := range err.lines {
		traceback[ii] = line.getTraceback()
	}
	return traceback
}
func (err *GonbError) ErrorMsg() string {
	return err.errMsg
}
func (err *GonbError) ErrorName() string {
	return "Error (new)"
}
func (err *GonbError) reportHtml(msg kernel.Message) {
	if msg == nil {
		// Ignore, if there is no kernel.Message to reply to.
		return
	}
	// Default report, and makes sure display is called at the end.
	reportHTML := "<pre>" + err.errMsg + "</pre>" // If anything goes wrong, simply display the error message.
	defer func() {
		// Display HTML report on exit.
		err := kernel.PublishDisplayDataWithHTML(msg, reportHTML)
		if err != nil {
			klog.Errorf("Failed to publish data in DisplayErrorWithContext: %+v", err)
		}
	}()

	// Render error block.
	buf := bytes.NewBuffer(make([]byte, 0, 512*len(err.lines)))
	if err := templateErrorReport.Execute(buf, err.toReport()); err != nil {
		klog.Errorf("Failed to execute template in DisplayErrorWithContext: %+v", err)
		return
	}
	reportHTML = buf.String()
	// reportHTML will be displayed on the deferred function above.
}
