package wasm

import (
	"github.com/gowebapi/webapi/core/js"
	"github.com/gowebapi/webapi/css/animations/webani"
	"github.com/gowebapi/webapi/css/cssom"
	"github.com/gowebapi/webapi/css/cssom/view"
	"github.com/gowebapi/webapi/css/typedom"
	"github.com/gowebapi/webapi/dom"
	"github.com/gowebapi/webapi/dom/domcore"
	"github.com/gowebapi/webapi/dom/geometry"
	"github.com/gowebapi/webapi/javascript"
)

// Compatible is an interface implemented by most of webapi types.
type Compatible interface {
	JSValue() js.Value
}

// EventTargetCompatible is the interface that all Element/Node types of the DOM implement.
type EventTargetCompatible interface {
	Compatible
	AddEventListener(_type string, callback *domcore.EventListenerValue, options *domcore.Union)
}

// NodeCompatible is an interface implemented by several of the webapi types that support having sub-nodes.
type NodeCompatible interface {
	EventTargetCompatible
	AppendChild(node *dom.Node) (_result *dom.Node)
	ChildNodes() *dom.NodeList
	RemoveChild(child *dom.Node) (_result *dom.Node)
}

// HtmlElementCompatible is anything that behaves like a dom.HtmlElement.
type HtmlElementCompatible interface {
	Element
	Style() *cssom.CSSStyleDeclaration
}

type EventCompatible interface {
	Compatible
	Bubbles() bool
	PreventDefault()
	StopPropagation()
	StopImmediatePropagation()
}

type MouseEventCompatible interface {
	EventCompatible
	Button() int

	AltKey() bool
	CtrlKey() bool
	ShiftKey() bool

	OffsetX() float64
	OffsetY() float64
}

type KeyboardEventCompatible interface {
	EventCompatible
	Key() string
	Code() string
	KeyCode() uint
	CharCode() uint

	AltKey() bool
	CtrlKey() bool
	ShiftKey() bool
	MetaKey() bool

	Repeat() bool
	IsComposing() bool
}

// Key codes that can be used when comparing with a KeyboardEventCompatible.KeyCode()
const (
	KeyCodeNone  = uint(0)
	KeyCodeLeft  = 37
	KeyCodeUp    = 38
	KeyCodeRight = 39
	KeyCodeDown  = 40
)

