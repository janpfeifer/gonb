// Package kernel handles the lower level communication with the Jupyter client.
//
// It creates the sockets, encoding, validation, etc.
//
// Reference documentation:
// https://jupyter-client.readthedocs.io/en/latest/messaging.html
package kernel

import (
	"container/list"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/go-zeromq/zmq4"
	"github.com/janpfeifer/gonb/common"
	"github.com/janpfeifer/gonb/gonbui/protocol"
	"github.com/janpfeifer/must"
	"github.com/pkg/errors"
	"k8s.io/klog/v2"
	"os"
	"os/signal"
	"regexp"
	"sync"
	"sync/atomic"
)

var (
	// ProtocolVersion defines the Jupyter protocol version. See differences here:
	// https://jupyter-client.readthedocs.io/en/stable/messaging.html#changelog
	//
	// Version >= 5.2 encodes cursor pos as one position per unicode character.
	// (https://jupyter-client.readthedocs.io/en/stable/messaging.html#cursor-pos-and-unicode-offsets)
	// But still Jupyter-Lab is using UTF-16 encoding for cursor-pos.
	ProtocolVersion = "5.4"
)

const (
	StatusStarting = "starting"
	StatusBusy     = "busy"
	StatusIdle     = "idle"
)

var _ = StatusStarting

// connectionInfo stores the contents of the kernel connection
// file created by Jupyter.
type connectionInfo struct {
	SignatureScheme string `json:"signature_scheme"`
	Transport       string `json:"transport"`
	StdinPort       int    `json:"stdin_port"`
	ControlPort     int    `json:"control_port"`
	IOPubPort       int    `json:"iopub_port"`
	HBPort          int    `json:"hb_port"`
	ShellPort       int    `json:"shell_port"`
	Key             string `json:"key"`
	IP              string `json:"ip"`
}

// SyncSocket wraps a zmq socket with a lock which should be used to control write access.
type SyncSocket struct {
	Socket zmq4.Socket
	Lock   sync.Mutex
}

// RunLocked locks socket and runs `fn`.
func (s *SyncSocket) RunLocked(fn func(socket zmq4.Socket) error) error {
	s.Lock.Lock()
	defer s.Lock.Unlock()
	return fn(s.Socket)
}

// SocketGroup holds the sockets needed to communicate with the kernel,
// and the key for message signing.
type SocketGroup struct {
	ShellSocket   SyncSocket
	ControlSocket SyncSocket
	StdinSocket   SyncSocket
	IOPubSocket   SyncSocket
	HBSocket      SyncSocket
	Key           []byte
}

type Kernel struct {
	// stop should be listened to after kernel creation. It is closed
	// when the Kernel is stopped (Kernel.Stop). Don't directly close it,
	// instead call Kernel.Stop.
	stop chan struct{}

	// Sockets connected to Jupyter client.
	sockets *SocketGroup

	// Channels with incoming messages.
	shell, stdin, control chan Message

	// Wait group for the various polling goroutines.
	pollingWait sync.WaitGroup

	// ExecCounter is incremented each time we run user code in the notebook.
	ExecCounter int

	// Channel where signals are received.
	signalsChan chan os.Signal

	// Interrupted indicates whether cell/shell currently being executed was Interrupted.
	Interrupted atomic.Bool

	// InterruptCond gets signaled whenever an interruption happens.
	interruptSubscriptions *list.List
	muSubscriptions        sync.Mutex

	// stdinMsg holds the MessageImpl that last asked from input from stdin (MessageImpl.PromptInput).
	stdinMsg *MessageImpl
	stdinFn  OnInputFn // Callback when stdin input is received.

	// JupyterKernelId is a unique id associated to the kernel by Jupyter.
	// It's different from the id created by goexec.State to identify the temporary Go
	// code compilation.
	// The use so far, besides to match Jupyter logs, is also to create URL to websocket port
	// for the session (see `dispatcher/comm.go`).
	JupyterKernelId string

	// KnownBlockIds are display data blocks with a "display_id" that have already been created, and
	// hence should be updated (instead of created anew) in calls to PublishUpdate
	KnownBlockIds common.Set[string]
}

// IsStopped returns whether the Kernel has been stopped.
func (k *Kernel) IsStopped() bool {
	select {
	case <-k.stop:
		return true
	default:
		return false
	}
}

