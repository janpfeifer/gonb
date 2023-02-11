package goexec

import (
	"github.com/janpfeifer/gonb/kernel"
	"math/rand"
	"strconv"
	"testing"
)

func CreateTestState(t *testing.T) *State {
	id := strconv.Itoa(rand.Int())
	s, err := New(id)
	if err != nil {
		t.Fatalf("Failed to create go executor: %+v", err)
	}
	return s
}

// TestMessage is a mock for a kernel.Message.
type TestMessage struct {
	test     *testing.T
	err      error
	Composed kernel.ComposedMsg
	kernel   *kernel.Kernel
}

// Error returns the error receiving the message, or nil if no error.
func (m *TestMessage) Error() error { return m.err }

// Ok returns whether there were no errors receiving the message.
func (m *TestMessage) Ok() bool { return m.err == nil }

// ComposedMsg that started the current Message -- it's contained by Message.
func (m *TestMessage) ComposedMsg() kernel.ComposedMsg { return m.Composed }

// Kernel returns reference to the Kernel connections from where this Message was created.
func (m *TestMessage) Kernel() *kernel.Kernel { return m.kernel }

// Publish creates a new ComposedMsg and sends it back to the return identities over the
// IOPub channel.
func (m *TestMessage) Publish(msgType string, content interface{}) error {
	m.test.Fatal("Publish not implemented")
	return nil
}

func (m *TestMessage) PromptInput(prompt string, password bool, onInput kernel.OnInputFn) error {
	m.test.Fatal("PromptInput not implemented")
	return nil
}

// CancelInput will cancel any `input_request` message sent by PromptInput.
func (m *TestMessage) CancelInput() error {
	m.test.Fatal("CancelInput not implemented")
	return nil
}

func (m *TestMessage) DeliverInput() error {
	m.test.Fatal("DeliverInput not implemented")
	return nil
}
func (m *TestMessage) Reply(msgType string, content interface{}) error {
	m.test.Fatal("Reply not implemented")
	return nil
}

func TestState_InspectPath(t *testing.T) {
	_ = CreateTestState(t)
	//msg := TestMessage{test: t}
}
