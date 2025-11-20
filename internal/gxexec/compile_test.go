//go:build gx

package gxexec

import (
	"strings"
	"testing"
)

func TestCompileSimple(t *testing.T) {
	err := compile(`
	package gxnb
 	func l2Squared(x [_ax]float32) float32 {
		result := einsum(x{i} * x{i})
		return result
	}
 	`)
	if err != nil {
		t.Fatalf("expected no error compiling simple gx code, got: %v", err)
	}
}

func TestCompilePackageName(t *testing.T) {
	err := compile(`
	package main // the package name shouldn't matter
 	func l2Squared(x [_ax]float32) float32 {
		result := einsum(x{i} * x{i})
		return result
	}
 	`)
	if err != nil {
		t.Fatalf("expected no error compiling simple gx code, got: %v", err)
	}
}

func TestCompileWithImport(t *testing.T) {
	err := compile(`
	package gxnb
	import "math"
 	func logitsSoftCap(x [___S]float32, cap float32) [S___]float32 {
		return cap * math.Tanh[float32](x/cap)
	}
 	`)
	// TODO: currently this fails because we don't have the math package
	// available in our loader. We need to fix that.
	if err == nil || !strings.Contains(err.Error(), "package math has not been built") {
		t.Fatalf("expected invalid input error, got %v", err)
	}
}