// StoppedChan returns a channel that can be listened (`select`) to check when the
// kernel is stopped. The channel will be closed when the Kernel is
// stopped.
func (k *Kernel) StoppedChan() <-chan struct{} {
	return k.stop
}

// Stop the Kernel, indicating to all polling processes to quit.
func (k *Kernel) Stop() {
	klog.V(1).Infof("Kernel.Stop()")
	k.Interrupted.Store(true) // Also mark as interrupted.
	close(k.stop)
	err := k.sockets.ShellSocket.Socket.Close()
	if err != nil {
		klog.Errorf("Failed to close Shell socket: %v", err)
	}
	err = k.sockets.StdinSocket.Socket.Close()
	if err != nil {
		klog.Errorf("Failed to close Stdin socket: %v", err)
	}
	err = k.sockets.ControlSocket.Socket.Close()
	if err != nil {
		klog.Errorf("Failed to close Control socket: %v", err)
	}
	err = k.sockets.IOPubSocket.Socket.Close()
	if err != nil {
		klog.Errorf("Failed to close IOPub socket: %v", err)
	}
	err = k.sockets.HBSocket.Socket.Close()
	if err != nil {
		klog.Errorf("Failed to close Heartbeat socket: %v", err)
	}
}

// HandleInterrupt will configure the kernel to listen to the system SIGINT,
// sent by default by Jupyter to indicate an interruption of the kernel
// (similar to a Control+C).
//
// So instead of the kernel dying, it will recover, and where appropriate
// interrupt other subprocesses it may have spawned.
func (k *Kernel) HandleInterrupt() {
	// Handle Sigint (os.Interrupt): Control+C, Jupyter uses it to ask for the kernel
	// to interrupt execution of a program.
	// All other signals captured are assumed to mean that the kernel should exit.
	if k.signalsChan == nil {
		k.signalsChan = make(chan os.Signal, 1)
		signal.Notify(k.signalsChan, CaptureSignals...) // Signals we are interested in.
		go func() {
			// At exit reset notification.
			defer func() {
				signal.Reset(os.Interrupt)
				k.signalsChan = nil
			}()
			for {
				select {
				case sig := <-k.signalsChan:
					k.Interrupted.Store(true)
					k.CallInterruptSubscribers()
					klog.Infof("Signal %s received.", sig)
					if sig == os.Interrupt {
						// Simply interrupt running cells.
						continue
					}
					// Otherwise stop kernel.
					klog.Errorf("Signal %s triggers kernel stop.", sig)
					k.Stop()
				case <-k.stop:
					return // kernel stopped.
				}
			}
		}()
	}
}

// SubscriptionId is returned by [Kernel.SubscribeInterrupt], and can be used by [Kernel.UnsubscribeInterrupt].
type SubscriptionId *list.Element

// InterruptFn is called on its own goroutine.
type InterruptFn func(id SubscriptionId)

// SubscribeInterrupt registers `fn` to be called if any interruptions occur.
// It returns a [SubscriptionId] that needs to be used to unsubscribe to it later.
func (k *Kernel) SubscribeInterrupt(fn InterruptFn) SubscriptionId {
	k.muSubscriptions.Lock()
	defer k.muSubscriptions.Unlock()
	if klog.V(2).Enabled() {
		klog.Infof("SubscribeInterrupt(): %d elements", k.interruptSubscriptions.Len()+1)
	}
	return k.interruptSubscriptions.PushBack(fn)
}

// UnsubscribeInterrupt stops being called back in case of interruptions.
// It takes the `id` returned by [SubscribeInterrupt].
func (k *Kernel) UnsubscribeInterrupt(id SubscriptionId) {
	k.muSubscriptions.Lock()
	defer k.muSubscriptions.Unlock()

	if id.Value == nil {
		// Already unsubscribed.
		return
	}
	id.Value = nil
	k.interruptSubscriptions.Remove(id)
	if klog.V(2).Enabled() {
		klog.Infof("UnsubscribeInterrupt(): %d elements left", k.interruptSubscriptions.Len())
	}
}

