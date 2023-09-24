// Package gonbui provides tools to interact with the front-end (the notebook)
// using HTML and other rich-data.
//
// In its simplest form, simply use `DisplayHtml` to display HTML content.
// But there is much more nuance and powerful types of interactions (including
// support for widgets, see the `widget` sup-package). Check out the
// [tutorial]() for details.
//
// If using the rich data content, consider adding the following to your `main()`
// function:
//
//	defer gonbui.Sync()
//
// This guarantees that no in-transit display content get left behind when a program
// exits.
package gonbui

import (
	"bytes"
	"encoding/base64"
	"encoding/gob"
	"fmt"
	"github.com/gofrs/uuid"
	"github.com/janpfeifer/gonb/common"
	"github.com/janpfeifer/gonb/gonbui/protocol"
	"github.com/pkg/errors"
	"image"
	"image/png"
	"io"
	"k8s.io/klog/v2"
	"log"
	"os"
	"sync"
)

func init() {
	IsNotebook = os.Getenv(protocol.GONB_PIPE_ENV) != ""
}

var Debug bool

// Logf is used to log debugging messages for the gonbui library.
// It is enabled by setting the global Debug to true.
//
// Usually only useful for those developing new widgets and the like.
func Logf(format string, args ...any) {
	if Debug {
		log.Printf(format, args...)
	}
}

var (
	// IsNotebook indicates whether the execution was started by a GoNB kernel.
	IsNotebook bool

	// mu control access to displaying content.
	mu sync.Mutex

	// gonbWriterPipe is the currently opened gonbWriterPipe, if one is opened.
	gonbWriterPipe, gonbReaderPipe *os.File
	gonbPipesError                 error

	// gonbEncoder encodes messages to GoNB, to gonbWriterPipe.
	//
	// The messages are always a protocol.DisplayData object.
	gonbEncoder *gob.Encoder

	// gonbDecoder decodes messages coming from GoNB, from gonbReaderPipe.
	//
	// The messages are always a protocol.CommValue.
	gonbDecoder *gob.Decoder
)

// Error returns the error that triggered failure on the communication with GoNB.
// It returns nil if there were no errors.
//
// It can be tested as a health check.
func Error() error {
	return gonbPipesError
}

// OnCommValueUpdate handler and dispatcher of value updates.
//
// Internal use only -- used by `gonb/gonbui/comms`.
var OnCommValueUpdate func(valueMsg *protocol.CommValue)

// Open pipes used to communicate to GoNB (and through it, to the front-end).
// This can be called every time, if connections are already opened, it does nothing.
//
// Users don't need to use this directly, since this is called every time by all other functions.
//
// Returns nil if succeeded (or if connections were already opened).
func Open() error {
	mu.Lock()
	defer mu.Unlock()
	return openLocked()
}

// openLock a singleton connection to the GoNB kernel, assuming `mu` is already locked. Returns any potential error.
// Errors can also later be accessed by the Error() function. The connection will be used by all Display* functions.
//
// Notice the display functions will call Open automatically, so it's not necessarily needed. Also, if the pipe is
// already opened, this becomes a no-op.
func openLocked() error {
	if !IsNotebook {
		return errors.Errorf("Trying to communicate with GoNB, but apparently program is not being executed by GoNB.")
	}
	Logf("openLocked() ...")
	if gonbPipesError != nil {
		return gonbPipesError // Errors are persistent, it won't recover.
	}
	if gonbWriterPipe == nil {
		gonbWriterPath := os.Getenv(protocol.GONB_PIPE_ENV)
		Logf("openLocked(): opening writer in %q...", gonbWriterPath)
		gonbWriterPipe, gonbPipesError = os.OpenFile(gonbWriterPath, os.O_WRONLY, 0600)
		Logf("openLocked(): opened writer in %q...", gonbWriterPath)
		if gonbPipesError != nil {
			gonbPipesError = errors.Wrapf(gonbPipesError, "failed opening pipe %q for writing", gonbWriterPath)
			closePipesLocked()
			return gonbPipesError
		}
		gonbEncoder = gob.NewEncoder(gonbWriterPipe)
	}
	if gonbReaderPipe == nil {
		gonbReaderPath := os.Getenv(protocol.GONB_PIPE_BACK_ENV)
		Logf("openLocked(): opening reader in %q...", gonbReaderPath)
		readerPipe, err := os.OpenFile(gonbReaderPath, os.O_RDONLY, 0600)
		Logf("openLocked(): opened reader in %q...", gonbReaderPath)
		if err == nil {
			gonbReaderPipe = readerPipe
			gonbDecoder = gob.NewDecoder(readerPipe)
		} else {
			if gonbPipesError == nil {
				gonbPipesError = errors.Wrapf(gonbPipesError, "failed opening pipe %q for reading", gonbReaderPath)
				closePipesLocked()
			}
		}
		if gonbPipesError != nil {
			Logf("openLocked(): failed with %+v", gonbPipesError)
			return gonbPipesError
		}
		// Start polling in this separate goroutine.
		go pollReaderPipe()
	}
	return nil
}

