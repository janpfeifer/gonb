package goplsclient

import "time"

// Simple Latch implementation.

// Latch based on channels that can be waited/selected on.
// It's considered enabled (or "on") if the channel is closed.
type Latch chan struct{}

// State return true if Latch is enabled (the channel closed).
func (l Latch) State() bool {
	select {
	case <-l:
		return true
	default:
		return false
	}
}

// Wait waits until the latch is enabled.
func (l Latch) Wait() { <-l }

// WaitTimeout returns true if the latch was enabled. If timeout is expired, returns false.
func (l Latch) WaitTimeout(timeout time.Duration) bool {
	timer := time.After(timeout)
	select {
	case <-l:
		return true
	case <-timer:
		return false
	}
}

// Enable latch, triggering anyone waiting for it. It closes the underlying channel.
func (l Latch) Enable() {
	close(l)
}
