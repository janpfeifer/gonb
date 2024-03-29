// Package websocket includes the Javascript code to establish a communication channel
// using WebSockets between the browser (the front-end) to the GoNB kernel -- and that
// it then bridged in GoNB to the user's Go code.
//
// See documentation and examples of how to use in gonb/docs/FrontEndCommunication.md.
package websocket

import (
	"bytes"
	_ "embed"
	"text/template"
)

//go:embed websocket.js
var webSocketConnectJs []byte

var tmplWebSocketConnectJs = template.Must(template.New("ws").Parse(
	string(webSocketConnectJs)))

// Javascript returns the javascript required to bootstrap the WebSocket library.
// It takes as input the kernel id -- provided when the kernel (GoNB) is executed.
//
// If `verbose` is set, it enables verbose logging in the Javascript console on
// the status of the communication -- useful for debugging.
func Javascript(jupyterKernelId string, verbose bool) string {
	data := struct {
		KernelId string
		Verbose  bool
	}{
		KernelId: jupyterKernelId,
		Verbose:  verbose,
	}
	var buf bytes.Buffer
	err := tmplWebSocketConnectJs.Execute(&buf, data)
	if err != nil {
		panic(err)
	}
	return buf.String()
}
