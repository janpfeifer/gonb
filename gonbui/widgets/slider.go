package widgets

import (
	"bytes"
	_ "embed"
	"fmt"
	"github.com/janpfeifer/gonb/common"
	"github.com/janpfeifer/gonb/gonbui"
	"github.com/janpfeifer/gonb/gonbui/comms"
	"github.com/janpfeifer/gonb/gonbui/dom"
	"text/template"
)

//go:embed slider.js
var sliderJs []byte

var tmplSliderJs = template.Must(template.New("sliderJs").Parse(
	string(sliderJs)))

// SliderBuilder is used to create a slider on the front-end.
type SliderBuilder struct {
	address, label, htmlId, parentHtmlId string
	onClick                              func()
	built                                bool

	// Parameters of the slider.
	min, max, currentValue int

	// listenUpdates is the channel used to keep tabs of the updates.
	listenUpdates *comms.AddressChan[int]
	firstUpdate   *common.Latch // If first update received.
}

// Slider returns a builder object that builds a new slider with the range
// and value given by `min`, `max` and `value`.
//
// Values (used for `Listen`, `Value` and `SetValue`) are integers representing
// the slider position.
//
// Call `Done` method when you finish configuring the SliderBuilder.
func Slider(min, max, value int) *SliderBuilder {
	return &SliderBuilder{
		min:          min,
		max:          max,
		currentValue: value,
		address:      "/select/" + gonbui.UniqueId(),
		htmlId:       "gonb_slider_" + gonbui.UniqueId(),
		firstUpdate:  common.NewLatch(),
	}
}

// WithHtmlId sets the id to use when creating the HTML element in the DOM.
// If not set, a unique one will be generated, and can be read with HtmlId.
//
// This can only be set before call to Done. If called afterward, it panics.
func (b *SliderBuilder) WithHtmlId(htmlId string) *SliderBuilder {
	if b.built {
		panicf("SliderBuilder cannot change parameters after it is built")
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
func (b *SliderBuilder) WithAddress(address string) *SliderBuilder {
	if b.built {
		panicf("SliderBuilder cannot change parameters after it is built")
	}
	b.address = address
	return b
}

// AppendTo defines an id of the parent element in the DOM (in the front-end)
// where to insert the widget.
//
// If not defined, it will simply display it as default in the output of the cell.
//
// It panics if called after the widget is built.
func (b *SliderBuilder) AppendTo(parentHtmlId string) *SliderBuilder {
	if b.built {
		panicf("SliderBuilder cannot change parameters after it is built")
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
func (b *SliderBuilder) Done() *SliderBuilder {
	if b.built {
		panicf("SliderBuilder.Done already called!?")
	}
	b.built = true

	// Record incoming slider updates.
	b.listenUpdates = comms.Listen[int](b.address)
	go func() {
		for newValue := range b.listenUpdates.C {
			b.firstUpdate.Trigger() // First update received, we are ready for business.
			gonbui.Logf("Slider(%s): new value is %d", b.htmlId, newValue)
			b.currentValue = newValue
		}
	}()

	html := fmt.Sprintf(`<input type="range" id="%s" min="%d" max="%d" value="%d"/>`,
		b.htmlId, b.min, b.max, b.currentValue)
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
	err := tmplSliderJs.Execute(&buf, data)
	if err != nil {
		panicf("Slider template is invalid!? Please report the error to GoNB: %v", err)
	}
	dom.TransientJavascript(buf.String())

	b.firstUpdate.Wait()
	return b
}

// Listen returns an `AddressChannel[int]` (a wrapper for a `chan int`) that receives a counter each time the
// slider is changed.
//
// Close the returned channel (`Close()` method) to unsubscribe from these messages and release the resources.
//
// It can only be called after the Slider is created with Done, otherwise it panics.
func (b *SliderBuilder) Listen() *comms.AddressChan[int] {
	if !b.built {
		panicf("SliderBuilder.Listen can only be called after the slider was created with `Done()` method")
	}
	return comms.Listen[int](b.address)
}

// HtmlId returns the `id` used in the widget HTML element created.
func (b *SliderBuilder) HtmlId() string {
	return b.htmlId
}

// Address returns the address used to communicate to the widgets HTML element.
func (b *SliderBuilder) Address() string {
	return b.address
}

// Value returns the current value set by the widget.
func (b *SliderBuilder) Value() int {
	return b.currentValue
}

// SetValue sets the value of the widget, communicating that with the UI.
func (b *SliderBuilder) SetValue(value int) {
	comms.Send(b.address, value)
	b.currentValue = value
}
