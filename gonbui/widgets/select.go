package widgets

import (
	"bytes"
	_ "embed"
	"fmt"
	"github.com/janpfeifer/gonb/common"
	"github.com/janpfeifer/gonb/gonbui"
	"github.com/janpfeifer/gonb/gonbui/comms"
	"github.com/janpfeifer/gonb/gonbui/dom"
	"strings"
	"text/template"
)

//go:embed select.js
var selectJs []byte

var tmplSelectJs = template.Must(template.New("selectJs").Parse(
	string(selectJs)))

// SelectBuilder is used to create a select element on the front-end.
type SelectBuilder struct {
	address, label, htmlId, parentHtmlId string
	onClick                              func()
	built                                bool

	// List to select from.
	options                    []string
	currentValue, defaultValue int

	// listenUpdates is the channel used to keep tabs of the updates.
	listenUpdates *comms.AddressChan[int]
	firstUpdate   *common.Latch // If first update received.
}

// Select returns a builder object that builds a new `<select>` element
// with the list of values given.
//
// Values (used for `Listen`, `Value` and `SetValue`) are integers representing
// the index of options selected.
//
// Call `Done` method when you finish configuring the SelectBuilder.
func Select(options []string) *SelectBuilder {
	return &SelectBuilder{
		address:     "/select/" + gonbui.UniqueId(),
		options:     options,
		htmlId:      "gonb_select_" + gonbui.UniqueId(),
		firstUpdate: common.NewLatch(),
	}
}

// WithHtmlId sets the id to use when creating the HTML element in the DOM.
// If not set, a unique one will be generated, and can be read with HtmlId.
//
// This can only be set before call to Done. If called afterward, it panics.
func (b *SelectBuilder) WithHtmlId(htmlId string) *SelectBuilder {
	if b.built {
		panicf("SelectBuilder cannot change parameters after it is built")
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
func (b *SelectBuilder) WithAddress(address string) *SelectBuilder {
	if b.built {
		panicf("SelectBuilder cannot change parameters after it is built")
	}
	b.address = address
	return b
}

// SetDefault option of the selection. If not set, it is 0.
// Can only be set before being built.
//
// It panics if called after the widget is built.
func (b *SelectBuilder) SetDefault(idx int) *SelectBuilder {
	if b.built {
		panicf("SelectBuilder cannot change parameters after it is built")
	}
	b.defaultValue = idx
	return b
}

// AppendTo defines an id of the parent element in the DOM (in the front-end)
// where to insert the widget.
//
// If not defined, it will simply display it as default in the output of the cell.
//
// It panics if called after the widget is built.
func (b *SelectBuilder) AppendTo(parentHtmlId string) *SelectBuilder {
	if b.built {
		panicf("SelectBuilder cannot change parameters after it is built")
	}
	b.parentHtmlId = parentHtmlId
	return b
}

// Done builds the HTML element in the frontend and starts listening to updates.
//
// After this is called options can no longer be set.
//
// The value associated with the widget can now be read or modified with `Value`, `GetValue` and
// `Listen` are available.
func (b *SelectBuilder) Done() *SelectBuilder {
	if b.built {
		panicf("SelectBuilder.Done already called!?")
	}
	b.built = true

	// Record incoming slider updates.
	b.listenUpdates = comms.Listen[int](b.address)
	go func() {
		for newValue := range b.listenUpdates.C {
			b.firstUpdate.Trigger() // First update received, we are ready for business.
			gonbui.Logf("Select(%s): new value is %q (%d)", b.htmlId, b.options[newValue], newValue)
			b.currentValue = newValue
		}
	}()

	parts := make([]string, 0, len(b.options)+3)
	parts = append(parts, fmt.Sprintf(`<select id="%s">`, b.htmlId))
	for ii, v := range b.options {
		var selected string
		if ii == b.defaultValue {
			selected = ` selected`
		}
		parts = append(parts, fmt.Sprintf(`<option value="%d"%s>%s</option>`, ii, selected, v))
	}
	parts = append(parts, "</select>")
	html := strings.Join(parts, "\n")
	if b.parentHtmlId == "" {
		gonbui.DisplayHtml(html)
	} else {
		dom.Append(b.parentHtmlId, html)
	}

	var buf bytes.Buffer
	data := struct {
		Address, HtmlId string
		Values          []string
	}{
		Address: b.address,
		HtmlId:  b.htmlId,
		Values:  b.options,
	}
	err := tmplSelectJs.Execute(&buf, data)
	if err != nil {
		panicf("Select template is invalid!? Please report the error to GoNB: %v", err)
	}
	dom.TransientJavascript(buf.String())

	b.firstUpdate.Wait()
	return b
}

// Listen returns an `AddressChannel[int]` (a wrapper for a `chan int`) that receives the index to the a counter each time the
// select is changed.
//
// Close the returned channel (`Close()` method) to unsubscribe from these messages and release the resources.
//
// It can only be called after the Slider is created with Done, otherwise it panics.
func (b *SelectBuilder) Listen() *comms.AddressChan[int] {
	if !b.built {
		panicf("SelectBuilder.Listen can only be called after the slider was created with `Done()` method")
	}
	return comms.Listen[int](b.address)
}

// HtmlId returns the `id` used in the widget HTML element created.
func (b *SelectBuilder) HtmlId() string {
	return b.htmlId
}

// Address returns the address used to communicate to the widgets HTML element.
func (b *SelectBuilder) Address() string {
	return b.address
}

// Value returns the current value set by the widget.
func (b *SelectBuilder) Value() int {
	return b.currentValue
}

// SetValue sets the value of the widget, communicating that with the UI.
func (b *SelectBuilder) SetValue(value int) {
	comms.Send(b.address, value)
	b.currentValue = value
}
