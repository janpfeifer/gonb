package gonbui

import (
	"bytes"
	"encoding/gob"
	"fmt"
	"github.com/janpfeifer/gonb/gonbui/protocol"
	"github.com/pkg/errors"
	"image"
	"image/png"
	"os"
	"sync"
)

func init() {
	IsNotebook = os.Getenv(protocol.GONB_PIPE_ENV) != ""
}

var (
	// IsNotebook indicates whether the execution was started by a GoNB kernel.
	IsNotebook bool

	// mu control access to displaying content.
	mu sync.Mutex

	// gonbPipe is the currently opened gonbPipe, if one is opened.
	gonbPipe      *os.File
	gonbPipeError error
	gonbEncoder   *gob.Encoder
)

// Error returns the first error that may have happened in communication to the kernel. Nil if there has been
// no errors.
func Error() error {
	mu.Lock()
	defer mu.Unlock()
	return gonbPipeError
}

// Open a singleton connection to the GoNB kernel. Returns any potential error. Errors can also later be accessed
// by the Error() function. The connection will be used by all Display* functions.
//
// Notice the display functions will call Open automatically, so it's not necessarily needed. Also, if the pipe is
// already opened, this becomes a no-op.
func Open() error {
	mu.Lock()
	defer mu.Unlock()
	return openLocked()
}

func openLocked() error {
	if gonbPipeError != nil {
		return gonbPipeError // Errors are persistent, it won't recover.
	}
	if gonbPipe != nil {
		// Pipe already opened.
		return nil
	}
	gonbPipe, gonbPipeError = os.OpenFile(os.Getenv(protocol.GONB_PIPE_ENV), os.O_WRONLY, 0600)
	if gonbPipeError != nil {
		gonbPipeError = errors.Wrapf(gonbPipeError, "failed opening pipe %q", os.Getenv(protocol.GONB_PIPE_ENV))
		return gonbPipeError
	}
	gonbEncoder = gob.NewEncoder(gonbPipe)
	return nil
}

func sendData(data *protocol.DisplayData) {
	mu.Lock()
	defer mu.Unlock()
	if err := openLocked(); err != nil {
		return
	}
	err := gonbEncoder.Encode(data)
	if err != nil {
		gonbPipeError = errors.Wrapf(err, "failed to write to pipe %q", os.Getenv(protocol.GONB_PIPE_ENV))
		gonbPipe.Close()
	}
}

// DisplayHTML will display the given HTML in the notebook, as the output of the cell being executed.
func DisplayHTML(html string) {
	if !IsNotebook {
		return
	}
	sendData(&protocol.DisplayData{
		Data: map[protocol.MIMEType]any{protocol.MIMETextHTML: html},
	})
}

// DisplayPNG displays the given PNG, given as raw bytes.
func DisplayPNG(png []byte) {
	if !IsNotebook {
		return
	}
	sendData(&protocol.DisplayData{
		Data: map[protocol.MIMEType]any{protocol.MIMEImagePNG: png},
	})
}

// DisplayImage displays the given image, by converting it to PNG first.
// It returns an error if it fails to encode to the image to PNG.
func DisplayImage(image image.Image) error {
	buf := bytes.NewBuffer(nil)
	err := png.Encode(buf, image)
	if err != nil {
		return err
	}
	DisplayPNG(buf.Bytes())
	return nil
}

func DisplaySVG(svg string) {
	if !IsNotebook {
		return
	}
	// This should be the implementation, but the Jupyter doesn't handle well SVG data
	// when the notebook is converted to HTML. So we try a simple work around of embedding
	// the SVG as HTML.
	// (Question in Jupyter forum:
	// https://discourse.jupyter.org/t/svg-either-not-loading-right-or-not-exporting-to-html/17824)
	//sendData(&protocol.DisplayData{
	//	Data: map[protocol.MIMEType]any{protocol.MIMEImageSVG: svg},
	//})
	DisplayHTML(fmt.Sprintf("<div>%s</div>", svg))
}
