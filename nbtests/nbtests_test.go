package nbtests

import (
	"flag"
	"fmt"
	"github.com/janpfeifer/gonb/kernel"
	"github.com/stretchr/testify/require"
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

func must(err error) {
	if err != nil {
		panic(err)
	}
}

func mustValue[T any](v T, err error) T {
	must(err)
	return v
}

func mustRemoveAll(dir string) {
	if dir == "" || dir == "/" {
		return
	}
	must(os.RemoveAll(dir))
}

var (
	rootDir, jupyterDir string
)

// setup for integration tests:
//
//	. Build a gonb binary with --cover (and set GOCOVERDIR).
//	. Set up a temporary jupyter kernel configuration, so that `nbconvert` will use it.
func setup() {
	rootDir = GoNBRootDir()

	// Overwrite GOCOVERDIR if $REAL_GOCOVERDIR is given, because
	// -test.gocoverdir value is not propagated.
	// See: https://groups.google.com/g/golang-nuts/c/tg0ZrfpRMSg
	if goCoverDir := os.Getenv("REAL_GOCOVERDIR"); goCoverDir != "" {
		must(os.Setenv("GOCOVERDIR", goCoverDir))
	}

	// Compile and install gonb binary as a local jupyter kernel.
	jupyterDir = mustValue(InstallTmpGonbKernel(runArgs, extraInstallArgs))
	fmt.Printf("%s=%s\n", kernel.JupyterDataDirEnv, jupyterDir)
}

// TestMain is used to set-up / shutdown needed for these integration tests.
func TestMain(m *testing.M) {
	setup()

	// Run tests.
	code := m.Run()

	// Clean up.
	mustRemoveAll(jupyterDir)
	os.Exit(code)
}

// executeNotebook (in `examples/tests`) and returns a reader to the output of the execution.
// It executes using `nbconvert` set to `asciidoc` (text) output.
func executedNotebook(t *testing.T, notebook string) *os.File {
	// Prepare output file for nbconvert.
	tmpOutput := mustValue(os.CreateTemp("", "gonb_nbtests_output"))
	nbconvertOutputName := tmpOutput.Name()
	must(tmpOutput.Close())
	must(os.Remove(nbconvertOutputName))
	nbconvertOutputPath := nbconvertOutputName + ".asciidoc" // nbconvert adds this suffix.

	nbconvert := exec.Command(
		"jupyter", "nbconvert", "--to", "asciidoc", "--execute",
		"--output", nbconvertOutputName,
		path.Join(rootDir, "examples", "tests", notebook+".ipynb"))
	nbconvert.Stdout, nbconvert.Stderr = os.Stderr, os.Stdout
	klog.Infof("Executing: %q", nbconvert)
	err := nbconvert.Run()
	require.NoError(t, err)
	f, err := os.Open(nbconvertOutputPath)
	require.NoErrorf(t, err, "Failed to open the output of %q", nbconvert)
	return f
}

func TestHello(t *testing.T) {
	f := executedNotebook(t, "hello")
	err := Check(f,
		Match("+*Out[1]:*+",
			Separator,
			"Hello World!",
			Separator),
		*flagPrintNotebook)

	require.NoError(t, err)
	require.NoError(t, f.Close())
	require.NoError(t, os.Remove(f.Name()))
}

func TestFunctions(t *testing.T) {
	f := executedNotebook(t, "functions")
	err := Check(f,
		Match(
			"+*Out[2]:*+",
			Separator,
			"incr: x=2, y=4.14",
			Separator,
		), *flagPrintNotebook)

	require.NoError(t, err)
	require.NoError(t, f.Close())
	require.NoError(t, os.Remove(f.Name()))
}

func TestInit(t *testing.T) {
	f := executedNotebook(t, "init")
	err := Check(f,
		Sequence(
			Match(
				"+*Out[1]:*+",
				Separator,
				"init_a",
				Separator,
			),
			Match(
				"+*Out[2]:*+",
				Separator,
				"init_a",
				"init_b",
				Separator,
			),
			Match(
				"+*Out[3]:*+",
				Separator,
				"init: v0",
				"init_a",
				"init_b",
				Separator,
			),
			Match(
				"+*Out[4]:*+",
				Separator,
				"init: v1",
				"init_a",
				"init_b",
				Separator,
			),
			Match(
				"+*Out[5]:*+",
				Separator,
				"removed func init_a",
				"removed func init_b",
				Separator),
			Match(
				"+*Out[6]:*+",
				Separator,
				"init: v1",
				"Done",
				Separator,
			),
		),
		*flagPrintNotebook)

	require.NoError(t, err)
	require.NoError(t, f.Close())
	require.NoError(t, os.Remove(f.Name()))
}

// TestGoWork tests support for `go.work` and `%goworkfix` as well as management
// of tracked directories.
func TestGoWork(t *testing.T) {
	f := executedNotebook(t, "gowork")
	err := Check(f,
		Sequence(
			Match(
				"+*Out[4]:*+",
				Separator,
				`Added replace rule for module "a.com/a/pkg" to local directory`,
				Separator,
			),
			Match(
				"+*Out[5]:*+",
				Separator,
				"module gonb_",
				"",
				"go ",
				"",
				"replace a.com/a/pkg => TMP_PKG",
				Separator,
			),
			Match(
				"+*Out[6]:*+",
				Separator,
				"List of files/directories being tracked",
				"",
				"/tmp/gonb_tests_gowork_",
				Separator,
			),
			Match(
				"+*Out[8]:*+",
				Separator,
				`Untracked "/tmp/gonb_tests_gowork_..."`,
				"",
				"No files or directory being tracked yet",
				Separator,
			),
		), *flagPrintNotebook)

	require.NoError(t, err)
	require.NoError(t, f.Close())
	require.NoError(t, os.Remove(f.Name()))
}
