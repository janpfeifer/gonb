// Package dom is part of `gonbui` package and provides an API to directly manipulate the DOM, using Javascript.
//
// Most of the functions use TransientJavascript to execute javascript without
// leaving any traces.
//
// One important consideration: transient content and the manipulated DOM
// bypass the JupyterServer, so they are not exported when the notebook
// is saved or converted (`nbconvert`).
// To work around this, see function `PersistDomElement`.

package dom

import (
	"fmt"
	"github.com/janpfeifer/gonb/gonbui"
	"strings"
)

var transientJavascriptId = "gonb_transient_js_" + gonbui.UniqueId()

// TransientJavascript sends a block of javascript to the front-end notebook that is
// executed on a transient area (using UpdateHtml).
//
// This also prevents using vertical space in the cell output at every execution --
// that happens if using DisplayHtml or ScriptJavascript to execute the javascript code.
func TransientJavascript(js string) {
	gonbui.UpdateHtml(transientJavascriptId,
		fmt.Sprintf("<script>%s</script>\n", js))
	gonbui.Sync()
	// Remove javascript so it's not left-over to be saved and/or later executed without its full context.
	gonbui.UpdateHtml(transientJavascriptId, "")
}

// escapeForJavascriptSingleQuotes where str will be inserted in single quotes
// in a piece of javascript code.
func escapeForJavascriptSingleQuotes(str string) string {
	// - Escape the backslashes (\)
	str = strings.Replace(str, `\`, `\\`, -1)
	// - Escape single-quotes
	str = strings.Replace(str, `'`, `\'`, -1)
	// - Escape newlines
	str = strings.Replace(str, "\n", `\n`, -1)
	// - Escape tabs
	str = strings.Replace(str, "\t", `\t`, -1)
	return str
}

// RelativePositionId is used by InsertAdjacentHtml to indicate where, relative
// to an HTML element (pointed by an id), the html piece is to be inserted.
type RelativePositionId string

const (
	BeforeBegin RelativePositionId = "beforebegin"
	AfterBegin  RelativePositionId = "afterbegin"
	BeforeEnd   RelativePositionId = "beforeend"
	AfterEnd    RelativePositionId = "afterend"
)

// InsertAdjacent inserts the html content (in the front-end) adjacent to the element pointed by
// referenceId, and in the position described by pos.
// It uses the Javascript method `insertAdjacentHTML`, see details in:
// https://developer.mozilla.org/en-US/docs/Web/API/Element/insertAdjacentHTML
//
// Important considerations:
//   - Output generated in this format is not saved or convertable to HTML.
//     It is generated dynamically with Javascript, which is not captured by Jupyter.
//   - This prevents adding extra vertical space for each call of DisplayHtml,
//     which allows one to better tailor the output.
func InsertAdjacent(referenceId string, pos RelativePositionId, html string) {
	referenceId = escapeForJavascriptSingleQuotes(referenceId)
	html = escapeForJavascriptSingleQuotes(html)
	js := fmt.Sprintf(`
(() => {
	const html='%s';
	let element = document.getElementById('%s');
	element.insertAdjacentHTML('%s', html);
})();
`, html, referenceId, pos)
	TransientJavascript(js)
}

// Append the given `html` content to the element identified by `parentHtmlId` in the DOM.
// It uses TransientJavascript to inject the given html at the end of the element's `innerHTML` in the DOM.
//
// Important considerations:
//   - Output generated in this format is not saved or convertable to HTML.
//     It is generated dynamically with Javascript, which is not captured by Jupyter.
//   - This prevents adding extra vertical space for each call of DisplayHtml,
//     which allows one to better tailor the output.
//
// If you need more specialized control where the `html` is inserted, check
// InsertAdjacent call.
func Append(parentHtmlId, html string) {
	InsertAdjacent(parentHtmlId, BeforeEnd, html)
}

// SetInnerHtml sets the html contents of a DOM element identified by `htmlId`.
//
// Important considerations:
//   - Output generated in this format is not saved or convertable to HTML.
//     It is generated dynamically with Javascript, which is not captured by Jupyter.
//   - This prevents adding extra vertical space for each call of DisplayHtml,
//     which allows one to better tailor the output.
//
// For specialized control where the `html` is inserted, check
// InsertAdjacent call.
func SetInnerHtml(htmlId, html string) {
	htmlId = escapeForJavascriptSingleQuotes(htmlId)
	html = escapeForJavascriptSingleQuotes(html)
	js := fmt.Sprintf(`
(() => {
	let element = document.getElementById('%s');
	element.innerHTML = '%s';
})();
`, htmlId, html)
	TransientJavascript(js)
}

// Remove removes element identified by htmlId from DOM.
func Remove(htmlId string) {
	htmlId = escapeForJavascriptSingleQuotes(htmlId)
	js := fmt.Sprintf(`
(() => {
	let element = document.getElementById('%s');
	element.parentNode.removeChild(element);
})();
`, htmlId)
	TransientJavascript(js)
}