// CallInterruptSubscribers in a separate goroutine each.
// Meant to be called when JupyterServer sends a kernel interrupt (either a SIGINT, or a `interrupt_request` message to interrupt).
func (k *Kernel) CallInterruptSubscribers() {
	k.muSubscriptions.Lock()
	defer k.muSubscriptions.Unlock()

	for e := k.interruptSubscriptions.Front(); e != nil; e = e.Next() {
		if e.Value == nil {
			continue
		}
		fn := e.Value.(InterruptFn)
		go fn(e) // run on separate goroutine.
	}
}

// ExitWait will wait for the kernel to be stopped and all polling
// goroutines to finish.
func (k *Kernel) ExitWait() {
	k.pollingWait.Wait()
}

// MsgOrError describes an incoming message or a communication error.
// If an error occur consider closing the Kernel with Kernel.Stop().
type MsgOrError struct {
	Msg zmq4.Msg
	Err error
}

// Stdin returns the reading channel where incoming stdin messages
// are received.
//
// One should also select for the Kernel.StoppedChan, to check if
// connection to kernel was closed.
//
// These are messages received from a console like input, while executing
// a command. For MessageImpl.PipeExecToJupyterWithInput to work, you need
// to call MessageImpl.DeliverInput() on these messages.
func (k *Kernel) Stdin() <-chan Message {
	return k.stdin
}

// Shell returns the reading channel where incoming shell messages
// are received.
//
// One should also select for the Kernel.StoppedChan, to check if
// connection to kernel was closed.
func (k *Kernel) Shell() <-chan Message {
	return k.shell
}

// Control returns the reading channel where incoming control messages
// are received.
//
// One should also select for the Kernel.StoppedChan, to check if
// connection to kernel was closed.
func (k *Kernel) Control() <-chan Message {
	return k.control
}

var reExtractJupyterSessionId = regexp.MustCompile(
	`^.*kernel-([0-9a-f-]+)\.json$`)

// New builds and start a kernel. Various goroutines are started to poll
// for incoming messages. It automatically handles the heartbeat.
//
// Incoming messages should be listened to using Kernel.Stdin, Kernel.Shell and
// Kernel.Control.
//
// The Kernel can be stopped by calling Kernel.Stop. And one can wait for the
// kernel clean up of the various goroutines, after it is stopped, by calling
// kernel.ExitWait.
//
// The `connectionFile` is created by Jupyter with information on which ports to
// connect to each socket. The path itself is also used to extract the JupyterKernelId
// associated with this instance of the kernel.
func New(connectionFile string) (*Kernel, error) {
	k := &Kernel{
		stop:    make(chan struct{}),
		shell:   make(chan Message, 1),
		stdin:   make(chan Message, 1),
		control: make(chan Message, 1),

		interruptSubscriptions: list.New(),
		KnownBlockIds:          make(common.Set[string]),
	}

	if matches := reExtractJupyterSessionId.FindStringSubmatch(connectionFile); len(matches) == 2 {
		k.JupyterKernelId = matches[1]
		must.M(os.Setenv(protocol.GONB_JUPYTER_KERNEL_ID_ENV, k.JupyterKernelId))
		klog.V(1).Infof("%s=%s", protocol.GONB_JUPYTER_KERNEL_ID_ENV, k.JupyterKernelId)
	} else {
		klog.Warningf("Could not parse Jupyter KernelId from kernel configuration path %q",
			connectionFile)
	}

	// Parse the connection info.
	var connInfo connectionInfo
	connData, err := os.ReadFile(connectionFile)
	if err != nil {
		return nil, errors.WithMessagef(err, "failed to open connection file %s", connectionFile)
	}
	if err = json.Unmarshal(connData, &connInfo); err != nil {
		return nil, errors.WithMessagef(err, "failed to read from connection file %s", connectionFile)
	}

	// Bind ZMQ sockets used for communication with Jupyter.
	k.sockets, err = bindSockets(connInfo)
	if err != nil {
		return nil, errors.WithMessagef(err, "failed to connect to sockets described in connection file %s", connectionFile)
	}

	k.pollHeartbeat()
	k.pollCommonSocket(k.shell, k.sockets.ShellSocket.Socket, "shell")
	k.pollCommonSocket(k.stdin, k.sockets.StdinSocket.Socket, "stdin")
	k.pollCommonSocket(k.control, k.sockets.ControlSocket.Socket, "control")
	return k, nil
}

