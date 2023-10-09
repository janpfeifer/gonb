package nbtests

import (
	"github.com/stretchr/testify/require"
	"os"
	"testing"
)

// TestWidgets tests `gonbui/widgets` package.
func TestWidgets(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration (nbconvert) test for short tests.")
		return
	}
	notebook := "widgets"
	f := executeNotebook(t, notebook)
	err := Check(f,
		Sequence(
			Match(
				OutputLine(2),
				Separator,
				"ok",
				Separator,
			),

			// Some empty lines in between, or with a representation of the empty
			// transient divs.

			// Button
			Match(OutputLine(4), Separator),
			Match("clicked", Separator),

			// Slider
			Match(OutputLine(5), Separator),
			Match("widget tested ok", Separator),

			// Select (Dropdown)
			Match(OutputLine(6), Separator),
			Match("widget tested ok", Separator),
		), *flagPrintNotebook)

	require.NoError(t, err)
	require.NoError(t, f.Close())
	require.NoError(t, os.Remove(f.Name()))
	clearNotebook(t, notebook)
}
