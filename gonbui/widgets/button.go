package widgets

import (
	"bytes"
	_ "embed"
	"fmt"
	"github.com/janpfeifer/gonb/gonbui"
	"github.com/janpfeifer/gonb/gonbui/comms"
	"github.com/janpfeifer/gonb/gonbui/dom"
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

// Button returns a builder object that configures and builds a new button with
// the given `label`.
//
// One can use `Listen` to get updates (button clicks) -- the value returned
// is an int that is incremented at every click.
//
// Call `Done` method when you finish configuring the ButtonBuilder.
func Button(label string) *ButtonBuilder {
	return &ButtonBuilder{
		label:   label,
		address: "/button/" + gonbui.UniqueId(),
		htmlId:  "gonb_button_" + gonbui.UniqueId(),
	}
}

// WithHtmlId sets the id to use when creating the HTML element in the DOM.
// If not set, a unique one will be generated, and can be read with HtmlId.
//
// This can only be set before call to Done. If called afterward, it panics.
func (b *ButtonBuilder) WithHtmlId(htmlId string) *ButtonBuilder {
	if b.built {
		panicf("ButtonBuilder cannot change parameters after it is built")
	}
	b.htmlId = htmlId
	return b
}

// WithAddress configures the widget to use the given address to communicate its state
// with the front-end.
//
// The default is to use a randomly created unique address.
//
// It panics if called after the widget is built.
func (b *ButtonBuilder) WithAddress(address string) *ButtonBuilder {
	if b.built {
		panicf("ButtonBuilder cannot change parameters after it is built")
	}
	b.address = address
	return b
}

// AppendTo defines an id of the parent element in the DOM (in the front-end)
// where to insert the button.
//
// If not defined, it will simply display it as default in the output of the cell.
func (b *ButtonBuilder) AppendTo(parentHtmlId string) *ButtonBuilder {
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

	// Consume the first incoming button message, with counter == 0.
	clicks := comms.Listen[int](b.address)

	html := fmt.Sprintf(`<button id="%s" type="button">%s</button>`, b.htmlId, b.label)
	if b.parentHtmlId == "" {
		gonbui.DisplayHtml(html)
	} else {
		dom.Append(b.parentHtmlId, html)
	}

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
	dom.TransientJavascript(buf.String())

	<-clicks.C // Consume the first incoming button message, with counter == 0.
	clicks.Close()
	return b
}

// Listen returns an `AddressChannel[int]` (a wrapper for a `chan int`) that receives a counter each time the
// button is clicked.
// The counter is incremented at every click.
//
// Close the returned channel (`Close()` method) to unsubscribe from these messages and release the resources.
//
// It can only be called after the Button is created with Done, otherwise it panics.
//
// If for any reason you need to listen to clicks before the button is created, create a channel
// with the function `Listen[int](address)` directly, but you will need to ignore the first
// counter value sent when the button is created (with value 0).
func (b *ButtonBuilder) Listen() *comms.AddressChan[int] {
	if !b.built {
		panicf("ButtonBuilder.Listen can only be called after the button was created with `Done()` method")
	}
	return comms.Listen[int](b.address)
}

// HtmlId returns the `id` used in the widget HTML element created.
func (b *ButtonBuilder) HtmlId() string {
	return b.htmlId
}

// Address returns the address used to communicate to the widgets HTML element.
func (b *ButtonBuilder) Address() string {
	return b.address
}
