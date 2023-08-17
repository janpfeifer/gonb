// Package nbtests holds tools to run functional tests for GoNB using
// `nbconvert`.
//
// The actual tests are instrumented in `nbtests_test.go`, but this package
// exports several tools that would allow one to instrument tests for their
// own notebooks elsewhere, if for some reason it is handy.
package nbtests

import (
	"bufio"
	"fmt"
	"github.com/janpfeifer/gonb/kernel"
	"github.com/pkg/errors"
	"io"
	"k8s.io/klog/v2"
	"os"
	"os/exec"
	"path"
	"runtime"
	"strings"
)

const Separator = "----" // String used as separator by `nbconvert` in text mode.

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

// ExpectFn is a function that checks for expectations of a line of input.
// It should return true, if the expectation was matched, false if not yet, or
// return an error if there was something wrong with the line and it the testing
// should fail immediately.
//
// When the input is finished, ExpectFn is called with `eof=true`, in which case
// it should either return true or an error.
//
// See the following functions that return `ExpectFn` that can be used:
//
// Match()
// Sequence()
type ExpectFn func(line string, eof bool) (done bool, err error)

// Check that the input given in reader matches the `want` expectation.
// Returns nil is expectation was matched, or an error with the failed match description.
//
// If `print` is set, it also prints the lines read from `r`.
func Check(r io.Reader, expectation ExpectFn, print bool) error {
	byLine := bufio.NewScanner(r)
	for byLine.Scan() {
		line := byLine.Text()
		if print {
			fmt.Println(line)
		}
		done, err := expectation(line, false)
		if err != nil {
			return err
		}
		if done {
			if print {
				// Drain rest of input.
				for byLine.Scan() {
					fmt.Println(byLine.Text())
				}
			}
			return nil
		}
	}
	if err := byLine.Err(); err != nil {
		return errors.Wrapf(err, "failed to read contents for Check()")
	}
	_, err := expectation("", true)
	return err
}

// Match returns an ExpectFn that checks if the input has the given string.
// If more than one string is given, they are expected to match consecutively,
// exactly one line after another.
func Match(search ...string) ExpectFn {
	current := 0
	if len(search) == 0 {
		panic("Match() requires at least one string.")
	}
	return func(line string, eof bool) (done bool, err error) {
		if eof {
			return false, errors.Errorf("Match(%q): search string #%d never matched", search, current)
		}
		found := strings.Contains(line, search[current])
		if !found {
			if current != 0 {
				return false, errors.Errorf("Match(%q): search string #%d not matched in sequence", search, current)
			}
			return false, nil
		}

		// Search string matched, move to next.
		current++
		if current < len(search) {
			// Still need to match following strings consecutively.
			return false, nil
		}
		return true, nil
	}
}

// Sequence returns an ExpectFn that checks whether each of the given expectations
// are matched in order. They don't need to be consecutive, that is, there can
// be unrelated lines in-between.
func Sequence(expectations ...ExpectFn) ExpectFn {
	current := 0
	if len(expectations) == 0 {
		panic("Sequence() requires at least one expectation.")
	}
	return func(line string, eof bool) (done bool, err error) {
		done, err = expectations[current](line, eof)
		if err != nil {
			return false, errors.WithMessagef(err, "Sequence(): at element #%d of %d", current, len(expectations))
		}
		if !done {
			return false, nil
		}
		// Bump to next expectation.
		current++
		if current < len(expectations) {
			if eof {
				// Check for EOF for all remaining expectations.
				for ii, e := range expectations[current:] {
					done, err = e(line, eof)
					if err != nil {
						return false, errors.WithMessagef(err, "Sequence(): at element #%d of %d", ii, len(expectations))
					}
				}
				// EOF is fine with all remaining expectations.
				return true, nil
			}
			// Current expectation matched, but not yet the full sequence, keep moving.
			return false, nil
		}

		// All expectations matched.
		return true, nil
	}
}
