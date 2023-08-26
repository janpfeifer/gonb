package goexec

import (
	"github.com/janpfeifer/gonb/gonbui/protocol"
	"github.com/pkg/errors"
	"os"
	"path"
	"strconv"
)

// This file handles the compilation and execution of wasm.

const (
	JupyterSessionNameEnv = "JPY_SESSION_NAME"
	JupyterPidEnv         = "JPY_PARENT_PID"

	JupyterFilesSubdir = ".jupyter_files"
)

// Cached values for current kernel's WASM subdirectory and URL.
var wasmSubdir, wasmUrl string

// MakeWasmSubdir creates a subdirectory named `.wasm/<notebook name>/` in the
// same directory as the notebook.
//
// It also copies current Go compiler `wasm_exec.js` file to this directory, if
// it's not there already.
//
// It returns the full path to the subdir and the url to be used to refer to these
// files in the notebook HTML.
func (s *State) MakeWasmSubdir() (subdir, subdirUrl string, err error) {
	// Check if value already cached.
	if wasmSubdir != "" && wasmUrl != "" {
		return wasmSubdir, wasmUrl, nil
	}

	// Set and create `subdir`.
	var jupyterRoot string
	jupyterRoot, err = JupyterRootDirectory()
	if err != nil {
		return
	}
	subdir = path.Join(jupyterRoot, JupyterFilesSubdir, s.UniqueID)
	err = os.MkdirAll(subdir, 0777)
	if err != nil {
		err = errors.Wrapf(err, "failed to created subdirectory %q required to install WASM files", subdir)
		return
	}

	// Set `subdirUrl`.
	subdirUrl = path.Join("/files", JupyterFilesSubdir, s.UniqueID)

	// Copy over `wasm_exec.js` if needed.
	//if _, err := os.Stat()

	// Cache subdir and subdirUrl and set environment variables before returning.
	wasmSubdir = subdir
	wasmUrl = subdirUrl
	if err = os.Setenv(protocol.GONB_WASM_SUBDIR_ENV, subdir); err != nil {
		err = errors.Wrapf(err, "failed to set environment variable %q", protocol.GONB_WASM_SUBDIR_ENV)
		return
	}
	if err = os.Setenv(protocol.GONB_WASM_URL_ENV, subdirUrl); err != nil {
		err = errors.Wrapf(err, "failed to set environment variable %q", protocol.GONB_WASM_URL_ENV)
		return
	}
	return
}

var jupyterRootDirectory string

// JupyterRootDirectory returns Jupyter's root directory.
// This is needed to build the URL from where it serves static files.
//
// Unfortunately, this isn't directly provided by Jupyter.
// It does provide its PID number, but get the "cwd" (current-working-directory) differs
// for different OSes.
//
// See question here:
// https://stackoverflow.com/questions/46247964/way-to-get-jupyter-server-root-directory/58988310#58988310
func JupyterRootDirectory() (string, error) {
	if jupyterRootDirectory != "" {
		return jupyterRootDirectory, nil
	}

	pidStr := os.Getenv(JupyterPidEnv)
	if pidStr == "" {
		return "", errors.Errorf("cannot figure out Jupyter root directory, because environment variable %q is not set!?",
			JupyterPidEnv)
	}
	pid, err := strconv.Atoi(pidStr)
	if err != nil {
		return "", errors.Wrapf(err, "cannot parse a pid from environment variable %s=%q",
			JupyterPidEnv, pidStr)
	}

	cwd, err := CurrentWorkingDirectoryForPid(pid)
	if err != nil {
		return "", errors.Wrapf(err, "cannot read current working directrom from pid %s=%q",
			JupyterPidEnv, pidStr)
	}

	jupyterRootDirectory = cwd
	return jupyterRootDirectory, nil
}
