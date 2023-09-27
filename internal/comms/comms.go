// Package comms implement the back-end (kernel) protocol over "Custom Messages"
// used to communicate with the front-end.
//
// This is the counter-part to the `websocket` package, which implements (in
// javascript) the front-end side.
//
// See details in `internal/websockets/README.md` file.
package comms

import (
	"fmt"
	"github.com/janpfeifer/gonb/common"
	"github.com/janpfeifer/gonb/gonbui/protocol"
	"github.com/janpfeifer/gonb/internal/jpyexec"
	"github.com/janpfeifer/gonb/internal/websocket"
	"github.com/janpfeifer/gonb/kernel"
	"github.com/pkg/errors"
	"k8s.io/klog/v2"
	"strings"
	"sync"
	"time"
)

// State for comms protocol. There is a singleton for the kernel, owned
// by goexec.State.
type State struct {
	// mu makes sure to protect the whole state.
	mu sync.Mutex

	// IsWebSocketInstalled indicates if the Javascript that runs a WebSocket that connects to JupyterServer
	// (and through that to GoNB) is installed in the front-end.
	// This is required for widgets to work: that's how they exchange update information.
	// Notice that having it installed doesn't mean yet the connection was established back -- that's what happens
	// usually, but it may take some cycles (or fail for any reason).
	IsWebSocketInstalled bool

	// TransientDisplayId is where javascript code was installed as a transient "display data".
	// It is randomly created when the websocket is installed.
	// The "transient" cell itself can be cleared after connection is established, to make sure the javascript
	// code is not saved.
	TransientDisplayId string

	// Opened indicates whether "comm_open" message has already been received.
	Opened bool

	// openedLatch is created by InstallWebSocket, and is triggered when it is
	// finally opened (or failed to open).
	openLatch *common.Latch

	// CommId created when the channel is opened from the front-end.
	CommId string

	// LastMsgTime is used to condition the need of a heartbeat, to access if the connection is still alive.
	LastMsgTime time.Time

	// HeartbeatPongLatch is triggered when we receive a heartbeat reply ("pong"), or when it times out.
	// A true value means it got the heartbeat, false means it didn't.
	// It is recreated everytime a HeartbeatPing is sent.
	HeartbeatPongLatch *common.LatchWithValue[bool]

	// AddressSubscriptions by the program being executed. Needs to be reset at every program
	// execution.
	AddressSubscriptions common.Set[string]

	// ProgramExecutor is a reference to the executor of the user's program (current cell).
	// It is used to dispatch comms coming from the front-end to the program.
	// This is set at the start of every cell execution, and reset to nil when the execution finishes.
	ProgramExecutor *jpyexec.Executor

	// ProgramExecMsg is the kernel.Message used to start the program.
	// This is set at the start of every cell execution, and reset to nil when the execution finishes.
	ProgramExecMsg kernel.Message
}

const (
	// CommOpenAckAddress is messaged in acknowledgement to a "comm_open" message.
	CommOpenAckAddress = "#comm_open_ack"

	// HeartbeatPingAddress is a protocol private message address used as heartbeat request.
	HeartbeatPingAddress = "#heartbeat/ping"

	// HeartbeatPongAddress is a protocol private message address used as heartbeat reply.
	HeartbeatPongAddress = "#heartbeat/pong"
)

// New creates and initializes an empty comms.State.
func New() *State {
	s := &State{
		IsWebSocketInstalled: false,
		AddressSubscriptions: make(common.Set[string]),
	}
	return s
}

// getFromJson extracts given key (split by "/") in Json parsed `map[string]any`
// values.
func getFromJson[T any](values map[string]any, key string) (value T, err error) {
	parts := strings.Split(key, "/")
	for ii, part := range parts {
		v, ok := values[part]
		if !ok {
			err = errors.Errorf("can't find path %q", strings.Join(parts[0:ii+1], "/"))
			return
		}
		if ii < len(parts)-1 {
			values, ok = v.(map[string]any)
			if !ok {
				err = errors.Errorf("path %q is not a sub-map (or object), instead it's a %T", strings.Join(parts[0:ii+1], "/"), v)
				return
			}
		} else {
			// Last item should be T.
			value, ok = v.(T)
			if !ok {
				err = errors.Errorf("path %q is not a %T, instead it's a %T", key, value, v)
				return
			}
		}
	}
	return
}

const (
	HeartbeatTimeout          = 500 * time.Millisecond
	HeartbeatRequestThreshold = 1 * time.Second

	WaitForConnectionTimeout = 3 * time.Second
)

