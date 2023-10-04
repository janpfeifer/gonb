package nbtests

import (
	"github.com/stretchr/testify/require"
	"os"
	"testing"
)

// TestDom tests `gonbui/dom` package.
func TestDom(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration (nbconvert) test for short tests.")
		return
	}
	notebook := "dom"
	f := executeNotebook(t, notebook)
	err := Check(f,
		Sequence(
			Match(
				OutputLine(2),
				Separator,
				"ok",
				Separator,
			),

			Match(OutputLine(4), Separator),
			// Some empty lines in between (empty transient outputs).
			Match(
				"This is a test!",
				"And a second test.",
				"This is a test!",
				"And a second test.",
				Separator),

			Match(OutputLine(5), Separator),
			Match("ok", Separator),
		), *flagPrintNotebook)

	require.NoError(t, err)
	require.NoError(t, f.Close())
	require.NoError(t, os.Remove(f.Name()))
	clearNotebook(t, notebook)
}
