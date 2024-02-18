package gonbui

import (
	"bytes"
	"github.com/janpfeifer/gonb/gonbui/protocol"
	"github.com/pkg/errors"
	"text/template"
)

// ScriptJavascript executes the given Javascript script in the Notebook.
//
// Errors in javascript parsing are sent by Jupyter Server to the stderr -- as opposed to showing
// to the browser console, which may be harder to debug.
//
// Also, like with DisplayHtml, each execution creates a new `<div>` block in the output area.
// Even if empty, it uses up a bit of vertical space (Jupyter Notebook thing).
//
// If these are an issue, consider using TransientJavascript, which uses a transient area
// to execute the Javascript, which is re-used for every execution.
func ScriptJavascript(js string) {
	if !IsNotebook {
		return
	}
	SendData(&protocol.DisplayData{
		Data: map[protocol.MIMEType]any{protocol.MIMETextJavascript: js},
	})
}

var loadAndRunTmpl = template.Must(template.New("load_and_run").Parse(`
(() => {
	const src="{{.Src}}";
	var runJSFn = function() {
		{{.RunJS}}
	}
	
	var currentScripts = document.head.getElementsByTagName("script");
	for (const idx in currentScripts) {
		let script = currentScripts[idx];
		if (script.src == src) {
			runJSFn();
			return;
		}
	}

	var script = document.createElement("script");
{{range $key, $value := .Attributes}}
	script.{{$key}} = "{{$value}}";
{{end}}	
	script.src = src;
	script.onload = script.onreadystatechange = runJSFn
	document.head.appendChild(script);	
})();
`))

// LoadScriptModuleAndRun loads the given script module and, `onLoad`, runs the given code.
//
// If the module has been previously loaded, it immediately runs the given code.
//
// The script module given is appended to the `HEAD` of the page.
//
// Extra `attributes` can be given, and will be appended to the `script` node.
//
// Example: to make sure Plotly Javascript (https://plotly.com/javascript/) is loaded --
// please check Plotly's installation directions for the latest version.
//
//	gonbui.LoadScriptModuleAndRun(
//		"https://cdn.plot.ly/plotly-2.29.1.min.js", {"charset": "utf-8"},
//		"console.log('Plotly loaded.'));
func LoadScriptModuleAndRun(src string, attributes map[string]string, runJS string) error {
	var buf bytes.Buffer
	data := struct {
		Src, RunJS string
		Attributes map[string]string
	}{
		Src:        src,
		RunJS:      runJS,
		Attributes: attributes,
	}
	err := loadAndRunTmpl.Execute(&buf, data)
	if err != nil {
		return errors.Wrapf(err, "failed to execut template for LoadScriptModuleRun()")
	}
	js := buf.String()
	ScriptJavascript(js)
	return nil
}