// InstallWebSocket in the front-end, if not already installed.
// In the browser this is materialized as a global `gonb_comm` object, that handles
// communication.
//
// If it is supposedly installed, but there has been no communication > `HeartbeatRequestThreshold`
// (~ 1 second), it probes with a heartbeat "ping" to check.
//
// To install it sends a javascript is output as a transient output, so it's not saved.
//
// If it has already been installed, this does nothing.
func (s *State) InstallWebSocket(msg kernel.Message) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.installWebSocketLocked(msg)
}

// installWebSocketLocked implements InstallWebSocket, but assumes `s.mu` lock is
// already acquired.
func (s *State) installWebSocketLocked(msg kernel.Message) error {
	if s.IsWebSocketInstalled {
		// Already installed: if we haven't heard from the other side in more than HeartbeatRequestThreshold, then
		// we want to confirm with a ping.
		if time.Since(s.LastMsgTime) <= HeartbeatRequestThreshold {
			klog.V(1).Infof("comms.State.InstallWebSocket(): already installed")
			return nil
		}

		// Send heartbeat to confirm.
		if s.CommId != "" && s.Opened {
			klog.V(1).Infof("comms.State.InstallWebSocket(): confirm installation with heartbeat")
			heartbeat, err := s.sendHeartbeatPingLocked(msg, HeartbeatTimeout)
			if err != nil {
				return err
			}
			if heartbeat {
				// We got a heartbeat: websocket already installed, and connection is established.
				klog.V(1).Infof("comms.State.InstallWebSocket(): heartbeat pong received, all good.")
				return nil
			}
			klog.V(1).Infof("comms.State.InstallWebSocket(): heartbeat timed out and not heard back.")
		}

		// Likely we have a stale comms connection (e.g.: if the browser reloaded), we reset it and
		// follow with the re-install.
		s.CommId = ""
		s.IsWebSocketInstalled = false
		s.Opened = false
	}

	if s.openLatch == nil {
		// Install WebSocked javascript and create openLatch to wait it to open.
		// Notice if s.openLatch is created already, this is a concurrent call to
		// InstallWebSocket, and we can simply wait on it.
		klog.V(1).Infof("comms.State.InstallWebSocket(): running Javascript to install WebSocket...")
		s.openLatch = common.NewLatch()
		js := websocket.Javascript(msg.Kernel().JupyterKernelId)
		jsData := kernel.Data{
			Data:      make(kernel.MIMEMap, 1),
			Metadata:  make(kernel.MIMEMap),
			Transient: make(kernel.MIMEMap),
		}
		jsData.Data[string(protocol.MIMETextHTML)] = fmt.Sprintf("<script>%s</script>", js)
		s.TransientDisplayId = "gonb_websocket_" + common.UniqueId()
		jsData.Transient["display_id"] = s.TransientDisplayId
		err := kernel.PublishUpdateDisplayData(msg, jsData)
		if err != nil {
			klog.Error("Widgets won't work without a javascript WebSocket connection.")
			klog.Errorf("Failed to publish javascript to bootstrap GoNB websocket connection: %+v", err)
			return err
		}
		// Timeout for waiting to open.
		go func(l *common.Latch) {
			time.Sleep(WaitForConnectionTimeout)
			l.Trigger() // No-op if already triggered.
		}(s.openLatch)
	}

	// Wait for communication to be established.
	klog.V(2).Infof("comms.State.InstallWebSocket(): waiting for open request...")
	l := s.openLatch
	s.mu.Unlock()
	l.Wait()
	s.mu.Lock()
	if s.openLatch == l {
		s.openLatch = nil
	}

	// Check whether connection opening was successful.
	if !s.Opened {
		return errors.Errorf("InstallWebSocket failed: Javascript was sent to execution, but connection was not established. " +
			"Likely widgets won't work, since connection with front-end (browser can't be installed).")
	}

	s.IsWebSocketInstalled = true
	klog.V(1).Infof("Installed WebSocket javascript for GoNB connection (for widgets to work)")
	return nil
}

