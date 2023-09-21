// Package comms implements a protocol of communication wih the front-ent (notebook), and
// it's used to implement widgets.
//
// The front-end uses a WebSocket to connect to Jupyter Server, which in turns uses
// ZeroMQ (a framework for communication) to talk to GoNB, which in turns uses named
// pipes to talk to the user program (when executing cells).
//
// Widgets developers can simply use this library as is.
// For GoNB developers there is a more detailed description of what is going on in
// [gonb/internal/websocket/README.md]().
//
// Errors on the underlying named pipes used to connect to GoNB can be checked with
// gonbui.Error().
package comms

import (
	"github.com/janpfeifer/gonb/gonbui"
	"github.com/janpfeifer/gonb/gonbui/protocol"
	"golang.org/x/exp/slices"
	"k8s.io/klog/v2"
	"log"
	"sync"
)

func init() {
	// Inject dispatcher.
	gonbui.OnCommValueUpdate = dispatchValueUpdates
}

// UpdateValue in the front-end, using "comms", a channel used to talk to a
// WebSocket in the browser (notebook).
//
// This is used to implement widgets, or arbitrary Javascript/Wasm code running
// in the front-end.
func UpdateValue[T protocol.CommValueTypes](address string, value T) {
	data := &protocol.DisplayData{
		Data: map[protocol.MIMEType]any{
			protocol.MIMECommValue: &protocol.CommValue{
				Address: address,
				Request: false,
				Value:   value,
			}},
	}
	gonbui.SendData(data)
}

// ReadValue from the front-end, using "comms", a channel used to talk to a
// WebSocket in the browser (notebook).
// It may lock waiting for a reply if something goes wrong with the channel in between
// the request, so consider running this in a goroutine and handling gracefully such cases (with
// a timeout?).
//
// Notice anyone subscribed (Subscribe) to the address will also receive the value read.
//
// This is used to implement widgets, or arbitrary Javascript/Wasm code running
// in the front-end.
func ReadValue[T protocol.CommValueTypes](address string) (value T) {
	if !gonbui.IsNotebook || gonbui.Error() != nil {
		return
	}
	var wg sync.WaitGroup
	wg.Add(1)
	id := Subscribe(address, func(address string, receivedValue T) {
		value = receivedValue
		wg.Done()
	})
	data := &protocol.DisplayData{
		Data: map[protocol.MIMEType]any{
			protocol.MIMECommValue: &protocol.CommValue{
				Address: address,
				Request: true,
			}},
	}
	gonbui.SendData(data)
	wg.Wait() // Wait for value to arrive and
	Unsubscribe(id)
	return
}

type internalCallbackFn func(address string, value any)

// SubscriptionID is returned upon a subscription, and is used to unsubscribe.
// It can be discarded if one is never going to unsubscribe.
type SubscriptionID int

type subscriptionRecord struct {
	id       SubscriptionID
	callback internalCallbackFn
}

var (
	// subscriptions hold all subscriptions of addresses.
	subscriptions           = make(map[string][]subscriptionRecord)
	nextSubscriptionId      = SubscriptionID(0) // Unique id for subscriptions.
	subscriptionIdToAddress []string
	muSubscriptions         sync.Mutex
)

// Subscribe to updates on the given address.
// It returns a SubscriptionID that can be used with Unsubscribe.
func Subscribe[T protocol.CommValueTypes](address string, callback func(address string, value T)) SubscriptionID {
	_ = gonbui.Open()
	muSubscriptions.Lock()
	id := nextSubscriptionId
	nextSubscriptionId++
	subscriptionIdToAddress = append(subscriptionIdToAddress, address)
	s := subscriptions[address]
	newAddress := len(s) == 0

	// Create a wrapper callback that converts the incoming `any` type to the selected
	// user type during subscription.
	fn := func(address string, value any) {
		typedValue, ok := value.(T)
		if !ok {
			// If conversion fails, we warn the user, and callback anyway with the default (zero)
			// value for the users given type.
			log.Printf("Warning: gonbui/comms: received from front-end type %T for address %q, wanted type %T",
				value, address, typedValue)
		}
		callback(address, typedValue)
	}

	subscriptions[address] = append(
		s,
		subscriptionRecord{id: id, callback: fn})
	muSubscriptions.Unlock()

	// Inform GoNB to start sending messages for this address.
	if newAddress {
		// If the first time someone is subscribing to address, send message to
		// subscribe.
		data := &protocol.DisplayData{
			Data: map[protocol.MIMEType]any{
				protocol.MIMECommSubscribe: &protocol.CommSubscription{
					Address:     address,
					Unsubscribe: false,
				}},
		}
		gonbui.SendData(data)
	}

	return id
}

// Unsubscribe from receiving front-end updates, using the SubscriptionID returned by Subscribe.
func Unsubscribe(id SubscriptionID) {
	if gonbui.Open() != nil {
		return
	}
	muSubscriptions.Lock()
	if int(id) > len(subscriptionIdToAddress) {
		return
	}
	address := subscriptionIdToAddress[id]
	s := subscriptions[address]
	s = slices.DeleteFunc(s, func(e subscriptionRecord) bool {
		return e.id == id
	})
	if len(s) > 0 {
		// More subscriptions to the address, simply update it.
		subscriptions[address] = s
	} else {
		delete(subscriptions, address)
	}
	muSubscriptions.Unlock()

	// No more subscriptions to the address.
	if len(s) == 0 {
		data := &protocol.DisplayData{
			Data: map[protocol.MIMEType]any{
				protocol.MIMECommSubscribe: &protocol.CommSubscription{
					Address:     address,
					Unsubscribe: true,
				}},
		}
		gonbui.SendData(data)
	}
}

// dispatchValueUpdates handles new incoming value updates.
func dispatchValueUpdates(valueMsg *protocol.CommValue) {
	if gonbui.Open() != nil {
		return
	}
	klog.Infof("dispatchValueUpdates(%q, %v)", valueMsg.Address, valueMsg.Value)
	if valueMsg.Request {
		log.Printf("WARNING: gonbui/comms.DeliverValue(%+v): invalid message with Request=true received from front-end!?", valueMsg)
		return
	}

	muSubscriptions.Lock()
	address := valueMsg.Address
	value := valueMsg.Value
	subscribers, found := subscriptions[address]
	if !found {
		// No (longer any) subscribers to the address, simply drop.
		return
	}
	for _, s := range subscribers {
		go s.callback(address, value)
	}
	muSubscriptions.Unlock()
}
