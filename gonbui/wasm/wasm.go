//go:build js || wasm

// Package wasm defines several utilities to speed up the writing of small WASM
// widgets in GoNB (or elsewhere).
//
// The variable `IsWasm` can be used to check in runtime if a program was compiled
// for wasm -- in case this is needed. This is the only symbol exported by this
// package for non-wasm builds.
//
// **Warning**: this is still experimental, and the API may change.
package wasm

// IsWasm is true in WASM builds, and false otherwise.
var IsWasm = true
