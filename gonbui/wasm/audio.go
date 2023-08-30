package wasm

import "github.com/gowebapi/webapi/core/js"

// Audio is a simple wrapper over JS value that should represent a "<audio ...>" node.
type Audio struct {
	value js.Value
}

// Play will play the audio object using Javascript functionality.
func (a *Audio) Play() { a.value.Call("play") }
