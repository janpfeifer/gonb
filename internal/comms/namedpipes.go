package comms

import (
	"github.com/janpfeifer/gonb/common"
	"github.com/janpfeifer/gonb/internal/jpyexec"
	"k8s.io/klog/v2"
)

// This file handles the communication with the named pipes created by jpyexec package.
// It implements the CommHandler interface defined there.

// Compile-time check that `*State` implements jpyexec.CommsHandler.
var _ jpyexec.CommsHandler = &State{}

// ProgramStart is called each time a program is being executed (the contents of a cell),
// which is configured to use named pipes (for front-end communication/widgets).
func (s *State) ProgramStart(exec *jpyexec.Executor) {
	klog.V(2).Infof("comms: ProgramStart()")
	s.AddressSubscriptions = make(common.Set[string])
	s.ProgramExecutor = exec
	s.ExecMsg = exec.Msg
}

// ProgramFinished is called when the program (cell execution) finishes.
func (s *State) ProgramFinished() {
	klog.V(2).Infof("comms: ProgramFinished()")
	s.AddressSubscriptions = make(common.Set[string])
	s.ExecMsg = nil
	s.ProgramExecutor = nil
}

// ProgramValueUpdateRequest handler, it implements jpyexec.CommsHandler.
// It sends a message with the update to the front-end.
//
// It also tries to install the WebSocket, if not yet installed.
func (s *State) ProgramValueUpdateRequest(address string, value any) {
	// Notice the program may end while handling this request, so we save the value
	// of the msg that will be used to complete the request, even if the program ends.
	msg := s.ExecMsg
	if msg == nil {
		klog.Infof("Failed to communicate with front-end. This seems to be a logic bug in "+
			"the program, where comms.State.ProgramStart() was not called before a request to "+
			"communication was made (address=%q)", address)
		return
	}
	if klog.V(2).Enabled() {
		klog.Infof("comms: ValueUpdate: address=%q, value=%v", address, value)
	}
	err := s.InstallJavascript(msg)
	if err != nil {
		klog.Infof("Failed to install WebSocket in front-end, used to communicate with programs, "+
			"in particular widgets -- those will not work. Error message: %+v", err)
		return
	}

	err = s.Send(msg, address, value)
	if err != nil {
		klog.Infof("Failed to send to value (%v) to address %q in the front-end -- widgets may mal-function. "+
			"Consider restart the GoNB kernel. Error message: %+v", value, address, err)
		return
	}
}

// ProgramReadValueRequest handler, it implements jpyexec.CommsHandler.
// It sends a message with a request to read the value from the address to the front-end.
//
// It also tries to install the WebSocket, if not yet installed.
func (s *State) ProgramReadValueRequest(address string) {
	// Notice the program may end while handling this request, so we save the value
	// of the msg that will be used to complete the request, even if the program ends.
	msg := s.ExecMsg
	if msg == nil {
		klog.Infof("Failed to communicate with front-end. This seems to be a logic bug in "+
			"the program, where comms.State.ProgramStart() was not called before a request to "+
			"communication was made (address=%q)", address)
		return
	}
	if klog.V(2).Enabled() {
		klog.Infof("comms: ReadValue: address=%q", address)
	}
}

// ProgramSubscribeRequest handler, it implements jpyexec.CommsHandler.
// It subscribes the program to receive updates on the given address.
//
// It also tries to install the WebSocket, if not yet installed.
func (s *State) ProgramSubscribeRequest(address string) {
	// Notice the program may end while handling this request, so we save the value
	// of the msg that will be used to complete the request, even if the program ends.
	msg := s.ExecMsg
	if msg == nil {
		klog.Infof("Failed to communicate with front-end. This seems to be a logic bug in "+
			"the program, where comms.State.ProgramStart() was not called before a request to "+
			"communication was made (address=%q)", address)
		return
	}
	if klog.V(2).Enabled() {
		klog.Infof("comms: SubscribeRequest: address=%q", address)
	}

}

// ProgramUnsubscribeRequest handler, it implements jpyexec.CommsHandler.
// It unsubscribes the program to receive updates on the given address.
//
// It also tries to install the WebSocket, if not yet installed.
func (s *State) ProgramUnsubscribeRequest(address string) {
	// Notice the program may end while handling this request, so we save the value
	// of the msg that will be used to complete the request, even if the program ends.
	msg := s.ExecMsg
	if msg == nil {
		klog.Infof("Failed to communicate with front-end. This seems to be a logic bug in "+
			"the program, where comms.State.ProgramStart() was not called before a request to "+
			"communication was made (address=%q)", address)
		return
	}
	if klog.V(2).Enabled() {
		klog.Infof("comms: SubscribeRequest: address=%q", address)
	}

}
