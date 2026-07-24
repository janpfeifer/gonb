package jpyexec

import (
	"encoding/gob"
	"fmt"
	"github.com/janpfeifer/gonb/gonbui/protocol"
	"github.com/janpfeifer/gonb/internal/kernel"
	"github.com/pkg/errors"
	"io"
	"k8s.io/klog/v2"
	"os"
)

func init() {
	gob.Register(map[string]any{})
	gob.Register([]string{})
	gob.Register([]any{})
}

type CommsHandler interface {
	ProgramStart(exec *Executor)
	ProgramFinished()
	ProgramSendValueRequest(address string, value any)
	ProgramReadValueRequest(address string)
	ProgramSubscribeRequest(address string)
	ProgramUnsubscribeRequest(address string)
}

const PipeWriterFifoBufferSize = 128

func (exec *Executor) pollNamedPipeReader() {
	decoder := gob.NewDecoder(exec.pipeReader)
	for {
		data := &protocol.DisplayData{}
		err := decoder.Decode(data)
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrClosedPipe) || errors.Is(err, os.ErrClosed) {
			return
		} else if err != nil {
			klog.Infof("Named pipe: failed to parse message: %+v", err)
			return
		}

		if reqAny, found := data.Data[protocol.MIMEJupyterInput]; found {
			klog.V(2).Infof("Received InputRequest: %v", reqAny)
			req, ok := reqAny.(protocol.InputRequest)
			if !ok {
				exec.reportCellError(errors.Errorf(
					"A MIMEJupyterInput sent to GONB_PIPE without an associated protocol.InputRequest!? -- got (%T) %#v",
					reqAny, reqAny))
				continue
			}
			exec.dispatchInputRequest(&req)
			continue
		}

		if reqAny, found := data.Data[protocol.MIMECommValue]; found {
			req, ok := reqAny.(protocol.CommValue)
			if !ok {
				exec.reportCellError(errors.Errorf(
					"Invalid message sent in named pipes to GoNB from cell, "+
						"this may affect widgets communication -- "+
						"MIMECommValue sent to $GONB_PIPE_BACK without an associated `protocol.CommValue` "+
						"type, got %T instead", reqAny))
				continue
			}

			if req.Address == protocol.GonbuiSyncAddress {
				syncId, ok := req.Value.(int)
				if !ok {
					klog.Errorf("comms: Receive Sync request with invalid value %+v. Communication with cell program may be left in an unusable state!", req)
					continue
				}
				klog.V(2).Infof("comms: Received Sync(%d) at %q, sending back ack", syncId, req.Address)
				exec.PipeWriterFifo <- &protocol.CommValue{
					Address: protocol.GonbuiSyncAckAddress,
					Value:   syncId,
				}
				continue
			}

			if exec.commsHandler == nil {
				klog.V(2).Infof("Received and dropped (no handler registered) CommValue: %+v", req)
			} else if req.Request {
				klog.V(2).Infof("ProgramReadValueRequest(%q) requested", req.Address)
				exec.commsHandler.ProgramReadValueRequest(req.Address)
			} else {
				klog.V(2).Infof("ProgramSendValueRequest(%q, %v) requested", req.Address, req.Value)
				exec.commsHandler.ProgramSendValueRequest(req.Address, req.Value)
			}
			continue
		}

		if reqAny, found := data.Data[protocol.MIMECommSubscribe]; found {
			req, ok := reqAny.(protocol.CommSubscription)
			if !ok {
				exec.reportCellError(errors.Errorf(
					"Invalid message sent in named pipes to GoNB from cell, "+
						"this may affect widgets communication -- "+
						"MIMECommSubscribe sent to $GONB_PIPE_BACK without an associated `protocol.CommSubscription` "+
						"type, got %T instead", reqAny))
				continue
			}
			if exec.commsHandler == nil {
				klog.V(2).Infof("Received and dropped (no handler registered) ProgramSubscribeRequest: %+v", req)
			} else if req.Unsubscribe {
				klog.V(2).Infof("ProgramUnsubscribeRequest(%q) requested", req.Address)
				exec.commsHandler.ProgramUnsubscribeRequest(req.Address)
			} else {
				klog.V(2).Infof("ProgramSubscribeRequest(%q) requested", req.Address)
				exec.commsHandler.ProgramSubscribeRequest(req.Address)
			}
			continue
		}

		exec.dispatchDisplayData(data)
	}
}

func (exec *Executor) pollPipeWriterFifo() {
	encoder := gob.NewEncoder(exec.pipeWriter)
	klog.V(2).Infof("jpyexec: pollPipeWriterFifo() listening to requests.")
	for msg := range exec.PipeWriterFifo {
		if klog.V(2).Enabled() {
			klog.Infof("jpyexec: encoding %+v to named pipe to cell program", msg)
		}
		err := encoder.Encode(msg)
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrClosedPipe) || errors.Is(err, os.ErrClosed) {
			return
		} else if err != nil {
			klog.Infof("while writing to cell program, failed to encode message %+v. "+
				"Communication with cell program broken, widgets won't work properly. "+
				"You can try re-executing the cell. Error: %+v", msg, err)
			return
		}
	}
	klog.V(2).Infof("jpyexec: pollPipeWriterFifo() closed.")
}

func (exec *Executor) reportCellError(err error) {
	errStr := fmt.Sprintf("%+v", err)
	klog.Errorf("%s", errStr)
	err = kernel.PublishWriteStream(exec.Msg, kernel.StreamStderr, "GoNB Error:\n"+errStr)
	if err != nil {
		klog.Errorf("%+v", errors.WithStack(err))
	}
}

func (exec *Executor) dispatchDisplayData(data *protocol.DisplayData) {
	msgData := kernel.Data{
		Data:      make(kernel.MIMEMap, len(data.Data)),
		Metadata:  make(kernel.MIMEMap),
		Transient: make(kernel.MIMEMap),
	}
	for mimeType, content := range data.Data {
		msgData.Data[string(mimeType)] = content

		if exec.captureDisplayDataOutput != nil {
			str, ok := content.(string)
			if ok {
				_, err := exec.captureDisplayDataOutput.Write([]byte(str))
				if err != nil {
					klog.Errorf("failed to capture display data output: %v", err)
				}
			}
		}
	}

	if klog.V(1).Enabled() {
		kernel.LogDisplayData(msgData.Data)
	}
	for key, content := range data.Metadata {
		msgData.Metadata[key] = content
	}
	var err error
	if data.DisplayID != "" {
		msgData.Transient["display_id"] = data.DisplayID
		err = kernel.PublishUpdateDisplayData(exec.Msg, msgData)
	} else {
		err = kernel.PublishData(exec.Msg, msgData)
	}
	if err != nil {
		klog.Errorf("Failed to display data (ignoring): %v", err)
	}
}

func (exec *Executor) dispatchInputRequest(req *protocol.InputRequest) {
	klog.V(2).Infof("Received InputRequest %+v", req)
	writeStdinFn := func(original, input *kernel.MessageImpl) error {
		content := input.Composed.Content.(map[string]any)
		value := content["value"].(string) + "\n"
		klog.V(2).Infof("stdin value: %q", value)
		go func() {
			exec.muDone.Lock()
			cmdStdin := exec.cmdStdin
			exec.muDone.Unlock()
			if exec.isDone {
				return
			}
			_, err := cmdStdin.Write([]byte(value))
			if err != nil {
				klog.Warningf("failed to write to stdin of cell: %+v", err)
			}
		}()
		return nil
	}
	err := exec.Msg.PromptInput(req.Prompt, req.Password, writeStdinFn)
	if err != nil {
		exec.reportCellError(err)
	}
}
