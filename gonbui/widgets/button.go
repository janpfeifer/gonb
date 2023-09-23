package widgets

import (
	"bytes"
	_ "embed"
	"fmt"
	"github.com/janpfeifer/gonb/gonbui"
	"text/template"
)

//go:embed button.js
var buttonJs []byte

var tmplButtonJs = template.Must(template.New("buttonJs").Parse(
	string(buttonJs)))

// ButtonBuilder is used to create a button on the front-end.
type ButtonBuilder struct {
	address, label, htmlId, parentHtmlId string
	onClick                              func()
	built                                bool
}

// Button returns a builder object to builds a new button with the given `label`.
//
// Call `Done` method when you finish to configure the ButtonBuilder.
func Button(label string) *ButtonBuilder {
	return &ButtonBuilder{
		label: label,
	}
}

// Address configures the button to use the given address to communicate its state
// with the front-end.
// The state is an int value that is incremented every time the button is pressed.
//
// The default is to use randomly created unique address.
//
// It panics if called after the button is built.
func (b *ButtonBuilder) Address(address string) *ButtonBuilder {
	if b.built {
		panicf("ButtonBuilder cannot change parameters after it is built")
	}
	b.address = address
	return b
}

// AppendToId defines an id of the parent element in the DOM (in the front-end)
// where to insert the button.
//
// If not defined it will simply display it as default in the output of the cell.
func (b *ButtonBuilder) AppendToId(parentHtmlId string) *ButtonBuilder {
	if b.built {
		panicf("ButtonBuilder cannot change parameters after it is built")
	}
	b.parentHtmlId = parentHtmlId
	return b
}

func (b *ButtonBuilder) Done() *ButtonBuilder {
	if b.built {
		panicf("ButtonBuilder.Done already called!?")
	}
	b.built = true
	if b.address == "" {
		b.address = "/button/" + gonbui.UniqueId()
	}
	b.htmlId = gonbui.UniqueId() + "_button"

	// Consume the first incoming button message, with counter == 0.
	clicks := Listen[int](b.address)

	gonbui.DisplayHTML(
		fmt.Sprintf(`<button id="%s" type="button">%s</button>`, b.htmlId, b.label))

	var buf bytes.Buffer
	data := struct {
		Address, HtmlId string
	}{
		Address: b.address,
		HtmlId:  b.htmlId,
	}
	err := tmplButtonJs.Execute(&buf, data)
	if err != nil {
		panicf("Button template is invalid!? Please report the error to GoNB: %v", err)
	}
	gonbui.ScriptJavascript(buf.String())

	<-clicks // Consume the first incoming button message, with counter == 0.
	close(clicks)
	return b
}

// Listen returns a channel that receives a counter (`int`) each time the button is clicked.
// The counter is incremented at every click.
//
// Close the returned channel to unsubscribe from these messages and release the resources.
//
// It can only be called after the Button is created with Done, otherwise it panics.
//
// If for any reason you need to listen to clicks before the button is created, create a channel
// with the function `Listen[int](address)` directly, but you will need to ignore the first
// counter value sent when the button is created (with value 0).
func (b *ButtonBuilder) Listen() chan int {
	if !b.built {
		panicf("ButtonBuilder.Listen can only be called after the button was created with `Done()` method")
	}
	return Listen[int](b.address)
}
