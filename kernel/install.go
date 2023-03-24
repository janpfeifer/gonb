package kernel

import (
	"encoding/json"
	"github.com/pkg/errors"
	"log"
	"os"
	"os/exec"
	"path"
	"runtime"
	"strings"
)

// jupyterKernelConfig is the Jupyter configuration to be
// converted to a `kernel.json` file under `~/.local/share/jupyter/kernels/gonb`
// (or `${HOME}/Library/Jupyter/kernels/` in Macs)
type jupyterKernelConfig struct {
	Argv        []string          `json:"argv"`
	DisplayName string            `json:"display_name"`
	Language    string            `json:"language"`
	Env         map[string]string `json:"env"`
}

// Install gonb in users local Jupyter configuration, making it available. It assumes
// the kernel is implemented by the same binary that is calling this function (os.Args[0])
// and that the flag to pass the `connection_file` is `--kernel`.
func Install(extraArgs []string, force bool) error {
	config := jupyterKernelConfig{
		Argv:        []string{os.Args[0], "--kernel", "{connection_file}"},
		DisplayName: "Go (gonb)",
		Language:    "go",
		Env:         make(map[string]string),
	}
	if len(extraArgs) > 0 {
		config.Argv = append(config.Argv, extraArgs...)
	}

	// Jupyter configuration directory for gonb.
	home := os.Getenv("HOME")
	var configDir string
	switch runtime.GOOS {
	case "linux":
		configDir = path.Join(home, ".local/share/jupyter/kernels/gonb")
	case "darwin":
		configDir = path.Join(home, "Library/Jupyter/kernels/")
	default:
		return errors.Errorf("Unknown OS %q: not sure how to install GoNB.", runtime.GOOS)
	}
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return errors.WithMessagef(err, "failed to create configuration directory %q", configDir)
	}

	// If binary is in /tmp, presumably temporary compilation of Go binary,
	// make a copy of the binary (since it will be deleted) to the configuration
	// directory.
	if strings.HasPrefix(os.Args[0], "/tmp/") {
		newBinary := path.Join(configDir, "gonb")
		// Move previous version out of the way.
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
	configPath := path.Join(configDir, "kernel.json")
	f, err := os.Create(configPath)
	if err != nil {
		return errors.WithMessagef(err, "failed to create configuration file %q", configPath)
	}
	encoder := json.NewEncoder(f)
	//encoder.SetIndent("", "  ")
	if err := encoder.Encode(&config); err != nil {
		if err != nil {
			return errors.WithMessagef(err, "failed to write configuration file %q", configPath)
		}
	}
	if err := f.Close(); err != nil {
		if err != nil {
			return errors.WithMessagef(err, "failed to write configuration file %q", configPath)
		}
	}

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
		if force {
			log.Fatal(msg)
		}
		log.Printf(msg)
	}

	log.Printf("Go (gonb) kernel configuration installed in %q.\n", configPath)
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
