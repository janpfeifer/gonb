package goexec

import (
	"math/rand"
	"strconv"
	"testing"
)

func createTestState(t *testing.T) *State {
	id := strconv.Itoa(rand.Int())
	s, err := New(id)
	if err != nil {
		t.Fatalf("Failed to create go executor: %+v", err)
	}
	return s
}

func TestState_InspectPath(t *testing.T) {
	_ = createTestState(t)
}
