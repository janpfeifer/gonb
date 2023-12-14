package nbtests

import (
	"github.com/stretchr/testify/require"
	"k8s.io/klog/v2"
	"os"
	"testing"
)

// TestInputBoxes tests input boxes, created by `%with_inputs` special command and with `gonbui.RequestInput()`.
func TestInputBoxes(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration (nbconvert) test for short tests.")
		return
	}
	klog.Infof("GOCOVERDIR=%s", os.Getenv("GOCOVERDIR"))
	require.NoError(t, os.Setenv("GONB_GIT_ROOT", rootDir))

	notebook := "input_boxes"
	f := executeNotebookWithInputBoxes(t, notebook, []string{"foo", "bar", "42", "123456"})
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

			// Bash `%with_inputs` and `%with_password`.
			Match(OutputLine(4), Separator),
			Match("foo"),
			Match("str=foo"),
			Match("pass=bar", Separator),

			// gonbui.RequestInput:
			Match(OutputLine(5), Separator),
			Match("Enter: 42"),
			Match("int=42"),
			Match("Pin: ···"),
			Match("secret=123456", Separator),
		), *flagPrintNotebook)

	require.NoError(t, err)
	require.NoError(t, f.Close())
	require.NoError(t, os.Remove(f.Name()))
	clearNotebook(t, notebook)
}
