package nbtests

import (
	"github.com/stretchr/testify/require"
	"os"
	"testing"
)

// TestComms tests `gonbui/comms` package, including communication in both ways.
func TestComms(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration (nbconvert) test for short tests.")
		return
	}
	notebook := "comms"
	f := executeNotebook(t, notebook)
	err := Check(f,
		Sequence(
			Match(OutputLine(5), Separator),
			// Some empty lines in between (empty transient outputs).
			Match(
				"sent 1",
				"got 2",
				"sent 2",
				"got 3",
				"sent 3",
				"got 4",
				"sent 4",
				"closed",
				"done",
				Separator),

			Match(OutputLine(6), Separator),
			Match("ok", Separator),
		), *flagPrintNotebook)

	require.NoError(t, err)
	require.NoError(t, f.Close())
	require.NoError(t, os.Remove(f.Name()))
	clearNotebook(t, notebook)
}
