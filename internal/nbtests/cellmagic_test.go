package nbtests

import (
	"fmt"
	"github.com/stretchr/testify/require"
	"k8s.io/klog/v2"
	"os"
	"path"
	"testing"
)

// TestWritefile tests that `%%writefile` tests that `%%writefile` works
func TestWritefile(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration (nbconvert) test for short tests.")
		return
	}
	klog.Infof("GOCOVERDIR=%s", os.Getenv("GOCOVERDIR"))

	// Create directory where to write the file, and set TEST_DIR env variable.
	testDir, err := os.MkdirTemp("", "gonb_nbtests_writefile_")
	require.NoError(t, err)
	require.NoError(t, os.Setenv("TEST_DIR", testDir+"/"))
	klog.Infof("TEST_DIR=%s/", testDir)

	// Run notebook test.
	notebook := "writefile"
	f := executeNotebook(t, notebook)
	err = Check(f,
		Sequence(
			Match(
				OutputLine(1),
				Separator,
				fmt.Sprintf(`Cell contents written to "%s/poetry.txt".`, testDir),
				Separator,
			),
			Match(
				OutputLine(2),
				Separator,
				fmt.Sprintf(`Cell contents appended to "%s/poetry.txt".`, testDir),
				Separator,
			),
		), *flagPrintNotebook)

	require.NoError(t, err)
	require.NoError(t, f.Close())
	require.NoError(t, os.Remove(f.Name()))
	clearNotebook(t, notebook)

	// Checks the file was written.
	filePath := path.Join(testDir, "poetry.txt")
	contents, err := os.ReadFile(filePath)
	require.NoErrorf(t, err, "Failed to read file %q that should have been written by the notebook.", filePath)
	want := `Um trem-de-ferro é uma coisa mecânica,
mas atravessa a noite, a madrugada, o dia,
atravessou minha vida,
virou só sentimento.

Adélia Prado
`
	require.Equalf(t, want, string(contents), "Contents written to %q don't match", filePath)

	// Remove directory.
	require.NoError(t, os.RemoveAll(testDir))
}

// TestScript tests the cell magic `%%script` (and `%%bash` and `%%sh`).
// It requires `bc` to be installed -- I assume available in most unixes.
func TestScript(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration (nbconvert) test for short tests.")
		return
	}
	klog.Infof("GOCOVERDIR=%s", os.Getenv("GOCOVERDIR"))

	// Run notebook test.
	notebook := "script"
	f := executeNotebook(t, notebook)
	err := Check(f,
		Sequence(
			Match(
				OutputLine(1),
				Separator,
				"1 : a",
				"2 : b",
				"3 : c",
				Separator,
			),
			Match(
				OutputLine(2),
				Separator,
				"18",
				Separator,
			),
			Match(
				OutputLine(3),
				Separator,
				"19",
				Separator,
			),
			Match(
				OutputLine(4),
				Separator,
				"",
				"can only appear at the start", // ... can only appear ...
				"",
				Separator,
			),
		), *flagPrintNotebook)

	require.NoError(t, err)
	require.NoError(t, f.Close())
	require.NoError(t, os.Remove(f.Name()))
	clearNotebook(t, notebook)
}
