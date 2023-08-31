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
	case "comm_info":
		// https://jupyter-client.readthedocs.io/en/latest/messaging.html#comm-info

	case "comm_open":
		//

	case "comm_close":

	case "comm_msg":

	}
	return nil
}