// closePipesLocked closes the remaining opened pipes dropping any errors while closing.
// It assumes mu is locked.
//
// This should be called in case of I/O errors any of the pipes.
func closePipesLocked() {
	if gonbWriterPipe != nil {
		_ = gonbWriterPipe.Close()
		gonbWriterPipe = nil
	}
	if gonbReaderPipe != nil {
		_ = gonbReaderPipe.Close()
		gonbReaderPipe = nil
	}
}

// SendData to be displayed in the connected Notebook.
//
// This is a lower level function, that most end users won't need to use, instead
// look for the other functions DisplayHtml, DisplayMarkdown, etc.
//
// But if you are testing new types of MIME types, this is the way to result
// messages ("execute_result" message type) directly to the front-end.
func SendData(data *protocol.DisplayData) {
	Logf("SendData() sending ...")
	mu.Lock()
	defer mu.Unlock()

	if err := openLocked(); err != nil {
		Logf("SendData(): failed, error: %+v", err)
		return
	}
	err := gonbEncoder.Encode(data)
	if err != nil {
		gonbPipesError = errors.Wrapf(err, "failed to write to GoNB pipe %q, pipes closed", os.Getenv(protocol.GONB_PIPE_ENV))
		closePipesLocked()
		klog.Errorf("%+v", gonbPipesError)
	}
}

// pollReaderPipe loops on reading messages (protocol.CommValue) from gonbReaderPipe and
// calling comms.DeliverValue, until the pipe is closed.
func pollReaderPipe() {
	Logf("pollReaderPipe() started")
	for gonbReaderPipe != nil {
		valueMsg := &protocol.CommValue{}
		err := gonbDecoder.Decode(valueMsg)
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrClosedPipe) || errors.Is(err, os.ErrClosed) {
			Logf("pollReaderPipe() closed")
			return
		} else if err != nil {
			Logf("pollReaderPipe() error decoding: %+v", err)
			mu.Lock()
			if gonbPipesError == nil {
				gonbPipesError = errors.Wrapf(err, "failed to read from GoNB pipe %q, pipes closed", os.Getenv(protocol.GONB_PIPE_BACK_ENV))
				closePipesLocked()
				klog.Errorf("%+v", gonbPipesError)
			}
			mu.Unlock()
			break
		}
		Logf("pollReaderPipe() received %+v", valueMsg)

		if valueMsg.Address == protocol.GonbuiSyncAckAddress {
			// Signal arrival of sync_ack.
			mu.Lock()
			syncId, ok := valueMsg.Value.(int)
			var l *common.Latch
			if ok {
				l, ok = syncRequestsMap[syncId]
			}
			if ok {
				l.Trigger()
				Logf("\ttriggered sync(%d) latch.", syncId)
				delete(syncRequestsMap, syncId)
			} else {
				log.Printf("Received invalid sync acknowledgment %+v !? Communication to front-end may have become unstable!", valueMsg)
			}
			mu.Unlock()

		} else if OnCommValueUpdate != nil {
			// Generic Comms update.
			Logf("dispatching OnCommValueUpdate(%q)", valueMsg.Address)
			OnCommValueUpdate(valueMsg)
		}

		Logf("pollReaderPipe() delivered to %q", valueMsg.Address)
	}
}

var (
	// Control Sync requests/acknowledges.
	nextSyncId      int
	syncRequestsMap = make(map[int]*common.Latch)
)

