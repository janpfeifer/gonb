// Package widgets implement several simple widgets that can be used to
// make your Go programs interact with front-end widgets in a Jupyter
// Notebook, using GoNB kernel.
//
// Because most widgets will have many optional parameters, it uses
// the convention of calling the widget to create a "builder" object,
// have optional parameters as method calls, and then call `Done()`
// to actually display and start it.
//
// If you want to implement a new widget, checkout `gonb/gonbui/comms`
// package for the communication functionality, along with tools for
// building widgets.
package widgets

import (
	"github.com/janpfeifer/gonb/common"
	"github.com/janpfeifer/gonb/gonbui"
	"github.com/janpfeifer/gonb/gonbui/comms"
	"github.com/janpfeifer/gonb/gonbui/protocol"
)

// AddressChan provides a channel to listen to an Address in the communication between the front-end
// and GoNB.
// It can be used to interact with the front-end (the browser) in a notebook.
//
// Use Listen to create an AddressChan, and use `C` to receive the updates.
//
// It's a common output of widgets, to listen to its updates.
type AddressChan[T protocol.CommValueTypes] struct {
	C    chan T
	done *common.Latch
}

// IsClosed checks whether this AddressChannel is closed.
func (c *AddressChan[T]) IsClosed() bool {
	return !c.done.Test()
}

// Close closes the channel and unsubscribe from messages on this address, freeing up resources.
//
// AddressChan doesn't use many resources, one may just leak these without consequences if only a few thousand.
func (c *AddressChan[T]) Close() {
	c.done.Trigger()
}

// WaitClose waits until the AddressChan.Close is called.
func (c *AddressChan[T]) WaitClose() {
	c.done.Wait()
}

// Listen returns an unbuffered channel (`*AddressChannel[T]`) that listens to incoming updates from
// the front-end to the incoming address.
//
// This can be used to directly get updates to widgets that use those addresses.
//
// A few resources are used to subscribe to the address.
// Use `AddressChan[T].Close()` to release those resources, when done listening.
func Listen[T protocol.CommValueTypes](address string) *AddressChan[T] {
	gonbui.Logf("Listen(%q) started", address)
	c := &AddressChan[T]{
		C:    make(chan T),
		done: common.NewLatch(),
	}
	var subscriptionId comms.SubscriptionId
	subscriptionId = comms.Subscribe[T](address, func(_ string, value T) {
		ok := common.TrySend(c.C, value)
		if !ok {
			// User closed channel, we should unsubscribe.
			gonbui.Logf("Listen(%q) closed", address)
			comms.Unsubscribe(subscriptionId)
		}
	})
	go func() {
		// Wait AddressChannel to be closed to unsubscribe from updates.
		c.WaitClose()
		comms.Unsubscribe(subscriptionId)
	}()
	return c
}
