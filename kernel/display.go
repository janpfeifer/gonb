package kernel

// This file implements the protocol to display rich content: it provides PollDisplayRequests that continuously
// read from a named pipe (mkfifo(3)) and display it.

import (
	"encoding/gob"
	"github.com/janpfeifer/gonb/gonbui/protocol"
	"github.com/pkg/errors"
	"io"
	"log"
	"os"
)

// PollDisplayRequests will continuously read for incoming requests for displaying content on the notebook.
// It expects pipeIn to be closed when the polling is to stop.
func PollDisplayRequests(msg Message, pipeReader *os.File) {
	decoder := gob.NewDecoder(pipeReader)
	knownBlockIds := make(map[string]struct{})
	for {
		data := &protocol.DisplayData{}
		err := decoder.Decode(data)
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrClosedPipe) || errors.Is(err, os.ErrClosed) {
			return
		} else if err != nil {
			log.Printf("Failed to read from named pipe, stopped polling for new data content: %+v", err)
			return
		}
		processDisplayData(msg, data, knownBlockIds)
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
			log.Printf("DisplayData(%s): %q", key, displayValue)
		case []byte:
			log.Printf("DisplayData(%s): %d bytes", key, len(value))
		default:
			log.Printf("DisplayData(%s): unknown type %t", key, value)
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
	logDisplayData(msgData.Data)
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
		log.Printf("Failed to display data (ignoring): %v", err)
	}
}