// Sync synchronizes with GoNB, and can be used to make sure all pending output has been sent.
//
// This can be used at the end of a program to make sure that everything that is in the pipe to be
// displayed is fully displayed (flushed) before a program exits.
func Sync() {
	if !IsNotebook || gonbPipesError != nil {
		return
	}

	mu.Lock()
	syncId := nextSyncId
	nextSyncId++
	Logf("Sync(id=%d) ...", syncId)
	l := common.NewLatch()
	syncRequestsMap[syncId] = l
	mu.Unlock()

	data := &protocol.DisplayData{
		Data: map[protocol.MIMEType]any{
			protocol.MIMECommValue: &protocol.CommValue{
				Address: "#gonbui/sync",
				Value:   syncId,
			}},
	}
	SendData(data)
	Logf("\tsync(%d) request sent, waiting...", syncId)

	// Wait, the latch will trigger when a "#gonbui/sync_ack" is received from GoNB.
	l.Wait()
	Logf("\twait for sync(%d) done.", syncId)
}

// UniqueId returns newly created unique id.
// It can be used for instance with UpdateHtml.
func UniqueId() string {
	uuid, _ := uuid.NewV7()
	uuidStr := uuid.String()
	uid := uuidStr[len(uuidStr)-8:]
	return uid
}

// UniqueID returns a newly created unique id.
// UniqueID is an alias for UniqueId.
//
// Deprecated: Use UniqueId instead.
func UniqueID() string {
	return UniqueId()
}

// DisplayHtml will display the given HTML in the notebook, as the output of the cell being executed.
func DisplayHtml(html string) {
	if !IsNotebook {
		return
	}
	SendData(&protocol.DisplayData{
		Data: map[protocol.MIMEType]any{protocol.MIMETextHTML: html},
	})
}

// DisplayHTML is an alias to DisplayHtml.
func DisplayHTML(html string) {
	DisplayHtml(html)
}

// DisplayHtmlf is similar to DisplayHtml, but it takes a format string and its args which
// are passed to fmt.Sprintf.
func DisplayHtmlf(htmlFormat string, args ...any) {
	if !IsNotebook {
		return
	}
	html := fmt.Sprintf(htmlFormat, args...)
	DisplayHtml(html)
}

// DisplayMarkdown will display the given markdown content in the notebook, as the output of
// the cell being executed.
// This also renders math formulas using latex, use `$x^2$` for formulas inlined in text, or
// double "$" for formulas in a separate line -- e.g.:
// `$$f(x) = \int_{-\infty}^{\infty} e^{-x^2} dx$$`.
func DisplayMarkdown(markdown string) {
	if !IsNotebook {
		return
	}
	SendData(&protocol.DisplayData{
		Data: map[protocol.MIMEType]any{protocol.MIMETextMarkdown: markdown},
	})
}

// UpdateHtml displays the given HTML in the notebook on an output block with the given `id`:
// the block identified by 'id' is created automatically the first time this function is
// called, and simply updated thereafter.
//
// The contents of these output blocks are considered transient, and intended to live
// only during a kernel session.
//
// Usage example:
//
// ```go
//
//	counterDisplayId := "counter_"+gonbui.UniqueId()
//	for ii := 0; ii < 10; ii++ {
//	  gonbui.UpdateHtml(counterDisplayId, fmt.Sprintf("Count: <b>%d</b>\n", ii))
//	}
//	gonbui.UpdateHtml(counterDisplayId, "")  // Erase transient block.
//	gonbui.DisplayHtml(fmt.Sprintf("Count: <b>%d</b>\n", ii))  // Show on final block.
//
// ```
func UpdateHtml(id, html string) {
	if !IsNotebook {
		return
	}
	SendData(&protocol.DisplayData{
		Data:      map[protocol.MIMEType]any{protocol.MIMETextHTML: html},
		DisplayID: id,
	})
}

// UpdateHTML is an alias for UpdateHtml.
// Deprecated: use UpdateHtml instead, it's the same.
func UpdateHTML(id, html string) {
	UpdateHtml(id, html)
}

