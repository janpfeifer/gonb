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
	"github.com/janpfeifer/gonb/gonbui"
	"github.com/janpfeifer/gonb/gonbui/comms"
	"github.com/janpfeifer/gonb/gonbui/protocol"
	"github.com/pkg/errors"
)

// panicf panics with an error constructed with the given format and args.
func panicf(format string, args ...any) {
	panic(errors.Errorf(format, args...))
}

// Listen returns an unbuffered channel that listens to incoming updates from
// the front-end to the incoming address.
//
// This can be used to directly get updates to widgets that use those addresses.
func Listen[T protocol.CommValueTypes](address string) chan T {
	c := make(chan T)
	go func(c chan T) {
		c2 := make(chan T)
		subscriptionId := comms.Subscribe[T](address, func(_ string, value T) {
			c2 <- value
		})

		for {
			select {
			case value := <-c2:
				// Deliver incoming value.
				c <- value
			case _ = <-c:
				// User closed channel, we should unsubscribe.
				gonbui.Logf("Listen(%q) closed", address)
				comms.Unsubscribe(subscriptionId)
				close(c2)
				return
			}
		}
	}(c)
	return c
}
