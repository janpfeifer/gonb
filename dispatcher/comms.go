package dispatcher

import (
	"github.com/janpfeifer/gonb/goexec"
	"github.com/janpfeifer/gonb/kernel"
	"k8s.io/klog/v2"
)

// This file handles Comm messages (custom messages) incoming in the Shell channel
// for the Kernel (and IOPub channel in the Javascript/Wasm front-end).
//
// See details in:
// 1. Router (sent in the Shell socket):
//    https://jupyter-client.readthedocs.io/en/latest/messaging.html#comm-info
// 2. Custom Messages:
//    https://jupyter-client.readthedocs.io/en/latest/messaging.html#custom-messages
//
// Javascript front-end:
//
// - Session id: part of the filename of the json file passed to the kernel execution as an argument (--kernel=<file.json>).
//   It can be separated from the file name with a regexp like `^.*/kernel-([a-f0-9-]+).json$`.
// - Websocket connection: /api/kernels/cb142eeb-450a-47ed-9e9c-5c31aa8dba27/channels
// - JupyterServer Websocket Protocol: https://jupyter-server.readthedocs.io/en/latest/developers/websocket-protocols.html

// This file handles Comm messages (custom messages), coming from the Shell socket.
// Notice that in the front-end they are sent/received in the IOPub channel.
//
// See details in:
//  1. Router (sent in the Shell socket):
//     https://jupyter-client.readthedocs.io/en/latest/messaging.html#comm-info
//  2. Custom Messages:
//     https://jupyter-client.readthedocs.io/en/latest/messaging.html#custom-messages
func handleComms(msg kernel.Message, goExec *goexec.State) error {
	msgType := msg.ComposedMsg().Header.MsgType
	if klog.V(2).Enabled() {
		klog.Infof("Comms message %q: %+v", msgType, msg.ComposedMsg())
	}

	switch msgType {
	case "comm_info_request":
		// https://jupyter-client.readthedocs.io/en/latest/messaging.html#comm-info

	case "comm_open":
		return goExec.Comms.HandleOpen(msg)

	case "comm_close":
		klog.Warningf("\"comm_close\" received, but not implemented -- likely there is no impact.")
		return nil

	case "comm_msg":
		return goExec.Comms.HandleMsg(msg)

	}
	return nil
}
