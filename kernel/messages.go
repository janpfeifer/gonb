package kernel

import (
	"fmt"
	"github.com/janpfeifer/gonb/gonbui/protocol"
	"github.com/pkg/errors"
	"io"
	"log"
	"runtime"
	"time"

	"github.com/go-zeromq/zmq4"
	"github.com/gofrs/uuid"
)

// zmqMsgHeader encodes header info for ZMQ messages.
type zmqMsgHeader struct {
	MsgID           string `json:"msg_id"`
	Username        string `json:"username"`
	Session         string `json:"session"`
	MsgType         string `json:"msg_type"`
	ProtocolVersion string `json:"version"`
	Timestamp       string `json:"date"`
}

// ComposedMsg represents an entire message in a high-level structure.
type ComposedMsg struct {
	Header       zmqMsgHeader
	ParentHeader zmqMsgHeader
	Metadata     map[string]interface{}
	Content      interface{}
}

// MIMEMap holds data that can be presented in multiple formats. The keys are MIME types
// and the values are the data formatted with respect to its MIME type.
// All maps should contain at least a "text/plain" representation with a string value.
type MIMEMap = map[string]interface{}

// Data is the exact structure returned to Jupyter.
// It allows to fully specify how a value should be displayed.
type Data = struct {
	Data      MIMEMap
	Metadata  MIMEMap
	Transient MIMEMap
}

// KernelInfo holds information about the kernel being served, for kernel_info_reply messages.
type KernelInfo struct {
	ProtocolVersion       string             `json:"protocol_version"`
	Implementation        string             `json:"implementation"`
	ImplementationVersion string             `json:"implementation_version"`
	LanguageInfo          KernelLanguageInfo `json:"language_info"`
	Banner                string             `json:"banner"`
	HelpLinks             []HelpLink         `json:"help_links"`
}

// KernelLanguageInfo holds information about the language that this kernel executes code in.
type KernelLanguageInfo struct {
	Name              string `json:"name"`
	Version           string `json:"version"`
	MIMEType          string `json:"mimetype"`
	FileExtension     string `json:"file_extension"`
	PygmentsLexer     string `json:"pygments_lexer"`
	CodeMirrorMode    string `json:"codemirror_mode"`
	NBConvertExporter string `json:"nbconvert_exporter"`
}

// HelpLink stores data to be displayed in the help menu of the notebook.
type HelpLink struct {
	Text string `json:"text"`
	URL  string `json:"url"`
}

// CompleteReply message sent in reply to a "complete_request": used for auto-complete of the
// code under a certain position of the cursor.
type CompleteReply struct {
	Status      string   `json:"status"`
	Matches     []string `json:"matches"`
	CursorStart int      `json:"cursor_start"`
	CursorEnd   int      `json:"cursor_end"`
	Metadata    MIMEMap  `json:"metadata"`
}

// InspectReply message sent in reply to a "inspect_request": used for introspection on the
// code under a certain position of the cursor.
type InspectReply struct {
	Status   string  `json:"status"`
	Found    bool    `json:"found"`
	Data     MIMEMap `json:"data"`
	Metadata MIMEMap `json:"metadata"`
}

// InvalidSignatureError is returned when the signature on a received message does not
// validate.
type InvalidSignatureError struct{}

func (e *InvalidSignatureError) Error() string {
	return "message had an invalid signature"
}

type MessageIf interface {
	// Error returns the error receiving the message, or nil if no error.
	Error() error

	// Ok returns whether there were no errors receiving the message.
	Ok() bool

	// Publish creates a new ComposedMsg and sends it back to the return identities over the
	// IOPub channel.
	Publish(msgType string, content interface{}) error

	// PromptInput sends a request for input from the front-end. The text in prompt is shown
	// to the user, and password indicates whether the input is a password (input shouldn't
	// be echoed in terminal).
	//
	// onInputFn is the callback function. It receives the original shell execute
	// message (m) and the message with the incoming input value.
	PromptInput(prompt string, password bool, onInput OnInputFn) error

	// CancelInput will cancel any `input_request` message sent by PromptInput.
	CancelInput() error

	// DeliverInput should be called if a message is received in Stdin channel. It will
	// check if there is any running process listening to it, in which case it is forwarded
	// (usually to the caller of PromptInput).
	// Still the dispatcher has to handle its delivery by calling this function.
	DeliverInput() error

	// Reply creates a new ComposedMsg and sends it back to the return identities over the
	// Shell channel.
	Reply(msgType string, content interface{}) error
}

// Message represents a received message or an Error, with its return identities, and
// a reference to the kernel for communication.
type Message struct {
	err        error
	Composed   ComposedMsg
	Identities [][]byte
	Kernel     *Kernel

	// Special Message requests.
	Silent, StoreHistory bool // From "execute_request" message.
}

// Error returns the error receiving the message, or nil if no error.
func (m *Message) Error() error { return m.err }