// UpdateMarkdown updates the contents of the output identified by id:
// the block identified by 'id' is created automatically the first time this function is
// called, and simply updated thereafter.
//
// The contents of these output blocks are considered transient, and intended to live only
// during a kernel session.
//
// See example in UpdateHtml, just instead this used Markdown content.
func UpdateMarkdown(id, markdown string) {
	if !IsNotebook {
		return
	}
	SendData(&protocol.DisplayData{
		Data:      map[protocol.MIMEType]any{protocol.MIMETextMarkdown: markdown},
		DisplayID: id,
	})
}

// DisplayPng displays the given PNG, given as raw bytes.
func DisplayPng(png []byte) {
	if !IsNotebook {
		return
	}
	SendData(&protocol.DisplayData{
		Data: map[protocol.MIMEType]any{protocol.MIMEImagePNG: png},
	})
}

// DisplayPNG is an alias for DisplayPng.
// Deprecated: use DisplayPng instead.
func DisplayPNG(png []byte) {
	DisplayPng(png)
}

// DisplayImage displays the given image, by converting it to PNG first.
// It returns an error if it fails to encode to the image to PNG.
func DisplayImage(image image.Image) error {
	buf := bytes.NewBuffer(nil)
	err := png.Encode(buf, image)
	if err != nil {
		return err
	}
	DisplayPng(buf.Bytes())
	return nil
}

func DisplaySvg(svg string) {
	if !IsNotebook {
		return
	}
	// This should be the implementation, but Jupyter doesn't handle well SVG data
	// when the notebook is converted to HTML.
	// So we try a simple workaround of embedding the SVG as HTML.
	// (Question in Jupyter forum:
	// https://discourse.jupyter.org/t/svg-either-not-loading-right-or-not-exporting-to-html/17824)
	//SendData(&protocol.DisplayData{
	//	Data: map[protocol.MIMEType]any{protocol.MIMEImageSVG: svg},
	//})
	DisplayHtml(fmt.Sprintf("<div>%s</div>", svg))
}

// DisplaySVG is an alias for DisplaySvg.
// Deprecated: use DisplaySvg instead.
func DisplaySVG(svg string) {
	DisplaySvg(svg)
}

// RequestInput from the Jupyter notebook.
// It triggers the opening of a small text field in the cell output area where the user
// can type something.
// Whatever the user writes is written to the stdin of cell program -- and can be read,
// for instance, with `fmt.Scanf`.
//
// Args:
//   - prompt: string displayed in front of the field to be entered. Leave empty ("") if not needed.
//   - password: if whatever the user is typing is not to be displayed.
func RequestInput(prompt string, password bool) {
	if !IsNotebook {
		return
	}
	req := protocol.InputRequest{
		Prompt:   prompt,
		Password: password,
	}
	SendData(&protocol.DisplayData{
		Data: map[protocol.MIMEType]any{
			protocol.MIMEJupyterInput: &req,
		},
	})
}

// ScriptJavascript executes the given Javascript script in the Notebook.
//
// Errors in javascript parsing are sent by Jupyter Server to the stderr -- as opposed to showing
// to the browser console, which may be harder to debug.
//
// Also, like with DisplayHtml, each execution creates a new `<div>` block in the output area.
// Even if empty, it uses up a bit of vertical space (Jupyter Notebook thing).
//
// If these are an issue, consider using TransientJavascript, which uses a transient area
// to execute the Javascript, which is re-used for every execution.
func ScriptJavascript(js string) {
	if !IsNotebook {
		return
	}
	SendData(&protocol.DisplayData{
		Data: map[protocol.MIMEType]any{protocol.MIMETextJavascript: js},
	})
}

// EmbedImageAsPNGSrc returns a string that can be used as in an HTML <img> tag, as its source (it's `src` field).
// This simplifies embedding an image in HTML without requiring separate files. It embeds it as a PNG file
// base64 encoded.
func EmbedImageAsPNGSrc(img image.Image) (string, error) {
	buf := &bytes.Buffer{}
	err := png.Encode(buf, img)
	if err != nil {
		return "", errors.Wrapf(err, "failed to encode image as PNG")
	}
	encoded := base64.StdEncoding.EncodeToString(buf.Bytes())
	return fmt.Sprintf("data:image/png;base64,%s", encoded), nil
}
