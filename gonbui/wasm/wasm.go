// Package wasm defines several utilities to facilitate the writing of small WASM
// widgets in GoNB (or elsewhere).
//
// It's based on `gowebapi`, but extends it in ways to make it ergonomic.
//
// The variable `IsWasm` can be used to check in runtime if a program was compiled
// for wasm -- in case this is needed. This is the only symbol exported by this
// package for non-wasm builds.
//
// It's built on top of the library `github.com/gowebapi/webapi`, a wrapper
// to convert most web APIs directly to Go methods -- as opposed to using
// the "syscall/js" indirect calling methods.
//
// **Warning**: this is still experimental, and the API may change. Hopefully
// someone will improve `gowebapi` or create something new better.
package wasm

import (
	"flag"
	"fmt"
	"github.com/gowebapi/webapi"
	"github.com/gowebapi/webapi/core/js"
	"github.com/gowebapi/webapi/dom"
	"github.com/gowebapi/webapi/dom/domcore"
	"github.com/gowebapi/webapi/html"
	"os"
	"reflect"
	"strings"
)

var (
	// Doc is a shortcut to the webpage's DOM document.
	Doc *webapi.Document

	// Win is a shortcut to the webpage's DOM window.
	Win *webapi.Window
)

// Initialize document/window.
func init() {
	// WASM tests don't have Document/Window.
	if flag.Lookup("test.v") != nil {
		// If used in test, it is not being run in a browser, so no document is available.
		return
	}

	// Get document and parse URI and query parameters.
	Doc = webapi.GetDocument()
	Win = webapi.GetWindow()
}

// WaitForever waits on a channel forever, preventing the Go WASM program to ever exit.
// This is needed on a program that will be listening to user interaction.
// Use at the end of the program.
func WaitForever() {
	<-make(chan struct{})
}

// ParseFlags should take as parameters the arguments passed to Wasm by GoNB, in the variable
// `GonbWasmArgs`.
// It sets `os.Args` and uses that to parse flags as usual.
//
// Example of cell:
//
//		```
//		%wasm
//		import "github.com/janpfeifer/gonb/gonbui/wasm"
//	 var flagName = flag.String("name", "", "enter your name")
//		%% --name="World"
//	 wasm.ParseFlags(GonbWasmArgs)
//	 wasm.Alertf("Hello %s!", *flagName)
//		```
func ParseFlags(args []string) {
	// We need to include Args[0], which we hard-code to "wasm".
	os.Args = append([]string{"wasm"}, args...)
	flag.Parse()
}

// Alert opens up a pop-up with the given msg.
func Alert(msg string) {
	js.Global().Get("alert").Invoke(msg)
}

// Alertf opens up a pop-up with the formatted msg.
func Alertf(msg string, args ...any) {
	js.Global().Get("alert").Invoke(fmt.Sprintf(msg, args...))
}

// ById returns the HTML element with the given Id, or nil if didn't find it.
func ById(id string) *html.HTMLElement {
	e := Doc.GetElementById(id)
	if e == nil {
		return nil
	}
	return html.HTMLElementFromWrapper(e)
}

// KeyValue is a simple structure used for different things.
// Key and Value are both string.
type KeyValue struct {
	Key, Value string
}

// NewElem creates a new DOM element with the tag / attributes given.
// Attributes can be given without a value (e.g.: `readonly`) or as a key/value pair (`width=10`), split by the
// first "=".
func NewElem(tag string, attributes ...string) *dom.Element {
	e := Doc.CreateElement(tag, nil)
	for _, attr := range attributes {
		var key, value string
		if eqPos := strings.Index(attr, "="); eqPos > 0 {
			key = attr[:eqPos]
			value = attr[eqPos+1:]
		} else {
			key = attr // No "=".
		}
		e.SetAttribute(key, value)
	}
	return e
}

// IsNil checks for nil interface or pointer.
func IsNil(e Compatible) bool {
	if e == nil || (reflect.ValueOf(e).Kind() == reflect.Ptr && reflect.ValueOf(e).IsNil()) {
		return true
	}
	return false
}

// Append will append the child to the parent node.
func Append(parent, child NodeCompatible) {
	cn := AsNode(child)
	if cn == nil {
		fmt.Println("ChildNode failed to convert to node!")
		return
	}
	parent.AppendChild(cn)
}

// AppendHTML inserts html at the end of the current element.
// It's a shortcut to InsertAdjacentHTML("beforeend", html).
func AppendHTML(parent Element, html string) {
	parent.InsertAdjacentText("beforeend", html)
}

// TRAddValue adds a cell to a row (`tr`) in a table.
func TRAddValue(tr *html.HTMLTableRowElement, value interface{}) *html.HTMLTableColElement {
	td := html.HTMLTableColElementFromJS(NewElem("td").JSValue())
	td.SetInnerText(fmt.Sprintf("%s", value))
	Append(tr, td)
	return td
}

// On adds a callback to the element when event type happens, using `element.AddEventListener`.
// All event options default to false. See OnWith() to set options.
func On(element EventTargetCompatible, eventType string, callback func(ev EventCompatible)) {
	OnWith(element, eventType, callback, EventOptions{})
}

// EventOptions are the options passed to AddEventListener used in the `OnWith` function.
type EventOptions struct {
	Capture, Once, Passive bool
}

// OnWith adds a callback to the element when eventType happens, using `element.AddEventListener`.
// There is no easy way to remove the event listener from Go :(
//
// Event types: "change", "load", "click", "mouseup", etc.
func OnWith(element EventTargetCompatible, eventType string, callback func(ev EventCompatible), options EventOptions) {
	if len(eventType) < 2 || strings.ToLower(eventType[:2]) == "on" {
		fmt.Printf("Suspicious eventType name: %q -- REMOVE THE \"on\" PREFIX from the eventType", eventType)
		return
	}
	jsOptions := map[string]any{
		"Capture": options.Capture,
		"Once":    options.Once,
		"Passive": options.Passive,
	}
	element.AddEventListener(
		eventType,
		domcore.NewEventListenerFunc(func(ev *domcore.Event) { callback(ev) }),
		domcore.UnionFromJS(js.ValueOf(jsOptions)))
}

// DiscardEvent is an event handler that stops propagation and the
// default browser handling. Can be used as a parameter to the `On` function.
func DiscardEvent(e *domcore.Event) {
	e.StopPropagation()
	e.PreventDefault()
}