// Ok returns whether there were no errors receiving the message.
func (m *Message) Ok() bool { return m.err == nil }

// sendMessage sends a message to jupyter (response or request). Used original received
// message for identification.
func (m *Message) sendMessage(socket zmq4.Socket, msg *ComposedMsg) error {

	msgParts, err := m.Kernel.ToWireMsg(msg)
	if err != nil {
		return err
	}

	var frames = make([][]byte, 0, len(m.Identities)+1+len(msgParts))
	frames = append(frames, m.Identities...)
	frames = append(frames, []byte("<IDS|MSG>"))
	frames = append(frames, msgParts...)

	err = socket.SendMulti(zmq4.NewMsgFrom(frames...))
	if err != nil {
		return err
	}

	return nil
}

// NewComposed creates a new ComposedMsg to respond to a parent message.
// This includes setting up its headers.
func NewComposed(msgType string, parent ComposedMsg) (*ComposedMsg, error) {
	msg := &ComposedMsg{}

	msg.ParentHeader = parent.Header
	msg.Header.Session = parent.Header.Session
	msg.Header.Username = parent.Header.Username
	msg.Header.MsgType = msgType
	msg.Header.ProtocolVersion = ProtocolVersion
	msg.Header.Timestamp = time.Now().UTC().Format(time.RFC3339)

	u, err := uuid.NewV4()
	if err != nil {
		return msg, err
	}
	msg.Header.MsgID = u.String()

	return msg, nil
}

// Publish creates a new ComposedMsg and sends it back to the return identities over the
// IOPub channel.
func (m *Message) Publish(msgType string, content interface{}) error {
	msg, err := NewComposed(msgType, m.Composed)
	if err != nil {
		return err
	}
	msg.Content = content
	return m.Kernel.sockets.IOPubSocket.RunLocked(func(socket zmq4.Socket) error {
		return m.sendMessage(socket, msg)
	})
}

// OnInputFn is the callback function. It receives the original shell execute
// message and the message with the incoming input value.
type OnInputFn func(original, input *Message) error

// PromptInput sends a request for input from the front-end. The text in prompt is shown
// to the user, and password indicates whether the input is a password (input shouldn't
// be echoed in terminal).
//
// onInputFn is the callback function. It receives the original shell execute
// message (m) and the message with the incoming input value.
func (m *Message) PromptInput(prompt string, password bool, onInput OnInputFn) error {
	log.Printf("Message.PromptInput(%q, %v)", prompt, password)
	inputRequest, err := NewComposed("input_request", m.Composed)
	if err != nil {
		return errors.WithMessagef(err, "Message.PromptInput(): creating an input_request message")
	}
	inputRequest.Content = map[string]any{
		"prompt":   prompt,
		"password": password,
	}
	log.Printf("Stdin(%v) input request", inputRequest.Content)
	err = m.Kernel.sockets.StdinSocket.RunLocked(
		func(socket zmq4.Socket) error {
			return m.sendMessage(socket, inputRequest)
		})
	if err != nil {
		return errors.WithMessagef(err, "Message.PromptInput(): sending input_request message")
	}

	// Register callback.
	m.Kernel.stdinMsg = m
	m.Kernel.stdinFn = onInput

	return nil
}

// CancelInput will cancel any `input_request` message sent by PromptInput.
func (m *Message) CancelInput() error {
	log.Printf("Message.CancelInput()")
	// TODO: Check for any answers in the cross-posted question:
	// https://discourse.jupyter.org/t/cancelling-input-request-at-end-of-execution/17637
	// https://stackoverflow.com/questions/75206276/kernel-cancelling-a-input-request-at-the-end-of-the-execution-of-a-cell
	return nil
}

// DeliverInput should be called if a message is received in Stdin channel. It will
// check if there is any running process listening to it, in which case it is delivered.
// Still the user has to handle its delivery.
func (m *Message) DeliverInput() error {
	log.Printf("Message.DeliverInput()")
	if m.Kernel.stdinMsg == nil {
		return nil
	}
	return m.Kernel.stdinFn(m.Kernel.stdinMsg, m)
}

// Reply creates a new ComposedMsg and sends it back to the return identities over the
// Shell channel.
func (m *Message) Reply(msgType string, content interface{}) error {
	msg, err := NewComposed(msgType, m.Composed)
	if err != nil {
		return err
	}

	msg.Content = content
	log.Printf("Reply(%s):", msgType)
	return m.Kernel.sockets.ShellSocket.RunLocked(func(shell zmq4.Socket) error {
		return m.sendMessage(shell, msg)
	})
}

func EnsureMIMEMap(bundle MIMEMap) MIMEMap {
	if bundle == nil {
		bundle = make(MIMEMap)
	}
	return bundle
}

//func merge(a MIMEMap, b MIMEMap) MIMEMap {
//	if len(b) == 0 {
//		return a
//	}
//	if a == nil {
//		a = make(MIMEMap)
//	}
//	for k, v := range b {
//		a[k] = v
//	}
//	return a
//}

