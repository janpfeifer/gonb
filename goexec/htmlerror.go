package goexec

import (
	"bytes"
	"regexp"
	"text/template"

	"github.com/janpfeifer/gonb/kernel"
	"k8s.io/klog/v2"
)

// errorReport is the structure to feed templateErrorReport
type errorReport struct {
	Lines []errorLine
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
{{.HtmlContext}}
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
func (s *State) DisplayErrorWithContext(msg kernel.Message, fileToCellIdAndLine []CellIdAndLine, errorMsg string, error error) error {
	nbErr := newError(s, fileToCellIdAndLine, errorMsg, error)
	if s.rawError {
		return nbErr
	} else {
		nbErr.reportHtml(msg)
		return error
	}
}

var reFileLinePrefix = regexp.MustCompile(`(^.*main\.go:(\d+):(\d+): )(.+)$`)

const LinesForErrorContext = 3

// toReport creates an error report (for use in html reports)
func (err *GonbError) toReport() *errorReport {
	report := &errorReport{Lines: make([]errorLine, len(err.lines))}
	for ii, line := range err.lines {
		report.Lines[ii] = line
	}
	return report
}
// reportHtml reports the error as an HTML report in jupyter
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

