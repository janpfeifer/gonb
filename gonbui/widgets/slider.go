package widgets

import (
	"bytes"
	_ "embed"
	"fmt"
	"github.com/janpfeifer/gonb/gonbui"
	"github.com/janpfeifer/gonb/gonbui/comms"
	"github.com/janpfeifer/gonb/gonbui/dom"
	"k8s.io/klog/v2"
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
	min, max, value int

	// listenUpdates is the channel used to keep tabs of the updates.
	listenUpdates *comms.AddressChan[int]
}

// Slider returns a builder object that builds a new slider with the range
// and value given by `min`, `max` and `value`.
//
// Call `Done` method when you finish configuring the SliderBuilder.
func Slider(min, max, value int) *SliderBuilder {
	return &SliderBuilder{
		min:    min,
		max:    max,
		value:  value,
		htmlId: "gonb_slider_" + gonbui.UniqueId(),
	}
}

// Address configures the slider to use the given address to communicate its state
// with the front-end.
// The state is an int value that is incremented every time the slider is pressed.
//
// The default is to use randomly created unique address.
//
// It panics if called after the slider is built.
func (s *SliderBuilder) Address(address string) *SliderBuilder {
	if s.built {
		panicf("SliderBuilder cannot change parameters after it is built")
	}
	s.address = address
	return s
}

// AppendTo defines an id of the parent element in the DOM (in the front-end)
// where to insert the slider.
//
// If not defined, it will simply display it as default in the output of the cell.
func (s *SliderBuilder) AppendTo(parentHtmlId string) *SliderBuilder {
	if s.built {
		panicf("SliderBuilder cannot change parameters after it is built")
	}
	s.parentHtmlId = parentHtmlId
	return s
}

func (s *SliderBuilder) Done() *SliderBuilder {
	if s.built {
		panicf("SliderBuilder.Done already called!?")
	}
	s.built = true
	if s.address == "" {
		s.address = "/slider/" + gonbui.UniqueId()
	}

	// Record incoming slider updates.
	s.listenUpdates = comms.Listen[int](s.address)
	go func() {
		for newValue := range s.listenUpdates.C {
			klog.V(2).Infof("Slider(%s): new value is %d", s.htmlId, newValue)
			s.value = newValue
		}
	}()

	html := fmt.Sprintf(`<input type="range" id="%s" min="%d" max="%d" value="%d"/>`,
		s.htmlId, s.min, s.max, s.value)
	if s.parentHtmlId == "" {
		gonbui.DisplayHtml(html)
	} else {
		dom.Append(s.parentHtmlId, html)
	}

	var buf bytes.Buffer
	data := struct {
		Address, HtmlId string
	}{
		Address: s.address,
		HtmlId:  s.htmlId,
	}
	err := tmplSliderJs.Execute(&buf, data)
	if err != nil {
		panicf("Slider template is invalid!? Please report the error to GoNB: %v", err)
	}
	dom.TransientJavascript(buf.String())
	return s
}

// Listen returns an `AddressChannel[int]` (a wrapper for a `chan int`) that receives a counter each time the
// slider is changed.
//
// Close the returned channel (`Close()` method) to unsubscribe from these messages and release the resources.
//
// It can only be called after the Slider is created with Done, otherwise it panics.
func (s *SliderBuilder) Listen() *comms.AddressChan[int] {
	if !s.built {
		panicf("SliderBuilder.Listen can only be called after the slider was created with `Done()` method")
	}
	return comms.Listen[int](s.address)
}

// GetHtmlId returns the `id` used in the slider (`<input type="range">`) element created when
// `Done` is called.
func (s *SliderBuilder) GetHtmlId() string {
	return s.htmlId
}

// GetValue returns the current value.
func (s *SliderBuilder) GetValue() int {
	return s.value
}

// SetValue sets the value of the slider, communicating that with the UI.
func (s *SliderBuilder) SetValue(value int) {
	comms.Send(s.address, value)
	s.value = value
}
