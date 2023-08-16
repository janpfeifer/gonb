// Package nbtests holds tools to run functional tests for GoNB using
// `nbconvert`.
//
// The actual tests are instrumented in `nbtests_test.go`, but this package
// exports several tools that would allow one to instrument tests for their
// own notebooks elsewhere, if for some reason it is handy.
package nbtests

import (
	"github.com/janpfeifer/gonb/kernel"
	"github.com/pkg/errors"
	"k8s.io/klog/v2"
	"os"
	"os/exec"
	"path"
	"runtime"
)

func GoNBRootDir() string {
	_, rootDir, _, _ := runtime.Caller(0)
	rootDir = path.Dir(path.Dir(rootDir))
	return rootDir
}

// InstallTmpGonbKernel will create a temporary directory, set the environment
// variable JUPYTER_DATA_DIR to that directory, and compile and install
// GoNB to that directory, so it will be used by `nbconvert`.
//
// Parameters:
//   - `runArgs` are passed to `go run --cover <runArgs> .` command, executed from GoNB's
//     root directory.
//   - `extraInstallArgs` are passed to the `gonb --install` execution.
//     Typical values here include `--logtostderr`, and `--vmodule=...` for verbose output.
//
// Notice that this precludes concurrent testing of various versions of GoNB in
// the same process, since it relies on this one global environment
// variable (JUPYTER_DATA_DIR) -- but one can still run different processes
// concurrently.
func InstallTmpGonbKernel(runArgs, extraInstallArgs []string) (tmpJupyterDir string, err error) {
	// Create and configure temporary jupyter directory.
	tmpJupyterDir, err = os.MkdirTemp("", "gonb_nbtests_jupyter")
	if err != nil {
		err = errors.Wrap(err, "failed to create temporary directory")
		return
	}
	err = os.Setenv(kernel.JupyterDataDirEnv, tmpJupyterDir)
	if err != nil {
		err = errors.Wrapf(err, "failed to set %q", kernel.JupyterDataDirEnv)
		return
	}

	// Run installation:
	rootDir := GoNBRootDir()
	args := []string{"run", "--cover"}
	args = append(args, runArgs...)
	args = append(args, ".", "--install")
	args = append(args, extraInstallArgs...)
	cmd := exec.Command("go", args...)
	cmd.Dir = rootDir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	klog.Infof("Executing: %q", cmd)
	err = cmd.Run()
	if err != nil {
		err = errors.Wrapf(err, "failed to compile and install GoNB with %q", cmd)
		return
	}
	return
}
