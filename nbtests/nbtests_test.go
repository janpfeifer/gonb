package nbtests

import (
	"bufio"
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
	runArgs          = []string{}
	extraInstallArgs = []string{"--logtostderr"}
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

	// Loop over the notebooks to be tested.
	for _, notebook := range []struct {
		name   string
		testFn func(t *testing.T, r io.Reader)
	}{
		{"hello", testHello},
		{"functions", testHello},
		{"init", testHello},
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
	byLine := bufio.NewScanner(r)
	for byLine.Scan() {
		line := byLine.Text()
		_ = line
		//fmt.Println(line)
	}
	return
}
