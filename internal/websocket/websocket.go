// Package websocket includes the Javascript code to establish a communication channel
// using WebSockets between the browser (the front-end) to the GoNB kernel -- and that
// it then bridged in GoNB to the user's Go code.
//
// See documentation in InteractiveFrontend.md for the full description of the whole process.
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
func Javascript(jupyterKernelId string) string {
	data := struct {
		KernelId string
	}{
		KernelId: jupyterKernelId,
	}
	var buf bytes.Buffer
	err := tmplWebSocketConnectJs.Execute(&buf, data)
	if err != nil {
		panic(err)
	}
	return buf.String()
}
