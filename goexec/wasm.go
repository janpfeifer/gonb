package goexec

import (
	"bytes"
	"fmt"
	"github.com/janpfeifer/gonb/gonbui/protocol"
	"github.com/janpfeifer/gonb/kernel"
	"github.com/pkg/errors"
	"k8s.io/klog/v2"
	"os"
	"os/exec"
	"path"
	"strconv"
	"strings"
	"text/template"
)

// This file handles the compilation and execution of wasm.

const (
	JupyterSessionNameEnv = "JPY_SESSION_NAME"
	JupyterPidEnv         = "JPY_PARENT_PID"

	JupyterFilesSubdir = "jupyter_files"
	CompiledWasmName   = "gonb_cell.wasm"
)

// MakeWasmSubdir creates a subdirectory named `.wasm/<notebook name>/` in the
// same directory as the notebook, if it is not yet created.
//
// It also copies current Go compiler `wasm_exec.js` file to this directory, if
// it's not there already.
//
// Path and URL to access it are stored in s.WasmDir and s.WasmUrl.
func (s *State) MakeWasmSubdir() (err error) {
	// Check if value already cached.
	if s.WasmDir != "" && s.WasmUrl != "" {
		return nil
	}

	// Set and create `WasmDir`.
	var jupyterRoot string
	jupyterRoot, err = JupyterRootDirectory()
	if err != nil {
		return
	}
	s.WasmDir = path.Join(jupyterRoot, JupyterFilesSubdir, s.UniqueID)
	err = os.MkdirAll(s.WasmDir, 0777)
	if err != nil {
		err = errors.Wrapf(err, "failed to created subdirectory %q required to install WASM files", s.WasmDir)
		return
	}

	// Set `WasmUrl`.
	s.WasmUrl = path.Join("/files", JupyterFilesSubdir, s.UniqueID)

	// Copy over `wasm_exec.js` if needed.
	var wasmExecSrc string
	wasmExecSrc, err = GoRoot()
	if err != nil {
		err = errors.WithMessage(err, "failed to find GOROOT, needed to copy wasm_exec.js for WASM programs")
		return
	}
	klog.Infof("GOROOT=%q", goRoot)
	wasmExecSrc = path.Join(wasmExecSrc, "misc", "wasm", "wasm_exec.js")
	wasmExecDst := path.Join(s.WasmDir, "wasm_exec.js")

	var data []byte
	data, err = os.ReadFile(wasmExecSrc)
	if err != nil {
		err = errors.Wrapf(err, "failed to read '$GOROOT/misc/wasm/wasm_exec.js'")
		return
	}
	err = os.WriteFile(wasmExecDst, data, 0775)
	if err != nil {
		err = errors.Wrapf(err, "failed to write 'wasm_exec.js' to %q", wasmExecDst)
		return
	}

	// Set the environment variables with the directory/url.
	if err = os.Setenv(protocol.GONB_WASM_DIR_ENV, s.WasmDir); err != nil {
		err = errors.Wrapf(err, "failed to set environment variable %q", protocol.GONB_WASM_DIR_ENV)
		return
	}
	if err = os.Setenv(protocol.GONB_WASM_URL_ENV, s.WasmUrl); err != nil {
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

var goRoot string

func GoRoot() (string, error) {
	if goRoot != "" {
		return goRoot, nil
	}

	cmd := exec.Command("go", "env", "GOROOT")
	klog.Infof("Executing %q", cmd)
	cmd.Stderr = os.Stderr
	output, err := cmd.Output()
	if err != nil {
		return "", errors.Wrapf(err, "failed to find GOROOT")
	}
	goRoot = string(output)
	goRoot = strings.TrimSuffix(goRoot, "\n")
	return goRoot, nil
}

var runWasmTemplate = template.Must(template.New("wasm_exec").Parse(
	`<script src="{{.WasmExecJsUrl}}"></script>
<script>
var go_{{.Id}} = new Go();
 
WebAssembly.instantiateStreaming(fetch("{{.CompiledWasmUrl}}"), go_{{.Id}}.importObject).
	then((result) => { go_{{.Id}}.run(result.instance); });
</script>
<div id="{{.WasmDivId}}"></div>
`))

// ExecuteWasm expects `wasm_exec.js` and CompiledWasmName to be in the directory
// pointed to `s.WasmDir` already.
func (s *State) ExecuteWasm(msg kernel.Message) error {
	data := struct {
		Id, WasmExecJsUrl, CompiledWasmUrl, WasmDivId string
	}{
		Id:              s.UniqueID,
		WasmExecJsUrl:   path.Join(s.WasmUrl, "wasm_exec.js"),
		CompiledWasmUrl: path.Join(s.WasmUrl, CompiledWasmName),
		WasmDivId:       s.WasmDivId,
	}
	var buf bytes.Buffer
	err := runWasmTemplate.Execute(&buf, &data)
	if err != nil {
		return errors.Wrapf(err, "failed to generate javascript to bootstrap WASM")
	}
	js := buf.String()
	klog.V(2).Infof("WASM bootstrap code served:\n%s\n", js)
	return kernel.PublishDisplayDataWithHTML(msg, js)
}

// DeclareStringConst creates a const definition in `decls` for a string value.
func DeclareStringConst(decls *Declarations, name, value string) {
	decls.Constants[name] = &Constant{
		Cursor:          NoCursor,
		CellLines:       CellLines{},
		Key:             name,
		TypeDefinition:  "",
		ValueDefinition: fmt.Sprintf("%q", value),
		CursorInKey:     false,
		CursorInType:    false,
		CursorInValue:   false,
		Next:            nil,
		Prev:            nil,
	}
}

// DeclareVariable creates a variable definition in `decls`.
// `value` is copied verbatim, so any type of variable goes.
func DeclareVariable(decls *Declarations, name, value string) {
	decls.Variables[name] = &Variable{
		Cursor:          NoCursor,
		CellLines:       CellLines{},
		Key:             name,
		Name:            name,
		ValueDefinition: value,
	}
}

func (s *State) ExportWasmConstants(decls *Declarations) {
	DeclareStringConst(decls, "GonbWasmDir", s.WasmDir)
	DeclareStringConst(decls, "GonbWasmUrl", s.WasmUrl)
	DeclareStringConst(decls, "GonbWasmDivId", s.WasmDivId)
	DeclareVariable(decls, "GonbWasmArgs", fmt.Sprintf("%#v", s.Args))
}

func (s *State) RemoveWasmConstants(decls *Declarations) {
	delete(decls.Constants, "GonbWasmDir")
	delete(decls.Constants, "GonbWasmUrl")
	delete(decls.Constants, "GonbWasmDivId")
	delete(decls.Variables, "GonbWasmArgs")
}