// HandleOpen message, with `msg_type` set to "comm_open".
//
// If message is incomplete, or apparently not addressed to us, it returns
// nil (no error) but also doesn't set communications as opened.
func (s *State) HandleOpen(msg kernel.Message) (err error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	defer func() {
		if s.openLatch != nil {
			// Confirms open was received and possibly replied.
			// This doesn't mean it succeeded, only that the attempt at establishing
			// the connection finished. Check `s.Opened` to see if it was correctly opened.
			s.openLatch.Trigger()
		}
	}()

	content, ok := msg.ComposedMsg().Content.(map[string]any)
	if !ok {
		klog.V(1).Infof("comms: ignored comm_open, no content in msg %+v", msg.ComposedMsg())
		return nil
	}

	var targetName string
	targetName, err = getFromJson[string](content, "target_name")
	if err != nil || targetName != "gonb_comm" {
		klog.V(1).Infof("comms: ignored comm_open, \"target_name\" not set or unknown (%q): %v", targetName, err)
		return nil
	}

	kernelId := msg.Kernel().JupyterKernelId
	var msgKernelId string
	msgKernelId, err = getFromJson[string](content, "kernel_id")
	if err != nil || msgKernelId != kernelId {
		klog.V(1).Infof("comms: ignored comm_open, field \"kernel_id\" not set or unknown (%q) -- "+
			"current kernelId is %q: %v", msgKernelId, kernelId, err)
		return nil
	}
	klog.V(2).Infof("comm_open: kernel_id=%q", kernelId)

	var commId string
	commId, err = getFromJson[string](content, "comm_id")
	if err != nil {
		klog.V(1).Infof("comms: ignored comm_open, \"comm_id\" not set: %+v", err)
		return nil
	}

	if s.Opened {
		// Close the previous connection if it is still open.
		err = s.closeLocked(msg)
		if err != nil {
			return
		}
	}

	// Erase javascript that installs WebSocket.
	jsData := kernel.Data{
		Data:      make(kernel.MIMEMap, 1),
		Metadata:  make(kernel.MIMEMap),
		Transient: make(kernel.MIMEMap),
	}
	jsData.Data[string(protocol.MIMETextHTML)] = "" // Empty.
	jsData.Transient["display_id"] = s.TransientDisplayId
	if err = kernel.PublishUpdateDisplayData(msg, jsData); err != nil {
		klog.Warningf("comms: failed to erase <div> block with javascript used to install websocket: %+v", err)
		err = nil
	}

	// Mark comms opened.
	s.CommId = commId
	s.LastMsgTime = time.Now()
	err = s.sendDataLocked(msg, map[string]any{
		"address": CommOpenAckAddress,
		"value":   true,
	})
	if err != nil {
		klog.Warningf("Failed to acknowledge open connection to front-end, likely widgets won't work!")
		err = errors.WithMessagef(err, "failed to reply %q to front-end", CommOpenAckAddress)
		return
	}
	s.Opened = true
	return nil
}

// HandleMsg is called by the dispatcher whenever a new `comm_msg` arrives from the front-end.
// It filters out messages with the wrong `comm_id`, handles protocol messages (heartbeat)
// and routes other messages.
func (s *State) HandleMsg(msg kernel.Message) (err error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	content, ok := msg.ComposedMsg().Content.(map[string]any)
	if !ok {
		klog.Warningf("comms: ignored comm_msg, no content in msg %+v", msg.ComposedMsg())
		return nil
	}

	var commId string
	commId, err = getFromJson[string](content, "comm_id")
	if err != nil {
		klog.Warningf("comms: ignored comm_msg, \"comm_id\" not set: %+v", err)
		return nil
	}
	if commId != s.CommId {
		klog.Warningf("comms: ignored comm_msg, \"comm_id\" (%q) different than the one we established the connection (%q)",
			commId, s.CommId)
		return nil
	}

	// Update connection alive signal.
	s.LastMsgTime = time.Now()

	// Parses address of message.
	var address string
	address, err = getFromJson[string](content, "data/address")
	if err != nil {
		klog.Warningf("comms: comm_msg did not set an \"content/data/address\" field: %+v", err)
		return nil
	}
	klog.V(2).Infof("comms: HandleMsg(address=%q)", address)

	switch address {
	case HeartbeatPongAddress:
		return s.handleHeartbeatPongLocked(msg)
	case HeartbeatPingAddress:
		return s.handleHeartbeatPingLocked(msg)
	default:
		var value any
		value, err = getFromJson[any](content, "data/value")
		if err != nil {
			klog.Warningf("comms: comm_msg did not set an \"content/data/value\" field: %+v", err)
			return nil
		}
		if s.deliverProgramSubscriptionsLocked(address, value) {
			klog.V(2).Infof("comms: HandleMsg(address=%q) delivered", address)
		} else {
			klog.V(1).Infof("comms: HandleMsg(address=%q) dropped -- usually because there were no recipients", address)
		}
		return nil
	}
}

// Close connection with front-end. It sends a "comm_close" message.
func (s *State) Close(msg kernel.Message) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.closeLocked(msg)
}