// pollCommonSocket polls for messages from a socket, parses them, and sends them to msgChan.
//
// This function runs the loop of receiving messages, parsing and verifying from the wire
// protocol with FromWireMsg, and then passing the parsed "composed" message to the msgChan given.
//
// It also handles stopping and a clean-up, when the kernel is stopped.
//
// It runs on a separate Go routine, and uses `k.pollingWait` to account for it (it adds 1
// at the start, and calls `.Done()` when finished.
func (k *Kernel) pollCommonSocket(msgChan chan Message, sck zmq4.Socket, socketName string) {
	k.pollingWait.Add(1)
	go func() {
		klog.V(1).Infof("Polling of %q socket started.", socketName)
		defer func() {
			klog.V(1).Infof("Polling of %q socket finished.", socketName)
			k.pollingWait.Done()
			close(msgChan)
		}()
		for {
			zmqMsg, err := sck.Recv()
			var msg Message
			if err != nil {
				msg = &MessageImpl{kernel: k, err: err}
			} else {
				msg = k.FromWireMsg(zmqMsg)
			}
			select {
			case msgChan <- msg:
			case <-k.stop:
				return
			}
		}
	}()
}

// pollHeartbeat starts a goroutine for handling heartbeat ping messages sent over the given
// `hbSocket`.
func (k *Kernel) pollHeartbeat() {
	// Start the handler that will echo any received messages back to the sender.
	k.pollingWait.Add(1)
	klog.V(1).Infof("Polling of heartbeat socket started.")
	go func() {
		defer func() {
			klog.Infof("Polling of heartbeat socket finished.")
			k.pollingWait.Done()
		}()
		var err error
		var msg zmq4.Msg
		for err == nil {
			msg, err = k.sockets.HBSocket.Socket.Recv()
			if k.IsStopped() {
				return
			}
			klog.V(1).Infof("Heartbeat received.")
			if err != nil {
				err = errors.WithMessagef(err, "error reading heartbeat ping bytes")
				break
			}
			err = k.sockets.HBSocket.RunLocked(func(echo zmq4.Socket) error {
				if err := echo.Send(msg); err != nil {
					return errors.WithMessagef(err, "error sending heartbeat pong %q", msg.String())
				}
				return nil
			})
		}
		// Only breaks for loop if err != nil:
		klog.Errorf("*** kernel heartbeat failed: %+v", err)
		klog.Errorf("*** Stopping kernel")
		k.Stop()
	}()
}

// bindSockets creates and binds the ZMQ sockets through which the kernel communicates.
func bindSockets(connInfo connectionInfo) (sg *SocketGroup, err error) {
	// Initialize the socket group.
	ctx := context.Background()
	sg = &SocketGroup{
		// Set the message signing key.
		Key: []byte(connInfo.Key),

		// Create the shell socket, a request-reply socket that may receive messages from multiple frontend for
		// code execution, introspection, auto-completion, etc.
		ShellSocket: SyncSocket{Socket: zmq4.NewRouter(ctx)},

		// Create the control socket. This socket is a duplicate of the shell socket where messages on this channel
		// should jump ahead of queued messages on the shell socket.
		ControlSocket: SyncSocket{Socket: zmq4.NewRouter(ctx)},

		// Create the stdin socket, a request-reply socket used to request user input from a front-end. This is analogous
		// to a standard input stream.
		StdinSocket: SyncSocket{Socket: zmq4.NewRouter(ctx)},

		// Create the iopub socket, a publisher for broadcasting data like stdout/stderr output, displaying execution
		// results or errors, kernel status, etc. to connected subscribers.
		IOPubSocket: SyncSocket{Socket: zmq4.NewPub(ctx)},

		// Create the heartbeat socket, a request-reply socket that only allows alternating
		// receive-send (request-reply) calls. It should echo the byte strings it receives
		// to let the requester know the kernel is still alive.
		HBSocket: SyncSocket{Socket: zmq4.NewRep(ctx)},
	}

	// Bind the sockets.
	var addrFn func(portNum int) string
	switch connInfo.Transport {
	case "tcp":
		addrFn = func(portNum int) string {
			return fmt.Sprintf("tcp://%s:%d", connInfo.IP, portNum)
		}
	case "ipc":
		addrFn = func(portNum int) string {
			return fmt.Sprintf("ipc://%s-%d", connInfo.IP, portNum)
		}

	}
	portNums := []int{connInfo.ShellPort, connInfo.ControlPort, connInfo.StdinPort,
		connInfo.IOPubPort, connInfo.HBPort}
	sockets := []*SyncSocket{&sg.ShellSocket, &sg.ControlSocket, &sg.StdinSocket,
		&sg.IOPubSocket, &sg.HBSocket}
	socketName := []string{"shell-socket", "control-socket", "stdin-socket",
		"iopub-socket", "heartbeat-socket"}
	for ii, portNum := range portNums {
		address := addrFn(portNum)
		err = sockets[ii].Socket.Listen(address)
		if err != nil {
			return sg, errors.WithMessagef(err, fmt.Sprintf("failed to listen on %s", socketName[ii]))
		}
	}
	return
}

