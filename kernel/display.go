package kernel

// This file implements the protocol to display rich content: it provides PollGonbPipe that continuously
// read from a named pipe (mkfifo(3)) and display it.

import (
	"encoding/gob"
	"fmt"
	"github.com/janpfeifer/gonb/gonbui/protocol"
	"github.com/pkg/errors"
	"io"
	"k8s.io/klog/v2"
	"os"
)

// PollGonbPipe will continuously read for incoming requests for displaying content on the notebook.
// It expects pipeIn to be closed when the polling is to stop.
func PollGonbPipe(msg Message, pipeReader *os.File, cmdStdin io.Writer) {
	decoder := gob.NewDecoder(pipeReader)
	knownBlockIds := make(map[string]struct{})
	for {
		data := &protocol.DisplayData{}
		err := decoder.Decode(data)
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrClosedPipe) || errors.Is(err, os.ErrClosed) {
			return
		} else if err != nil {
			klog.V(2).Infof("Named pipe closed / EOF: %v", err)
			return
		}
		if reqAny, found := data.Data[protocol.MIMEJupyterInput]; found {
			// This is actually a request for input, process it separately.
			klog.V(2).Infof("Received InputRequest: %v", reqAny)
			req, ok := reqAny.(*protocol.InputRequest)
			if !ok {
				reportCellError(msg, errors.New("A MIMEJupyterInput sent to GONB_PIPE without an associated protocol.InputRequest!?"))
				continue
			}
			processInputRequest(msg, cmdStdin, req)
			continue
		}
		processDisplayData(msg, data, knownBlockIds)
	}
}

// reportCellError reports error to both, the notebook and the standard logger (gonb's stderr).
func reportCellError(msg Message, err error) {
	errStr := fmt.Sprintf("%+v", err) // Error with stack.
	klog.Errorf("%s", errStr)
	err = PublishWriteStream(msg, StreamStderr, errStr)
	if err != nil {
		klog.Errorf("%+v", errors.WithStack(err))
	}
}

func logDisplayData(data MIMEMap) {
	for key, valueAny := range data {
		switch value := valueAny.(type) {
		case string:
			displayValue := value
			if len(displayValue) > 20 {
				displayValue = displayValue[:20] + "..."
			}
			klog.Infof("DisplayData(%s): %q", key, displayValue)
		case []byte:
			klog.Infof("DisplayData(%s): %d bytes", key, len(value))
		default:
			klog.Infof("DisplayData(%s): unknown type %t", key, value)
		}
	}
}

// processDisplayData process an incoming `protocol.DisplayData` object.
func processDisplayData(msg Message, data *protocol.DisplayData, knownBlockIds map[string]struct{}) {
	// Log info about what is being displayed.
	msgData := Data{
		Data:      make(MIMEMap, len(data.Data)),
		Metadata:  make(MIMEMap),
		Transient: make(MIMEMap),
	}
	for mimeType, content := range data.Data {
		msgData.Data[string(mimeType)] = content
	}
	if klog.V(1).Enabled() {
		logDisplayData(msgData.Data)
	}
	for key, content := range data.Metadata {
		msgData.Metadata[key] = content
	}
	isUpdate := false
	if data.DisplayID != "" {
		msgData.Transient["display_id"] = data.DisplayID
		if _, found := knownBlockIds[data.DisplayID]; found {
			isUpdate = true
		}
		knownBlockIds[data.DisplayID] = struct{}{}
	}
	var err error
	if isUpdate {
		err = PublishUpdateDisplayData(msg, msgData)
	} else {
		err = PublishDisplayData(msg, msgData)
	}
	if err != nil {
		klog.Errorf("Failed to display data (ignoring): %v", err)
	}
}

func processInputRequest(msg Message, cmdStdin io.Writer, req *protocol.InputRequest) {
	klog.V(2).Infof("Received InputRequest %+v", req)
	writeStdinFn := func(original, input *MessageImpl) error {
		content := input.Composed.Content.(map[string]any)
		value := content["value"].(string) + "\n"
		klog.V(2).Infof("stdin value: %q", value)
		go func() {
			// Write concurrently, not to block, in case program doesn't
			// actually read anything from the stdin.
			_, err := cmdStdin.Write([]byte(value))
			if err != nil {
				// Could happen if something was not fully written, and channel was closed, in
				// which case it's ok.
				klog.Warningf("failed to write to stdin of cell: %+v", err)
			}
		}()
		return nil
	}
	err := msg.PromptInput(req.Prompt, req.Password, writeStdinFn)
	if err != nil {
		reportCellError(msg, err)
	}
}
