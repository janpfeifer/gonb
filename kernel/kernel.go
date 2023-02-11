// Package kernel handles the lower level communication with the Jupyter client.
//
// It creates the sockets, encoding, validation, etc.
//
// Reference documentation:
// https://jupyter-client.readthedocs.io/en/latest/messaging.html
package kernel

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/pkg/errors"
	"log"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"

	"github.com/go-zeromq/zmq4"
)

var (
	// ProtocolVersion defines the Jupyter protocol version.
	ProtocolVersion = "5.0"
)

const (
	StatusStarting = "starting"
	StatusBusy     = "busy"
	StatusIdle     = "idle"
)

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
	shell, stdin, control chan *Message

	// Wait group for the various polling goroutines.
	pollingWait sync.WaitGroup

	// ExecCounter is incremented each time we run user code in the notebook.
	ExecCounter int

	// Channel where interruption is received.
	sigintC chan os.Signal

	// Interrupted indicates whether shell currently being executed was Interrupted.
	Interrupted atomic.Bool

	// stdinMsg holds the Message that last asked from input from stdin (Message.PromptInput).
	stdinMsg *Message
	stdinFn  OnInputFn // Callback when stdin input is received.
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
	close(k.stop)
}

// HandleInterrupt will configure the kernel to listen to the system SIGINT,
// sent by default by Jupyter to indicate an interruption of the kernel
// (similar to a Control+C).
//
// So instead of the kernel dying, it will recover, and where appropriate
// interrupt other sub-processes it may have spawned.
func (k *Kernel) HandleInterrupt() {
	if k.sigintC == nil {
		k.sigintC = make(chan os.Signal, 1)
		signal.Notify(k.sigintC, os.Interrupt)
		go func() {
			for {
				select {
				case <-k.sigintC:
					k.Interrupted.Store(true)
					log.Printf("INTERRUPT received")
				case <-k.stop:
					break // Kernel stopped.
				}
			}
			signal.Reset(os.Interrupt)
			k.sigintC = nil
		}()
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
// a command. For Message.PipeExecToJupyterWithInput to work, you need
// to call Message.DeliverInput() on these messages.
func (k *Kernel) Stdin() <-chan *Message {
	return k.stdin
}

// Shell returns the reading channel where incoming shell messages
// are received.
//
// One should also select for the Kernel.StoppedChan, to check if
// connection to kernel was closed.
func (k *Kernel) Shell() <-chan *Message {
	return k.shell
}

// Control returns the reading channel where incoming control messages
// are received.
//
// One should also select for the Kernel.StoppedChan, to check if
// connection to kernel was closed.
func (k *Kernel) Control() <-chan *Message {
	return k.control
}

// NewKernel builds and start a kernel. Various goroutines are started to poll
// for incoming messages. It automatically handles the heartbeat.
//
// Incoming messages should be listened to using Kernel.Stdin, Kernel.Shell and
// Kernel.Control.
//
// The Kernel can be stopped by calling Kernel.Stop. And one can wait for the
// kernel clean up of the various goroutines, after it is stopped, by calling
// kernel.ExitWait.
func NewKernel(connectionFile string) (*Kernel, error) {
	k := &Kernel{
		stop:    make(chan struct{}),
		shell:   make(chan *Message, 1),
		stdin:   make(chan *Message, 1),
		control: make(chan *Message, 1),
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

	// Set polling functions that will listen to the sockets and forward
	// messages (or errors) to the corresponding channels.
	poll := func(msgChan chan *Message, sck zmq4.Socket) {
		k.pollingWait.Add(1)
		go func() {
			defer close(msgChan)
			for {
				zmqMsg, err := sck.Recv()
				var msg *Message
				if err != nil {
					msg = &Message{Kernel: k, err: err}
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

	k.pollHeartbeat()
	poll(k.shell, k.sockets.ShellSocket.Socket)
	poll(k.stdin, k.sockets.StdinSocket.Socket)
	poll(k.control, k.sockets.ControlSocket.Socket)
	return k, nil
}

// pollHeartbeat starts a goroutine for handling heartbeat ping messages sent over the given
// `hbSocket`.
func (k *Kernel) pollHeartbeat() {
	// Start the handler that will echo any received messages back to the sender.
	k.pollingWait.Add(1)
	go func() {
		defer k.pollingWait.Done()
		var err error
		var msg zmq4.Msg
		for err == nil {
			msg, err = k.sockets.HBSocket.Socket.Recv()
			log.Printf("\tHeartbeat received.")
			if k.IsStopped() {
				return
			}
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
		log.Printf("*** Kernel heartbeat failed: %+v", err)
		log.Printf("*** Stopping kernel")
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
// a ComposedMsg struct and a slice of return identities. This includes verifying the
// message signature.
func (k *Kernel) FromWireMsg(zmqMsg zmq4.Msg) *Message {
	parts := zmqMsg.Frames
	signKey := k.sockets.Key
	m := &Message{Kernel: k}

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
			m.err = errors.WithMessagef(&InvalidSignatureError{}, "while decoding received message")
			return m
		}
		if !hmac.Equal(mac.Sum(nil), signature) {
			m.err = errors.WithMessagef(&InvalidSignatureError{}, "invalid signature of received message, doesn't match secret key used during initialization")
			return m
		}
	}

	// Unmarshal contents.
	var err error
	err = json.Unmarshal(parts[i+2], &m.Composed.Header)
	if err != nil {
		m.err = errors.WithMessagef(err, "while decoding ComposedMsg.Header")
		return m
	}
	err = json.Unmarshal(parts[i+3], &m.Composed.ParentHeader)
	if err != nil {
		m.err = errors.WithMessagef(err, "while decoding ComposedMsg.ParentHeader")
		return m
	}
	err = json.Unmarshal(parts[i+4], &m.Composed.Metadata)
	if err != nil {
		m.err = errors.WithMessagef(err, "while decoding ComposedMsg.Metadata")
		return m
	}
	err = json.Unmarshal(parts[i+5], &m.Composed.Content)
	if err != nil {
		m.err = errors.WithMessagef(err, "while decoding ComposedMsg.Content")
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
