package kernel

import (
	_ "embed"
	"encoding/json"
	"os"
	"os/exec"
	"path"
	"runtime"
	"strings"

	"github.com/pkg/errors"
	"k8s.io/klog/v2"
)

// JupyterDataDirEnv is the name of the environment variable pointing to
// the data files for Jupyter, including kernel configuration.
const JupyterDataDirEnv = "JUPYTER_DATA_DIR"

// Logo file, to install with kernel.
// See image copyright in bottom of README.md file.

//go:embed Go_gopher_favicon.svg
var logoSVG []byte

// jupyterKernelConfig is the Jupyter configuration to be
// converted to a `kernel.json` file under `~/.local/share/jupyter/kernels/gonb`
// (or `${HOME}/Library/Jupyter/kernels/` in Macs)
// See details in: https://jupyter-client.readthedocs.io/en/latest/kernels.html#kernelspecs
type jupyterKernelConfig struct {
	Argv          []string          `json:"argv"`
	DisplayName   string            `json:"display_name"`
	Language      string            `json:"language"`
	InterruptMode string            `json:"interrupt_mode"`
	Env           map[string]string `json:"env"`
}

// Install gonb in users local Jupyter configuration, making it available. It assumes
// the kernel is implemented by the same binary calling this function (os.Args[0])
// and that the flag to pass the `connection_file` is `--kernel`.
//
// If the binary is under `/tmp` (or if forceCopy is true), it is copied to the location of
// the kernel configuration, and that copy is used.
//
// If forceDeps is true, installation will succeed even with missing dependencies.
//
// Documentation: https://jupyter-client.readthedocs.io/en/latest/kernels.html#kernelspecs
func Install(extraArgs []string, forceDeps, forceCopy bool) error {
	gonbPath, err := os.Executable()
	if err != nil {
		return errors.Wrapf(err, "Failed to find path to GoNB binary")
	}
	config := jupyterKernelConfig{
		Argv:          []string{gonbPath, "--kernel", "{connection_file}"},
		DisplayName:   "Go (gonb)",
		Language:      "go",
		InterruptMode: "message", // "message" (a `interrupt_request` is sent) or "signal" (using SIGINT signal)
		Env:           make(map[string]string),
	}
	if len(extraArgs) > 0 {
		config.Argv = append(config.Argv, extraArgs...)
	}

	// Jupyter configuration directory for gonb.
	home := os.Getenv("HOME")
	jupyterDataDir := os.Getenv(JupyterDataDirEnv)
	if jupyterDataDir == "" {
		switch runtime.GOOS {
		case "linux", "openbsd", "freebsd", "netbsd", "illumos", "solaris":
			jupyterDataDir = path.Join(home, ".local/share/jupyter")
		case "darwin":
			jupyterDataDir = path.Join(home, "Library/Jupyter")
		default:
			return errors.Errorf("Unknown OS %q: not sure where to install GoNB kernel -- set the environment %q to force a location.", runtime.GOOS, JupyterDataDirEnv)
		}
	}
	kernelDir := path.Join(jupyterDataDir, "/kernels/gonb")
	if err := os.MkdirAll(kernelDir, 0755); err != nil {
		return errors.WithMessagef(err, "failed to create configuration directory %q", kernelDir)
	}

	// If the binary is in `/tmp` or `/var/folders` or a cache directory, then presumably it is a temporary compilation
	// of Go binary. We then make a copy of the binary (since it will be deleted) to the configuration
	// directory -- otherwise we just point to the current binary.
	cacheDir, cacheErr := os.UserCacheDir()
	if forceCopy ||
		strings.HasPrefix(os.Args[0], "/tmp/") ||
		strings.HasPrefix(os.Args[0], "/var/folders") ||
		(cacheErr == nil && strings.HasPrefix(os.Args[0], cacheDir)) {
		newBinary := path.Join(kernelDir, "gonb")
		// Move the previous version out of the way.
		if _, err := os.Stat(newBinary); err == nil {
			err = os.Rename(newBinary, newBinary+"~")
			if err != nil {
				return errors.WithMessagef(err, "failed to rename old binary from %s to %s~", newBinary, newBinary)
			}
		}

		err := copyFile(newBinary, os.Args[0])
		if err != nil {
			return errors.WithMessagef(err, "failed to copy temporary binary from %q to %q", os.Args[0], newBinary)
		}
		config.Argv[0] = newBinary
	}

	// Create kernel.json.
	configPath := path.Join(kernelDir, "kernel.json")
	f, err := os.Create(configPath)
	if err != nil {
		return errors.WithMessagef(err, "failed to create configuration file %q", configPath)
	}
	encoder := json.NewEncoder(f)
	//encoder.SetIndent("", "  ")
	if err := encoder.Encode(&config); err != nil {
		return errors.WithMessagef(err, "failed to write configuration file %q", configPath)
	}
	if err := f.Close(); err != nil {
		return errors.WithMessagef(err, "failed to write configuration file %q", configPath)
	}
	klog.Infof("Go (gonb) kernel configuration installed in %q.\n", configPath)

	// Create `logo-svg.svg`.
	logoPath := path.Join(kernelDir, "logo-svg.svg")
	err = os.WriteFile(logoPath, logoSVG, 0755)
	if err != nil {
		return errors.WithMessagef(err, "failed to install logo file %q", logoPath)
	}

	// Check that goimports and gopls are installed.
	_, err = exec.LookPath("goimports")
	if err == nil {
		_, err = exec.LookPath("gopls")
	}
	if err != nil {
		msg := `
Program goimports and/or gopls are not installed. They are required dependencies,
and generally are standard Go toolkit packages. You can install them with:

go install golang.org/x/tools/cmd/goimports@latest
go install golang.org/x/tools/gopls@latest

`
		if !forceDeps {
			klog.Fatal(msg)
		}
		klog.Info(msg)
		err = nil
	}
	return nil
}

// copyFile, by reading all to memory -- not good for large files.
func copyFile(dst, src string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	err = os.WriteFile(dst, data, 0755)
	return err
}
