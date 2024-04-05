// Package common holds generic functionality that is common to multiple packages.
package common

import (
	"fmt"
	"github.com/gofrs/uuid"
	"github.com/pkg/errors"
	"golang.org/x/exp/constraints"
	"io/fs"
	"os"
	"os/user"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// Panicf panics with an error constructed with the given format and args.
func Panicf(format string, args ...any) {
	panic(errors.Errorf(format, args...))
}

var pauseChan = make(chan struct{})

// Pause the current goroutine, it waits forever on a channel.
// Used for testing.
func Pause() {
	<-pauseChan
}

// UniqueId returns newly created unique id.
func UniqueId() string {
	v7id, _ := uuid.NewV7()
	uuidStr := v7id.String()
	uid := uuidStr[len(uuidStr)-8:]
	return uid
}

// Set implements a Set for the key type T.
type Set[T comparable] map[T]struct{}

// MakeSet returns an empty Set of the given type. Size is optional, and if given
// will reserve the expected size.
func MakeSet[T comparable](size ...int) Set[T] {
	if len(size) == 0 {
		return make(Set[T])
	}
	return make(Set[T], size[0])
}

func SetWithValues[T comparable](values ...T) Set[T] {
	if len(values) == 0 {
		return MakeSet[T]()
	}
	s := MakeSet[T](len(values))
	for _, v := range values {
		s.Insert(v)
	}
	return s
}

// Has returns true if Set s has the given key.
func (s Set[T]) Has(key T) bool {
	_, found := s[key]
	return found
}

// Insert key into set.
func (s Set[T]) Insert(key T) {
	s[key] = struct{}{}
}

// Delete key into set.
func (s Set[T]) Delete(key T) {
	delete(s, key)
}

// Keys returns the keys of a map in the form of a slice.
func Keys[K comparable, V any](m map[K]V) []K {
	s := make([]K, 0, len(m))
	for k := range m {
		s = append(s, k)
	}
	return s
}

// SortedKeys returns the sorted keys of a map in the form of a slice.
func SortedKeys[K constraints.Ordered, V any](m map[K]V) []K {
	s := Keys(m)
	sort.Slice(s, func(i, j int) bool {
		return s[i] < s[j]
	})
	return s
}

// WalkDirWithSymbolicLinks is similar filepath.WalkDir, but it follows symbolic links. It also checks for
// infinite loops in symbolic links, and returns an error if it finds one.
func WalkDirWithSymbolicLinks(root string, dirFunc fs.WalkDirFunc) error {
	visited := MakeSet[string]()
	return walkDirWithSymbolicLinksImpl(root, root, dirFunc, visited)
}

func walkDirWithSymbolicLinksImpl(root, current string, dirFunc fs.WalkDirFunc, visited Set[string]) error {
	// Break infinite loops
	if visited.Has(current) {
		return errors.Errorf("directory %q is in an infinite loop of symbolic links, cannot WalkDirWithSymbolicLinks(%q)", current, root)
	}
	visited.Insert(current)

	err := filepath.WalkDir(current, func(entryPath string, info fs.DirEntry, err error) error {
		if info == nil {
			// This happens when a linked file/dir does not exist, ignoring.
			return nil
		}
		if info.Type() == os.ModeSymlink {
			// Recursively follow symbolic links.
			linkedPath, err := os.Readlink(entryPath)
			if err != nil {
				err = errors.Wrapf(err, "WalkDirWithSymbolicLinks failed to resolve symlink %q", entryPath)
				return err
			}
			err = walkDirWithSymbolicLinksImpl(root, linkedPath, dirFunc, visited)
			if err != nil {
				err = errors.WithMessagef(err, "while traversing symlink %q -> %q", entryPath, linkedPath)
				return err
			}
		}

		// If not a symbolic link, call the user's function.
		return dirFunc(entryPath, info, err)
	})
	if err != nil {
		err = errors.WithMessagef(err, "while traversing dir %q", current)
	}
	return err
}

// ReplaceTildeInDir by the user's home directory. Returns dir if it doesn't start with "~".
//
// It may panic with an error if `dir` has an unknown user (e.g: `~unknown/...`)
func ReplaceTildeInDir(dir string) string {
	if dir[0] != '~' {
		return dir
	}
	var userName string
	if dir != "~" && !strings.HasPrefix(dir, "~/") {
		sepIdx := strings.IndexRune(dir, '/')
		if sepIdx == -1 {
			userName = dir[1:]
		} else {
			userName = dir[1:sepIdx]
		}
	}
	var usr *user.User
	var err error
	if userName == "" {
		usr, err = user.Current()
	} else {
		usr, err = user.Lookup(userName)
	}
	if err != nil {
		panic(errors.Wrapf(err, "failed to lookup home directory for user in path %q", dir))
	}
	homeDir := usr.HomeDir
	return path.Join(homeDir, dir[1+len(userName):])
}

// Latch implements a "latch" synchronization mechanism.
//
// A Latch is a signal that can be waited for until it is triggered.
// Once triggered it never changes state, it's forever triggered.
type Latch struct {
	muTrigger sync.Mutex
	wait      chan struct{}
}

// NewLatch returns an un-triggered latch.
func NewLatch() *Latch {
	return &Latch{
		wait: make(chan struct{}),
	}
}

// Trigger latch.
func (l *Latch) Trigger() {
	l.muTrigger.Lock()
	defer l.muTrigger.Unlock()

	if l.Test() {
		// Already triggered, discard value.
		return
	}
	close(l.wait)
}

// Wait waits for the latch to be triggered.
func (l *Latch) Wait() {
	<-l.wait
}

// Test checks whether the latch has been triggered.
func (l *Latch) Test() bool {
	select {
	case <-l.wait:
		return true
	default:
		return false
	}
}

// WaitChan returns the channel that one can use on a `select` to check when
// the latch triggers.
// The returned channel is closed when the latch is triggered.
func (l *Latch) WaitChan() <-chan struct{} {
	return l.wait
}

// LatchWithValue implements a "latch" synchronization mechanism, with a value associated with the
// triggering of the latch.
//
// A LatchWithValue is a signal that can be waited for until it is triggered. Once triggered it never
// changes state, it's forever triggered.
type LatchWithValue[T any] struct {
	value T
	latch *Latch
}

// NewLatchWithValue returns an un-triggered latch.
func NewLatchWithValue[T any]() *LatchWithValue[T] {
	return &LatchWithValue[T]{
		latch: NewLatch(),
	}
}

// Trigger latch and saves the associated value.
func (l *LatchWithValue[T]) Trigger(value T) {
	l.latch.muTrigger.Lock()
	defer l.latch.muTrigger.Unlock()

	if l.latch.Test() {
		// Already triggered, discard value.
		return
	}
	l.value = value
	close(l.latch.wait)
}

// Wait waits for the latch to be triggered.
func (l *LatchWithValue[T]) Wait() T {
	l.latch.Wait()
	return l.value
}

// Test checks whether the latch has been triggered.
func (l *LatchWithValue[T]) Test() bool {
	return l.latch.Test()
}

// TrySend tries to send value through the channel.
// It returns false if it failed, presumably because the channel is closed.
func TrySend[T any](c chan T, value T) (ok bool) {
	defer func() {
		exception := recover()
		ok = exception == nil
	}()
	c <- value
	return
}

// SendNoBlock tries to send value through the channel.
// It returns 0 if the value was sent, 1 if sending it would block (channel buffer full)
// or 2 if the channel `c` was closed.
func SendNoBlock[T any](c chan T, value T) (status int) {
	defer func() {
		exception := recover()
		if exception != nil {
			status = 2
		}
	}()
	select {
	case c <- value:
		status = 0
	default:
		status = 1
	}
	return
}

// ArrayFlag implements a flag type that append repeated settings into an array (slice).
// TODO: make it generic and accept `float64` and `int`.
type ArrayFlag []string

// String representation.
func (f *ArrayFlag) String() string {
	if f == nil || len(*f) == 0 {
		return "(empty)"
	}
	return fmt.Sprintf("%v", []string(*f))
}

// Set new value, by appending to the end of the string.
func (f *ArrayFlag) Set(value string) error {
	*f = append(*f, value)
	return nil
}

// FlagsParse parse args to map
func FlagsParse(args []string, noValArg Set[string], schema map[string]string) map[string]string {
	keyPos := 0 // position arg
	keyGen := func() string {
		keyPos++
		return fmt.Sprintf("-pos%d", keyPos)
	}
	resultMap := make(map[string]string)
	var key string
	for _, arg := range args {
		switch {
		case len(arg) > 2 && arg[:2] == "--":
			key = arg[2:]
			resultMap[key] = ""
		case len(arg) > 1 && arg[0] == '-':
			d, ok := schema[arg[1:]]
			if ok && len(d) > 0 {
				key = d
			} else {
				key = arg[1:]
			}
			resultMap[key] = ""
		case len(arg) > 0 && arg[0] != '-':
			if noValArg.Has(key) || key == "" {
				key = keyGen()
			}
			resultMap[key] = arg
			key = ""
		}
	}
	return resultMap
}
