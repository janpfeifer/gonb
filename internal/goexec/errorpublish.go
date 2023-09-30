package goexec

import (
	"bytes"
	"github.com/pkg/errors"
	"text/template"

	"github.com/janpfeifer/gonb/internal/kernel"
	"k8s.io/klog/v2"
)

// To check the standard Jupyter colors to choose from, see:
// https://github.com/jupyterlab/jupyterlab/blob/master/packages/theme-light-extension/style/variables.css
var templateErrorReport = template.Must(template.New("error_report").Parse(`
<style>
.gonb-err-location {
	background: var(--jp-err-color2);  
	border-radius: 3px;
	border-style: dotted;
	border-width: 1px;
	border-color: var(--jp-border-color2);
}
.gonb-err-location:hover {
	border-width: 2px;
	border-style: solid;
	border-color: var(--jp-border-color2);
}
.gonb-err-context {
	display: none;
}
.gonb-err-location:hover + .gonb-err-context {
	background: var(--jp-dialog-background);  
	border-radius: 3px;
	border-style: solid;
	border-width: 1px;
	border-color: var(--jp-border-color2);
	display: block;
	white-space: pre;
	font-family: monospace;
}
.gonb-err-line {
	border-radius: 3px;
	border-style: dotted;
	border-width: 1px;	
	border-color: var(--jp-border-color2);
	background-color: var(--jp-rendermime-err-background);
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
{{end}}<span class="gonb-err-location">{{.Location}}</span> {{.Message}}
<div class="gonb-err-context">
{{.HtmlContext}}
</div>
{{else}}
<span style="white-space: pre;">{{.Location}} {{.Message}}</span>
{{end}}
<br/>
{{end}}
</div>
`))

// Example type of err message:
// /tmp/gonb_4e5ea2e7/main.go:3:1: expected declaration, found fmt

// DisplayErrorWithContext in an HTML div, with a mouse-over pop-up window
// listing the Lines with the error, and highlighting the exact position.
//
// Except if `rawError` is set to true (see `New() *State`): in which case the enriched GonbError is returned
// instead, for a textual report back.
//
// Any errors within here are logged and simply ignored, since this is already
// used to report errors.
func (s *State) DisplayErrorWithContext(msg kernel.Message, fileToCellIdAndLine []CellIdAndLine, errorMsg string, err error) error {
	nbErr := newGonbErrors(s, fileToCellIdAndLine, errorMsg, err)
	if s.rawError {
		return nbErr
	} else {
		nbErr.PublishWithHTML(msg)
		return err
	}
}

// LinesForErrorContext indicates how many lines to display in the error context, before and after the offending line.
// Hard-coded for now, but it could be made configurable.
const LinesForErrorContext = 3

// PublishWithHTML reports the GonbError as an HTML report in Jupyter.
func (nbErr *GonbError) PublishWithHTML(msg kernel.Message) {
	if msg == nil {
		// Ignore, if there is no kernel.Message to reply to.
		return
	}
	// Default report, and makes sure display is called at the end.
	htmlReport := "<pre>" + nbErr.errMsg + "</pre>" // If anything goes wrong, simply display the err message.
	defer func() {
		// Display HTML report on exit.
		err := kernel.PublishHtml(msg, htmlReport)
		if err != nil {
			klog.Errorf("Failed to publish data in DisplayErrorWithContext: %+v", err)
		}
	}()

	// Render err block.
	buf := bytes.NewBuffer(make([]byte, 0, 512*len(nbErr.Lines)))
	if err := templateErrorReport.Execute(buf, nbErr); err != nil {
		klog.Errorf("Failed to execute template in DisplayErrorWithContext: %+v", err)
		return
	}
	htmlReport = buf.String()
	// htmlReport will be displayed on the deferred function above.
}

// JupyterErrorSplit takes an error and formats it into the components Jupyter
// protocol uses for it.
//
// It special cases the GonbError, where it adds each sub-error in the "traceback" repeated field.
func JupyterErrorSplit(err error) (string, string, []string) {
	var nbErr *GonbError
	if errors.As(err, &nbErr) {
		return nbErr.Name(), nbErr.Error(), nbErr.Traceback()
	} else {
		return "ERROR", err.Error(), []string{err.Error()}
	}
}
