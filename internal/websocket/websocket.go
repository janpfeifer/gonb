// Package websocket includes the Javascript code to establish a communication channel
// using WebSockets between the browser (the front-end) to the GoNB kernel -- and that
// it then bridged in GoNB to the user's Go code.
//
// See documentation in InteractiveFrontend.md for the full description of the whole process.
package websocket

import (
	"bytes"
	_ "embed"
	"github.com/janpfeifer/gonb/gonbui"
	"github.com/janpfeifer/gonb/gonbui/protocol"
	"os"
	"text/template"
)

//go:embed websocket.js
var webSocketConnectJs []byte

var tmplWebSocketConnectJs = template.Must(template.New("ws").Parse(
	string(webSocketConnectJs)))

// Javascript returns the javascript required to bootstrap the WebSocket library.
func Javascript() string {
	uuid := gonbui.UniqueId()
	data := struct {
		FormId, MsgId, LogId string
		KernelId             string
	}{
		FormId:   "form_id_" + uuid,
		MsgId:    "msg_id_" + uuid,
		LogId:    "log_id_" + uuid,
		KernelId: os.Getenv(protocol.GONB_JUPYTER_KERNEL_ID_ENV),
	}
	var buf bytes.Buffer
	err := tmplWebSocketConnectJs.Execute(&buf, data)
	if err != nil {
		panic(err)
	}
	return buf.String()
}
