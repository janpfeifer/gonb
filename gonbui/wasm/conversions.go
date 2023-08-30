package wasm

import (
	"github.com/gowebapi/webapi/dom"
	"github.com/gowebapi/webapi/html"
	"github.com/gowebapi/webapi/html/canvas"
	"github.com/gowebapi/webapi/html/htmlevent"
	"reflect"
)

// AsNode converts any pointer to a struct that extends dom.Node, back to a *dom.Node.
func AsNode(e NodeCompatible) *dom.Node {
	if IsNil(e) {
		return nil
	}
	if n, ok := e.(*dom.Node); ok {
		return n
	}
	if reflect.ValueOf(e).Kind() != reflect.Ptr {
		return nil
	}
	val := reflect.Indirect(reflect.ValueOf(e))
	val = val.FieldByName("Node") // Get dom.Node Value.
	if !val.IsValid() {
		return nil
	}
	val = val.Addr() // Get the *dom.Node Value.
	return val.Interface().(*dom.Node)
}

// Disable disables buttons.
func Disable(e HtmlElementCompatible) {
	e.SetAttribute("disable", "")
}

// Enable enables buttons.
func Enable(e HtmlElementCompatible) {
	e.RemoveAttribute("disable")
}

// AsHTML casts some type of element as an HTMLElement.
func AsHTML(e EventTargetCompatible) *html.HTMLElement {
	if IsNil(e) {
		return nil
	}
	return html.HTMLElementFromJS(e.JSValue())
}

// AsInput casts some type of element as an HTMLInputElement.
func AsInput(e EventTargetCompatible) *html.HTMLInputElement {
	if IsNil(e) {
		return nil
	}
	return html.HTMLInputElementFromJS(e.JSValue())
}

// AsButton casts some type of element as an HTMLButtonElement.
func AsButton(e EventTargetCompatible) *html.HTMLButtonElement {
	if IsNil(e) {
		return nil
	}
	return html.HTMLButtonElementFromJS(e.JSValue())
}

// AsTable casts some type of element as an HTMLTableElement.
func AsTable(e EventTargetCompatible) *html.HTMLTableElement {
	if IsNil(e) {
		return nil
	}
	return html.HTMLTableElementFromJS(e.JSValue())
}

// AsTR casts some type of element as an HTMLTableRowElement.
func AsTR(e EventTargetCompatible) *html.HTMLTableRowElement {
	if IsNil(e) {
		return nil
	}
	return html.HTMLTableRowElementFromJS(e.JSValue())
}

// AsSpan casts some type of element as an HTMLSpanElement.
func AsSpan(e EventTargetCompatible) *html.HTMLSpanElement {
	if IsNil(e) {
		return nil
	}
	return html.HTMLSpanElementFromJS(e.JSValue())
}

// AsSelect casts some type of element as an HTMLSelectElement.
func AsSelect(e EventTargetCompatible) *html.HTMLSelectElement {
	if IsNil(e) {
		return nil
	}
	return html.HTMLSelectElementFromJS(e.JSValue())
}

// AsOption casts some type of element as an HTMLOptionElement.
func AsOption(e EventTargetCompatible) *html.HTMLOptionElement {
	if IsNil(e) {
		return nil
	}
	return html.HTMLOptionElementFromJS(e.JSValue())
}

// AsCanvas casts some type of element as an HTMLCanvasElement.
func AsCanvas(e EventTargetCompatible) *canvas.HTMLCanvasElement {
	if IsNil(e) {
		return nil
	}
	return canvas.HTMLCanvasElementFromJS(e.JSValue())
}

// AsImage casts some type of element as an HTMLImageElement.
func AsImage(e EventTargetCompatible) *html.HTMLImageElement {
	if IsNil(e) {
		return nil
	}
	return html.HTMLImageElementFromJS(e.JSValue())
}

// AsScript casts some type of element as an HTMLScriptElement.
func AsScript(e EventTargetCompatible) *html.HTMLScriptElement {
	if IsNil(e) {
		return nil
	}
	return html.HTMLScriptElementFromJS(e.JSValue())
}

// AsMouseEvent casts some type of element as an HTMLMouseEventElement.
func AsMouseEvent(e EventCompatible) *htmlevent.MouseEvent {
	if IsNil(e) {
		return nil
	}
	return htmlevent.MouseEventFromJS(e.JSValue())
}

// AsKeyboardEvent casts some type of element as an HTMLKeyboardEventElement.
func AsKeyboardEvent(e EventCompatible) *htmlevent.KeyboardEvent {
	if IsNil(e) {
		return nil
	}
	return htmlevent.KeyboardEventFromJS(e.JSValue())
}

// AsAudio casts some type of element as an audio.AudioNode.
func AsAudio(e *html.HTMLElement) *Audio {
	if IsNil(e) {
		return nil
	}
	return &Audio{e.JSValue()}
}