// PublishExecutionResult publishes the result of the `execCount` execution as a string.
func (m *Message) PublishExecutionResult(execCount int, data Data) error {
	return m.Publish("execute_result", struct {
		ExecCount int     `json:"execution_count"`
		Data      MIMEMap `json:"data"`
		Metadata  MIMEMap `json:"metadata"`
	}{
		ExecCount: execCount,
		Data:      data.Data,
		Metadata:  EnsureMIMEMap(data.Metadata),
	})
}

// PublishExecutionError publishes a serialized error that was encountered during execution.
func (m *Message) PublishExecutionError(err string, trace []string) error {
	return m.Publish("error",
		struct {
			Name  string   `json:"ename"`
			Value string   `json:"evalue"`
			Trace []string `json:"traceback"`
		}{
			Name:  "ERROR",
			Value: err,
			Trace: trace,
		},
	)
}

// PublishDisplayData publishes a single image.
func (m *Message) PublishDisplayData(data Data) error {
	// copy Data in a struct with appropriate json tags
	return m.Publish("display_data", struct {
		Data      MIMEMap `json:"data"`
		Metadata  MIMEMap `json:"metadata"`
		Transient MIMEMap `json:"transient"`
	}{
		Data:      data.Data,
		Metadata:  EnsureMIMEMap(data.Metadata),
		Transient: EnsureMIMEMap(data.Transient),
	})
}

// PublishDisplayDataWithHTML is a shortcut to PublishDisplayData for HTML content.
func (m *Message) PublishDisplayDataWithHTML(html string) error {
	msgData := Data{
		Data:      make(MIMEMap, 1),
		Metadata:  make(MIMEMap),
		Transient: make(MIMEMap),
	}
	msgData.Data[string(protocol.MIMETextHTML)] = html
	logDisplayData(msgData.Data)
	return m.PublishDisplayData(msgData)
}

const (
	// StreamStdout defines the stream name for standard out on the front-end. It
	// is used in `PublishWriteStream` to specify the stream to write to.
	StreamStdout = "stdout"

	// StreamStderr defines the stream name for standard error on the front-end. It
	// is used in `PublishWriteStream` to specify the stream to write to.
	StreamStderr = "stderr"
)

// PublishWriteStream prints the data string to a stream on the front-end. This is
// either `StreamStdout` or `StreamStderr`.
func (m *Message) PublishWriteStream(stream string, data string) error {
	return m.Publish("stream",
		struct {
			Stream string `json:"name"`
			Data   string `json:"text"`
		}{
			Stream: stream,
			Data:   data,
		},
	)
}

// jupyterStreamWriter is an `io.Writer` implementation that writes the data to the notebook
// front-end.
type jupyterStreamWriter struct {
	stream string
	msg    *Message
}

// NewJupyterStreamWriter returns an io.Writer that forwards what is written to the Jupyter client,
// under the given stream name.
func (m *Message) NewJupyterStreamWriter(stream string) io.Writer {
	return &jupyterStreamWriter{stream, m}
}

// Write implements `io.Writer.Write` by publishing the data via `PublishWriteStream`
func (w *jupyterStreamWriter) Write(p []byte) (int, error) {
	data := string(p)
	n := len(p)
	if err := w.msg.PublishWriteStream(w.stream, data); err != nil {
		return 0, errors.WithMessagef(err, "failed to stream %d bytes of data to stream %q", n, w.stream)
	}
	return n, nil
}

// PublishKernelStatus publishes a status message notifying front-ends of the state the kernel
// is in. It supports the states "starting", "busy", and "idle".
func PublishKernelStatus(msg MessageIf, status string) error {
	return msg.Publish("status",
		struct {
			ExecutionState string `json:"execution_state"`
		}{
			ExecutionState: status,
		},
	)
}

// SendKernelInfo sends a kernel_info_reply message.
func SendKernelInfo(msg MessageIf, version string) error {
	return msg.Reply("kernel_info_reply",
		KernelInfo{
			ProtocolVersion:       ProtocolVersion,
			Implementation:        "gonb",
			ImplementationVersion: version,
			Banner:                fmt.Sprintf("Go kernel: gonb - v%s", version),
			LanguageInfo: KernelLanguageInfo{
				Name:          "go",
				Version:       runtime.Version(),
				FileExtension: ".go",
			},
			HelpLinks: []HelpLink{
				{Text: "Go", URL: "https://golang.org/"},
				{Text: "gonb", URL: "https://github.com/janpfeifer/gonb"},
			},
		},
	)
}

// PublishExecutionInput publishes a status message notifying front-ends of what code is
// currently being executed.
func PublishExecutionInput(msg MessageIf, execCount int, code string) error {
	return msg.Publish("execute_input",
		struct {
			ExecCount int    `json:"execution_count"`
			Code      string `json:"code"`
		}{
			ExecCount: execCount,
			Code:      code,
		},
	)
}
