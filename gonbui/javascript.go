package gonbui

import (
	"github.com/janpfeifer/gonb/gonbui/protocol"
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
//
// Note: `text/javascript` mime-type ([protocol.MIMETextJavascript]) is not supported by VSCode,
// it's displayed as text. So using this won't work in VSCode. Consider instead using [DisplayHtml],
// and wrapping the `js` string with `("<scrip>%s</script>", js)`.
func ScriptJavascript(js string) {
	if !IsNotebook {
		return
	}
	SendData(&protocol.DisplayData{
		Data: map[protocol.MIMEType]any{protocol.MIMETextJavascript: js},
	})
}
