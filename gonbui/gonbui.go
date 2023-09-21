package gonbui

import (
	"bytes"
	"encoding/base64"
	"encoding/gob"
	"fmt"
	"github.com/gofrs/uuid"
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
// It returns nil if there hasn't been any errors.
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
	log.Printf("openLocked() ...")
	if gonbPipesError != nil {
		return gonbPipesError // Errors are persistent, it won't recover.
	}
	if gonbWriterPipe == nil {
		gonbWriterPath := os.Getenv(protocol.GONB_PIPE_ENV)
		log.Printf("openLocked(): opening writer in %q...", gonbWriterPath)
		gonbWriterPipe, gonbPipesError = os.OpenFile(gonbWriterPath, os.O_WRONLY, 0600)
		log.Printf("openLocked(): opened writer in %q...", gonbWriterPath)
		if gonbPipesError != nil {
			gonbPipesError = errors.Wrapf(gonbPipesError, "failed opening pipe %q for writing", gonbWriterPath)
			closePipesLocked()
			return gonbPipesError
		}
		gonbEncoder = gob.NewEncoder(gonbWriterPipe)
	}
	if gonbReaderPipe == nil {
		gonbReaderPath := os.Getenv(protocol.GONB_PIPE_BACK_ENV)
		log.Printf("openLocked(): opening reader in %q...", gonbReaderPath)
		readerPipe, err := os.OpenFile(gonbReaderPath, os.O_RDONLY, 0600)
		log.Printf("openLocked(): opened reader in %q...", gonbReaderPath)
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
			log.Printf("openLocked(): failed with %+v", gonbPipesError)
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
// look for the other functions DisplayHTML, DisplayMarkdown, etc.
//
// But if you are testing new types of MIME types, this is the way to result
// messages ("execute_result" message type) directly to the front-end.
func SendData(data *protocol.DisplayData) {
	log.Printf("SendData() sending ...")
	mu.Lock()
	defer mu.Unlock()

	if err := openLocked(); err != nil {
		log.Printf("SendData(): failed, error: %+v", err)
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
	log.Printf("pollReaderPipe() started")
	for gonbReaderPipe != nil {
		valueMsg := &protocol.CommValue{}
		err := gonbDecoder.Decode(valueMsg)
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrClosedPipe) || errors.Is(err, os.ErrClosed) {
			log.Printf("pollReaderPipe() closed")
			return
		} else if err != nil {
			log.Printf("pollReaderPipe() error decoding: %+v", err)
			mu.Lock()
			if gonbPipesError == nil {
				gonbPipesError = errors.Wrapf(err, "failed to read from GoNB pipe %q, pipes closed", os.Getenv(protocol.GONB_PIPE_BACK_ENV))
				closePipesLocked()
				klog.Errorf("%+v", gonbPipesError)
			}
			mu.Unlock()
			break
		}
		log.Printf("pollReaderPipe() received %+v", valueMsg)
		if OnCommValueUpdate != nil {
			OnCommValueUpdate(valueMsg)
		}
	}
}

// DisplayHTML will display the given HTML in the notebook, as the output of the cell being executed.
func DisplayHTML(html string) {
	if !IsNotebook {
		return
	}
	SendData(&protocol.DisplayData{
		Data: map[protocol.MIMEType]any{protocol.MIMETextHTML: html},
	})
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

// UpdateHTML displays the given HTML in the notebook on an output block with the given `id`:
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
//	counterDisplayId := gonbui.UniqueID()
//	for ii := 0; ii < 10; ii++ {
//	  gonbui.UpdateHTML(counterDisplayId, fmt.Sprintf("Count: <b>%d</b>\n", ii))
//	}
//	gonbui.UpdateHTML(counterDisplayId, "")  // Erase transient block.
//	gonbui.DisplayHTML(fmt.Sprintf("Count: <b>%d</b>\n", ii))  // Show on final block.
//
// ```
func UpdateHTML(id, html string) {
	if !IsNotebook {
		return
	}
	SendData(&protocol.DisplayData{
		Data:      map[protocol.MIMEType]any{protocol.MIMETextHTML: html},
		DisplayID: id,
	})
}

// UniqueID returns a unique id that can be used for UpdateHTML.
// It should be generated once per display block the program wants to update.
func UniqueID() string {
	uuid, _ := uuid.NewV7()
	uuidStr := uuid.String()
	uid := uuidStr[len(uuidStr)-8:]
	return fmt.Sprintf("gonb_id_%s", uid)
}

// UpdateMarkdown updates the contents of the output identified by id:
// the block identified by 'id' is created automatically the first time this function is
// called, and simply updated thereafter.
//
// The contents of these output blocks are considered transient, and intended to live only
// during a kernel session.
//
// See example in UpdateHTML, just instead this used Markdown content.
func UpdateMarkdown(id, markdown string) {
	if !IsNotebook {
		return
	}
	SendData(&protocol.DisplayData{
		Data:      map[protocol.MIMEType]any{protocol.MIMETextMarkdown: markdown},
		DisplayID: id,
	})
}

// DisplayPNG displays the given PNG, given as raw bytes.
func DisplayPNG(png []byte) {
	if !IsNotebook {
		return
	}
	SendData(&protocol.DisplayData{
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
	// This should be the implementation, but Jupyter doesn't handle well SVG data
	// when the notebook is converted to HTML.
	// So we try a simple workaround of embedding the SVG as HTML.
	// (Question in Jupyter forum:
	// https://discourse.jupyter.org/t/svg-either-not-loading-right-or-not-exporting-to-html/17824)
	//SendData(&protocol.DisplayData{
	//	Data: map[protocol.MIMEType]any{protocol.MIMEImageSVG: svg},
	//})
	DisplayHTML(fmt.Sprintf("<div>%s</div>", svg))
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

// ScriptJavascript executes the given Javascript script in the Notebook.
//
// Errors in javascript parsing are sent by Jupyter Server to the stderr -- as opposed to showing
// to the browser console.
// These may be harder to debug during development than simply putting the contents inside a
// `<script>...javascript...</script>` and using `DisplayHTML` instead.
func ScriptJavascript(js string) {
	if !IsNotebook {
		return
	}
	SendData(&protocol.DisplayData{
		Data: map[protocol.MIMEType]any{protocol.MIMETextJavascript: js},
	})
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
