package nbtests

import (
	"github.com/stretchr/testify/require"
	"os"
	"testing"
)

// noTestComms tests `gonbui/comms` package, including communication in both ways.
func noTestComms(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration (nbconvert) test for short tests.")
		return
	}
	f := executeNotebook(t, "comms")
	err := Check(f,
		Sequence(Match(
			OutputLine(5),
			Separator,
			"sent 1",
			"got 2",
			"sent 2",
			"got 3",
			"sent 3",
			"got 4",
			"sent 4",
			"closed",
			"done",
			Separator)),
		*flagPrintNotebook)

	require.NoError(t, err)
	require.NoError(t, f.Close())
	require.NoError(t, os.Remove(f.Name()))
}
