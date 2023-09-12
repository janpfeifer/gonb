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
	"github.com/janpfeifer/gonb/gonbui"
	"github.com/janpfeifer/gonb/gonbui/protocol"
	"github.com/janpfeifer/gonb/internal/websocket"
	"github.com/janpfeifer/gonb/kernel"
	"github.com/pkg/errors"
	"k8s.io/klog/v2"
	"strings"
	"sync"
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

	// CommId created when the channel is opened from the front-end.
	CommId string
}

// New creates and initializes an empty comms.State.
func New() *State {
	s := &State{}
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

// InstallJavascript in the front end that open websocket for communication.
// The javascript is output as a transient output, so it's not saved.
//
// If it has already been installed, this does nothing.
func (s *State) InstallJavascript(msg kernel.Message) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.IsWebSocketInstalled {
		// Already installed.
		klog.V(1).Infof("comms.State.InstallJavascript(): already installed")
		return nil
	}
	js := websocket.Javascript()
	jsData := kernel.Data{
		Data:      make(kernel.MIMEMap, 1),
		Metadata:  make(kernel.MIMEMap),
		Transient: make(kernel.MIMEMap),
	}
	jsData.Data[string(protocol.MIMETextHTML)] = fmt.Sprintf("<script>%s</script>", js)
	s.TransientDisplayId = gonbui.UniqueID()
	jsData.Transient["display_id"] = s.TransientDisplayId
	err := kernel.PublishUpdateDisplayData(msg, jsData)
	//err := kernel.PublishJavascript(msg, js)
	if err == nil {
		s.IsWebSocketInstalled = true
		klog.V(1).Infof("Installed WebSocket javascript for GoNB connection (for widgets to work), waiting for connection")
	} else {
		klog.Error("Widgets won't work without a javascript WebSocket connection.")
		klog.Errorf("Failed to publish javascript to bootstrap GoNB websocket connection: %+v", err)
	}
	return err
}

// HandleOpen message, with `msg_type` set to "comm_open".
//
// If message is incomplete, or apparently not addressed to us, it returns
// nil (no error) but also doesn't set communications as opened.
func (s *State) HandleOpen(msg kernel.Message) (err error) {
	s.mu.Lock()
	defer s.mu.Unlock()

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
	s.Opened = true
	return s.send(msg, map[string]any{
		"comm_open_ack": true,
	})
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

// send using "comm_msg" message type.
func (s *State) send(msg kernel.Message, data map[string]any) error {
	content := map[string]any{
		"comm_id": s.CommId,
		"data":    data,
	}
	return msg.Reply("comm_msg", content)
}
