// Package common holds functionality that is common to multiple other packages.
package common

import (
	"golang.org/x/exp/constraints"
	"sort"
)

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
