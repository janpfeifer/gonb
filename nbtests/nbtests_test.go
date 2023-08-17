package nbtests

import (
	"flag"
	"fmt"
	"github.com/janpfeifer/gonb/kernel"
	"github.com/stretchr/testify/require"
	"io"
	"k8s.io/klog/v2"
	"os"
	"os/exec"
	"path"
	"testing"
)

var (
	flagPrintNotebook = flag.Bool("print_notebook", false, "print tested notebooks, useful if debugging unexpected results.")
	runArgs           = []string{}
	extraInstallArgs  = []string{"--logtostderr"}
)

func mustRemoveAll(dir string) {
	if dir == "" || dir == "/" {
		return
	}
	err := os.RemoveAll(dir)
	if err != nil {
		klog.Errorf("Failed to remove temporary directory %q: %+v", dir, err)
	}
}

func TestNotebooks(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping testing in short mode")
		return
	}
	tmpJupyterDir, err := InstallTmpGonbKernel(runArgs, extraInstallArgs)
	fmt.Printf("%s=%s\n", kernel.JupyterDataDirEnv, tmpJupyterDir)
	require.NoError(t, err)
	defer mustRemoveAll(tmpJupyterDir)

	// Prepare output file for nbconvert.
	tmpOutput, err := os.CreateTemp("", "gonb_nbtests_hello")
	require.NoError(t, err)
	tmpName := tmpOutput.Name()
	require.NoError(t, tmpOutput.Close())
	mustRemoveAll(tmpName)
	outputName := tmpName + ".asciidoc" // nbconvert adds this suffix.
	rootDir := GoNBRootDir()

	// Overwrite GOCOVERDIR if $REAL_GOCOVERDIR is given, because
	// -test.gocoverdir value is not propagated.
	// See: https://groups.google.com/g/golang-nuts/c/tg0ZrfpRMSg
	if goCoverDir := os.Getenv("REAL_GOCOVERDIR"); goCoverDir != "" {
		os.Setenv("GOCOVERDIR", goCoverDir)
	}

	// Loop over the notebooks to be tested.
	for _, notebook := range []struct {
		name   string
		testFn func(t *testing.T, r io.Reader)
	}{
		{"hello", testHello},
		{"functions", testFunctions},
		{"init", testInit},
	} {
		nbconvert := exec.Command(
			"jupyter", "nbconvert", "--to", "asciidoc", "--execute",
			"--output", tmpName,
			path.Join(rootDir, "examples", "tests", notebook.name+".ipynb"))
		nbconvert.Stdout, nbconvert.Stderr = os.Stderr, os.Stdout
		klog.Infof("Executing: %q", nbconvert)
		err = nbconvert.Run()
		require.NoError(t, err)
		f, err := os.Open(outputName)
		require.NoErrorf(t, err, "Failed to open the output of %q", nbconvert)
		notebook.testFn(t, f)
		require.NoError(t, f.Close())
		require.NoError(t, os.Remove(outputName))
	}
}

// testHello is called by TestNotebooks, and it takes as input the output of `nbconvert`
// to check for the expected results
func testHello(t *testing.T, r io.Reader) {
	err := Check(r,
		Sequence(
			Match("+*Out[1]:*+"),
			Match("Hello World!")),
		*flagPrintNotebook)
	require.NoError(t, err)
	return
}

// testFunctions is called by TestNotebooks, and it takes as input the output of `nbconvert`
// to check for the expected results
func testFunctions(t *testing.T, r io.Reader) {
	err := Check(r,
		Sequence(
			Match("+*Out[2]:*+"),
			Match("incr: x=2, y=4.14")),
		*flagPrintNotebook)
	require.NoError(t, err)
	return
}

// testInit is called by TestNotebooks, and it takes as input the output of `nbconvert`
// to check for the expected results
func testInit(t *testing.T, r io.Reader) {
	err := Check(r,
		Sequence(
			Match("+*Out[1]:*+"),
			Match("init_a"),

			Match("+*Out[2]:*+"),
			Match("init_a"),
			Match("init_b"),

			Match("+*Out[3]:*+"),
			Match("init: v0"),
			Match("init_a"),
			Match("init_b"),

			Match("+*Out[4]:*+"),
			Match("init: v1"),
			Match("init_a"),
			Match("init_b"),

			Match("+*Out[5]:*+"),
			Match("removed func init_a"),
			Match("removed func init_b"),

			Match("+*Out[6]:*+"),
			Match("init: v1"),
			Match("Done"),
		),
		*flagPrintNotebook)
	require.NoError(t, err)
	return
}
