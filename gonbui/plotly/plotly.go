// Package plotly adds barebones support to Plotly (https://plotly.com/javascript/getting-started/)
// library.
//
// It's a simple wrapper to execute javascript code, assuming the Plotly library is loaded.
//
// It also offers a library around github.com/MetalBlueberry/go-plotly (itself a wrapper around
// Plotly) to integrate it in GoNB, see `DisplayFig` method.
//
// API is a first stab at it (experimental), and `UpdateHtml` for dynamic plots is not yet supported.
// Ideas are welcome here.
package plotly

import (
	"encoding/json"
	"fmt"
	grob "github.com/MetalBlueberry/go-plotly/graph_objects"
	"github.com/janpfeifer/gonb/gonbui"
	"github.com/pkg/errors"
)

// PlotlySrc is the source from where to download Plotly.
// If you have a local copy or an updated version of the library, change the value here.
var PlotlySrc = "https://cdn.plot.ly/plotly-2.29.1.min.js"

// DisplayFig as HTML output.
func DisplayFig(fig *grob.Fig) error {
	// Create a unique div.
	divId := gonbui.UniqueId()
	gonbui.DisplayHTML(fmt.Sprintf(`<div id="%s"></div>`, divId))

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

	err = gonbui.LoadScriptOrRequireJSModuleAndRun("plotly", PlotlySrc, map[string]string{"charset": "utf-8"}, runJS)
	if err != nil {
		return err
	}
	return nil
}