// Element is the interface implemented by webapi.Element structure.
type Element interface {
	// This was automatically generated from grepping webapi/dom/dom_js.go:
	//
	// ```bash
	// grep 'func (_this \*dom.Element' dom/dom_js.go | sed 's/func (_this \*dom.Element) //g; s/ {$//g;'
	// ```

	NamespaceURI() *string
	Prefix() *string
	LocalName() string
	TagName() string
	Id() string
	SetId(value string)
	ClassName() string
	SetClassName(value string)
	ClassList() *domcore.DOMTokenList
	Slot() string
	SetSlot(value string)
	Attributes() *dom.NamedNodeMap
	ShadowRoot() *dom.ShadowRoot
	InnerHTML() string
	SetInnerHTML(value string)
	OuterHTML() string
	SetOuterHTML(value string)
	ScrollTop() float64
	SetScrollTop(value float64)
	ScrollLeft() float64
	SetScrollLeft(value float64)
	ScrollWidth() int
	ScrollHeight() int
	ClientTop() int
	ClientLeft() int
	ClientWidth() int
	ClientHeight() int
	OnFullscreenChange() domcore.EventHandlerFunc
	OnFullscreenError() domcore.EventHandlerFunc
	Children() *dom.HTMLCollection
	FirstElementChild() *dom.Element
	LastElementChild() *dom.Element
	ChildElementCount() uint
	PreviousElementSibling() *dom.Element
	NextElementSibling() *dom.Element
	AssignedSlot() js.Value
	Role() *string
	SetRole(value *string)
	AriaActiveDescendant() *string
	SetAriaActiveDescendant(value *string)
	AriaAtomic() *string
	SetAriaAtomic(value *string)
	AriaAutoComplete() *string
	SetAriaAutoComplete(value *string)
	AriaBusy() *string
	SetAriaBusy(value *string)
	AriaChecked() *string
	SetAriaChecked(value *string)
	AriaColCount() *string
	SetAriaColCount(value *string)
	AriaColIndex() *string
	SetAriaColIndex(value *string)
	AriaColSpan() *string
	SetAriaColSpan(value *string)
	AriaControls() *string
	SetAriaControls(value *string)
	AriaCurrent() *string
	SetAriaCurrent(value *string)
	AriaDescribedBy() *string
	SetAriaDescribedBy(value *string)
	AriaDetails() *string
	SetAriaDetails(value *string)
	AriaDisabled() *string
	SetAriaDisabled(value *string)
	AriaErrorMessage() *string
	SetAriaErrorMessage(value *string)
	AriaExpanded() *string
	SetAriaExpanded(value *string)
	AriaFlowTo() *string
	SetAriaFlowTo(value *string)
	AriaHasPopup() *string
	SetAriaHasPopup(value *string)
	AriaHidden() *string
	SetAriaHidden(value *string)
	AriaInvalid() *string
	SetAriaInvalid(value *string)
	AriaKeyShortcuts() *string
	SetAriaKeyShortcuts(value *string)
	AriaLabel() *string
	SetAriaLabel(value *string)
	AriaLabelledBy() *string
	SetAriaLabelledBy(value *string)
	AriaLevel() *string
	SetAriaLevel(value *string)
	AriaLive() *string
	SetAriaLive(value *string)
	AriaModal() *string
	SetAriaModal(value *string)
	AriaMultiLine() *string
	SetAriaMultiLine(value *string)
	AriaMultiSelectable() *string
	SetAriaMultiSelectable(value *string)
	AriaOrientation() *string
	SetAriaOrientation(value *string)
	AriaOwns() *string
	SetAriaOwns(value *string)
	AriaPlaceholder() *string
	SetAriaPlaceholder(value *string)
	AriaPosInSet() *string
	SetAriaPosInSet(value *string)
	AriaPressed() *string
	SetAriaPressed(value *string)
	AriaReadOnly() *string
	SetAriaReadOnly(value *string)
	AriaRelevant() *string
	SetAriaRelevant(value *string)
	AriaRequired() *string
	SetAriaRequired(value *string)
	AriaRoleDescription() *string
	SetAriaRoleDescription(value *string)
	AriaRowCount() *string
	SetAriaRowCount(value *string)
	AriaRowIndex() *string
	SetAriaRowIndex(value *string)
	AriaRowSpan() *string
	SetAriaRowSpan(value *string)
	AriaSelected() *string
	SetAriaSelected(value *string)
	AriaSetSize() *string
	SetAriaSetSize(value *string)
	AriaSort() *string
	SetAriaSort(value *string)
	AriaValueMax() *string
	SetAriaValueMax(value *string)
	AriaValueMin() *string
	SetAriaValueMin(value *string)
	AriaValueNow() *string
	SetAriaValueNow(value *string)
	AriaValueText() *string
	SetAriaValueText(value *string)
	AddEventFullscreenChange(listener func(event *domcore.Event, currentTarget *dom.Element)) js.Func
	SetOnFullscreenChange(listener func(event *domcore.Event, currentTarget *dom.Element)) js.Func
	AddEventFullscreenError(listener func(event *domcore.Event, currentTarget *dom.Element)) js.Func
	SetOnFullscreenError(listener func(event *domcore.Event, currentTarget *dom.Element)) js.Func
	HasAttributes() (_result bool)
	GetAttributeNames() (_result []string)
	GetAttribute(qualifiedName string) (_result *string)
	GetAttributeNS(namespace *string, localName string) (_result *string)
	SetAttribute(qualifiedName string, value string)
	SetAttributeNS(namespace *string, qualifiedName string, value string)
	RemoveAttribute(qualifiedName string)
	RemoveAttributeNS(namespace *string, localName string)
	ToggleAttribute(qualifiedName string, force *bool) (_result bool)
	HasAttribute(qualifiedName string) (_result bool)
	HasAttributeNS(namespace *string, localName string) (_result bool)
	GetAttributeNode(qualifiedName string) (_result *dom.Attr)
	GetAttributeNodeNS(namespace *string, localName string) (_result *dom.Attr)
	SetAttributeNode(attr *dom.Attr) (_result *dom.Attr)
	SetAttributeNodeNS(attr *dom.Attr) (_result *dom.Attr)
	RemoveAttributeNode(attr *dom.Attr) (_result *dom.Attr)
	AttachShadow(init *dom.ShadowRootInit) (_result *dom.ShadowRoot)
	Closest(selectors string) (_result *dom.Element)
	Matches(selectors string) (_result bool)
	WebkitMatchesSelector(selectors string) (_result bool)
	GetElementsByTagName(qualifiedName string) (_result *dom.HTMLCollection)
	GetElementsByTagNameNS(namespace *string, localName string) (_result *dom.HTMLCollection)
	GetElementsByClassName(classNames string) (_result *dom.HTMLCollection)
	InsertAdjacentElement(where string, element *dom.Element) (_result *dom.Element)
	InsertAdjacentText(where string, data string)
	InsertAdjacentHTML(position string, text string)
	GetFragmentInformation(filter dom.FragmentFilter) (_result *dom.PromiseDeadFragmentInformation)
	ComputedStyleMap() (_result *typedom.StylePropertyMapReadOnly)
	GetClientRects() (_result *geometry.DOMRectList)
	GetBoundingClientRect() (_result *geometry.DOMRect)
	ScrollIntoView(arg *dom.Union)
	Scroll(options *view.ScrollToOptions)
	Scroll2(x float64, y float64)
	ScrollTo(options *view.ScrollToOptions)
	ScrollTo2(x float64, y float64)
	ScrollBy(options *view.ScrollToOptions)
	ScrollBy2(x float64, y float64)
	RequestFullscreen(options *dom.FullscreenOptions) (_result *javascript.PromiseVoid)
	SetPointerCapture(pointerId int)
	ReleasePointerCapture(pointerId int)
	HasPointerCapture(pointerId int) (_result bool)
	RequestPointerLock()
	GetBoxQuads(options *view.BoxQuadOptions) (_result []*geometry.DOMQuad)
	ConvertQuadFromNode(quad *geometry.DOMQuadInit, from *dom.Union, options *view.ConvertCoordinateOptions) (_result *geometry.DOMQuad)
	ConvertRectFromNode(rect *geometry.DOMRectReadOnly, from *dom.Union, options *view.ConvertCoordinateOptions) (_result *geometry.DOMQuad)
	ConvertPointFromNode(point *geometry.DOMPointInit, from *dom.Union, options *view.ConvertCoordinateOptions) (_result *geometry.DOMPoint)
	Prepend(nodes ...*dom.Union)
	Append(nodes ...*dom.Union)
	QuerySelector(selectors string) (_result *dom.Element)
	QuerySelectorAll(selectors string) (_result *dom.NodeList)
	Before(nodes ...*dom.Union)
	After(nodes ...*dom.Union)
	ReplaceWith(nodes ...*dom.Union)
	Remove()
	Animate(keyframes *javascript.Object, options *dom.Union) (_result *webani.Animation)
	GetAnimations() (_result []*webani.Animation)
}
