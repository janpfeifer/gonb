// Package common holds functionality that is common to multiple other packages.
package common

import "sort"

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

// Has returns true if Set s has the given key.
func (s Set[T]) Has(key T) bool {
	_, found := s[key]
	return found
}

// Insert key into set.
func (s Set[T]) Insert(key T) {
	s[key] = struct{}{}
}

// SortedKeys enumerate keys from a string map and sort them.
// TODO: make it for any key type.
func SortedKeys[T any](m map[string]T) (keys []string) {
	keys = make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return
}
