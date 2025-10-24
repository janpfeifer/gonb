// Package util holds internal utility (or helper) functions.
package util

import (
	"runtime"

	"k8s.io/klog/v2"
)

// GetStackTrace returns a formatted stack trace of the goroutine that calls it.
func GetStackTrace() string {
	// Stack returns a formatted stack trace of the goroutine that calls it.
	// It accepts a byte slice, which it will grow if it's too small.
	// A small buffer is usually enough.
	buf := make([]byte, 4096)
	n := runtime.Stack(buf, false) // `false` means just the current goroutine
	return string(buf[:n])
}

// ReportError reports an error to the log, but otherwise ignores it.
func ReportError(err error) {
	if err != nil {
		klog.Warningf("Unhandled error: %+v", err)
	}
}
