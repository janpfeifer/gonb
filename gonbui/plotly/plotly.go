// Package plotly adds barebones support to Plotly (https://plotly.com/javascript/getting-started/)
// library.
//
// It's a simple wrapper to execute javascript code, assuming the Plotly library is loaded.
//
// It also offers a library around github.com/MetalBlueberry/go-plotly (itself a wrapper around
// Plotly) to integrate it in GoNB, see `DisplayFig` method.
//
// It uses go-plotly v0.7.0, which uses github.com/MetalBlueberry/go-plotly/generated/v2.34.0/graph_objects
// for the graph objects (usually aliased as "grob" package).
//
// API is a first stab at it (experimental), and `UpdateHtml` for dynamic plots is not yet supported.
// Ideas are welcome here.
package plotly

import (
	"encoding/json"
	"fmt"

	grob "github.com/MetalBlueberry/go-plotly/generated/v2.34.0/graph_objects"
	"github.com/janpfeifer/gonb/gonbui"
	"github.com/janpfeifer/gonb/gonbui/dom"
	"github.com/pkg/errors"
)

// PlotlySrc is the source from where to download Plotly.
// If you have a local copy or an updated version of the library, change the value here.
var PlotlySrc = "https://cdn.plot.ly/plotly-2.34.0.min.js"

// DisplayFig as HTML output.
func DisplayFig(fig *grob.Fig) error {
	return displayFigToId("", fig)
}

// AppendFig appends the figure to the HTML element with the given id. It uses [dom.TransientJavascript] so it won't be
// saved, exported to HTML, or even persistable with [dom.Persist].
// See [dom.CreateTransientDiv] to create transient div and get its `htmlId`.
func AppendFig(htmlId string, fig *grob.Fig) error {
	if htmlId == "" {
		return errors.Errorf("empty htmlId passed to plots.AppendFig(\"\", fig)")
	}
	return displayFigToId(htmlId, fig)
}

// displayFigToId implements DisplayFig and AppendFig.
func displayFigToId(elementId string, fig *grob.Fig) error {
	// Create a unique div.
	divId := gonbui.UniqueId()
	divContent := fmt.Sprintf(`<div id="%s"></div>`, divId)
	if elementId == "" {
		gonbui.DisplayHTML(divContent)
	} else {
		dom.Append(elementId, divContent)
	}

	// Encode figure.
	figBytes, err := json.Marshal(fig)
	if err != nil {
		return errors.Wrapf(err, "failed to marshal Json to use with plotly")
	}

	// Run in plotly.
	runJS := fmt.Sprintf(`
	if (!module) {
		module = window.Plotly;
	}
	let data = JSON.parse('%s');
	module.newPlot('%s', data);
`, figBytes, divId)

	if elementId == "" {
		err = dom.LoadScriptOrRequireJSModuleAndRun("plotly", PlotlySrc, map[string]string{"charset": "utf-8"}, runJS)
	} else {
		err = dom.LoadScriptOrRequireJSModuleAndRunTransient("plotly", PlotlySrc, map[string]string{"charset": "utf-8"}, runJS)
	}
	if err != nil {
		return err
	}
	return nil
}