// FromWireMsg translates a multipart ZMQ messages received from a socket into
// a ComposedMsg struct and a slice of return identities.
// This includes verifying the message signature.
//
// This "Wire Protocol" of the messages is described here:
// https://jupyter-client.readthedocs.io/en/latest/messaging.html#the-wire-protocol
func (k *Kernel) FromWireMsg(zmqMsg zmq4.Msg) Message {
	parts := zmqMsg.Frames
	signKey := k.sockets.Key
	m := &MessageImpl{kernel: k}

	i := 0
	for string(parts[i]) != "<IDS|MSG>" {
		i++
	}
	m.Identities = parts[:i]

	// Validate signature.
	if len(signKey) != 0 {
		mac := hmac.New(sha256.New, signKey)
		for _, part := range parts[i+2 : i+6] {
			mac.Write(part)
		}
		signature := make([]byte, hex.DecodedLen(len(parts[i+1])))
		_, err := hex.Decode(signature, parts[i+1])
		if err != nil {
			m.err = errors.Wrapf(&InvalidSignatureError{}, "while hex-decoding received message")
			return m
		}
		if !hmac.Equal(mac.Sum(nil), signature) {
			m.err = errors.Wrapf(&InvalidSignatureError{}, "invalid check sum of received message, doesn't match secret key used during initialization")
			return m
		}
	}

	// Unmarshal contents.
	var err error
	err = json.Unmarshal(parts[i+2], &m.Composed.Header)
	if err != nil {
		m.err = errors.Wrapf(err, "while decoding ComposedMsg.Header")
		return m
	}
	err = json.Unmarshal(parts[i+3], &m.Composed.ParentHeader)
	if err != nil {
		m.err = errors.Wrapf(err, "while decoding ComposedMsg.ParentHeader")
		return m
	}
	err = json.Unmarshal(parts[i+4], &m.Composed.Metadata)
	if err != nil {
		m.err = errors.Wrapf(err, "while decoding ComposedMsg.Metadata")
		return m
	}
	err = json.Unmarshal(parts[i+5], &m.Composed.Content)
	if err != nil {
		m.err = errors.Wrapf(err, "while decoding ComposedMsg.Content")
		return m
	}
	return m
}

// ToWireMsg translates a ComposedMsg into a multipart ZMQ message ready to send, and
// signs it. This does not add the return identities or the delimiter.
func (k *Kernel) ToWireMsg(c *ComposedMsg) ([][]byte, error) {
	signKey := k.sockets.Key
	parts := make([][]byte, 5)

	header, err := json.Marshal(c.Header)
	if err != nil {
		return parts, err
	}
	parts[1] = header

	parentHeader, err := json.Marshal(c.ParentHeader)
	if err != nil {
		return parts, err
	}
	parts[2] = parentHeader

	if c.Metadata == nil {
		c.Metadata = make(map[string]interface{})
	}

	metadata, err := json.Marshal(c.Metadata)
	if err != nil {
		return parts, err
	}
	parts[3] = metadata

	content, err := json.Marshal(c.Content)
	if err != nil {
		return parts, err
	}
	parts[4] = content

	// Sign the message.
	if len(signKey) != 0 {
		mac := hmac.New(sha256.New, signKey)
		for _, part := range parts[1:] {
			mac.Write(part)
		}
		parts[0] = make([]byte, hex.EncodedLen(mac.Size()))
		hex.Encode(parts[0], mac.Sum(nil))
	}

	return parts, nil
}