func (s *State) closeLocked(msg kernel.Message) error {
	if !s.Opened {
		klog.V(1).Infof("comms.State.Close(): it was not opened, nothing to do.")
		return nil
	}
	klog.V(1).Infof("comms.State.Close()")
	content := map[string]any{
		"comm_id": s.CommId,
	}
	err := msg.Reply("comm_close", content)
	s.CommId = "" // Erase comm_id.
	s.Opened = false
	s.IsWebSocketInstalled = false
	return err
}

// Send value to the given address in the front-end.
// This, along with subscribe, is the basic communication operation with the front-end.
// The value will be converted to JSON before being sent.
func (s *State) Send(msg kernel.Message, address string, value any) error {
	return s.sendData(msg, map[string]any{
		"address": address,
		"value":   value,
	})
}

// sendData using "comm_msg" message type.
func (s *State) sendData(msg kernel.Message, data map[string]any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sendDataLocked(msg, data)
}

// sendDataLocked is like sendData, but assumed lock is already acquired.
func (s *State) sendDataLocked(msg kernel.Message, data map[string]any) error {
	content := map[string]any{
		"comm_id": s.CommId,
		"data":    data,
	}
	klog.Infof("comms: sendData %+v", content)
	return msg.Publish("comm_msg", content)
	//return msg.Reply("comm_msg", content)
}

// SendHeartbeatAndWait sends a heartbeat request (ping) and waits for a reply within the given timeout.
// Returns true if a heartbeat was replied (pong) back, or false if it timed out.
// It returns an error if it failed to sendData the heartbeat message.
func (s *State) SendHeartbeatAndWait(msg kernel.Message, timeout time.Duration) (heartbeat bool, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.sendHeartbeatPingLocked(msg, timeout)
}

// sendHeartbeatPingLocked sends a heartbeat request (ping) and waits for a reply within the given timeout.
// Returns true if a heartbeat was replied (pong) back, or false if it timed out.
// It returns an error if it failed to sendData the heartbeat message.
//
// It unlocks the State while waiting, and reacquire the state before returning.
//
// If a heartbeat has already been sent, it won't create a new one, and the timeout time may not be honored, instead
// it will be used the one previously set.
func (s *State) sendHeartbeatPingLocked(msg kernel.Message, timeout time.Duration) (heartbeat bool, err error) {
	if s.HeartbeatPongLatch != nil {
		klog.Warningf("comms: heartbeat ping requested, but one is already running (it will be reused).")
	} else {
		klog.V(1).Infof("comms: sending heartbeat ping")
		data := map[string]any{
			"address": HeartbeatPingAddress,
			"value":   true,
		}
		err = s.sendDataLocked(msg, data)
		if err != nil {
			err = errors.WithMessagef(err, "failed to sendData heartbeat ping message")
			return
		}

		// Create latch to receive response, and a timeout trigger for the latch, in case we don't
		// get the reply in time.
		s.HeartbeatPongLatch = common.NewLatchWithValue[bool]()
		go func(l *common.LatchWithValue[bool]) {
			time.Sleep(timeout)
			// If latch has already triggered in the meantime, this trigger is discarded automatically.
			l.Trigger(false)
		}(s.HeartbeatPongLatch)
	}

	// Unlock and wait for reply (pong).
	latch := s.HeartbeatPongLatch
	s.mu.Unlock()
	heartbeat = latch.Wait() // true if heartbeat pong received, false if timed out.

	s.mu.Lock()
	// Clear the latch that we already used -- care in case in between some other process created a new latch.
	if s.HeartbeatPongLatch == latch {
		s.HeartbeatPongLatch = nil
	}
	return
}

// handleHeartbeatPong when one is received.
func (s *State) handleHeartbeatPongLocked(msg kernel.Message) error {
	if s.HeartbeatPongLatch != nil {
		klog.V(1).Infof("comms: heartbeat pong received, latch triggered")
		s.HeartbeatPongLatch.Trigger(true)
	} else {
		klog.Warningf("comms: heartbeat pong received but no one listening (no associated latch)!?")
	}
	return nil
}

// handleHeartbeatPing when one is received.
func (s *State) handleHeartbeatPingLocked(msg kernel.Message) (err error) {
	data := map[string]any{
		"address": HeartbeatPongAddress,
		"value":   true,
	}
	err = s.sendDataLocked(msg, data)
	if err != nil {
		err = errors.WithMessagef(err, "failed to reply a heartbeat pong message, widgets connection may be down")
	}
	return
}
