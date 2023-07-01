// Package common holds functionality that is common to multiple other packages.
package common

import (
	"github.com/pkg/errors"
	"golang.org/x/exp/constraints"
	"io/fs"
	"os"
	"os/user"
	"path"
	"path/filepath"
	"sort"
	"strings"
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

	return filepath.WalkDir(current, func(entryPath string, info fs.DirEntry, err error) error {
		if info.Type() == os.ModeSymlink {
			// Recursively follow symbolic links.
			linkedPath, err := os.Readlink(entryPath)
			if err != nil {
				err = errors.Wrapf(err, "WalkDirWithSymbolicLinks failed to resolve symlink %q", entryPath)
				return err
			}
			return walkDirWithSymbolicLinksImpl(root, linkedPath, dirFunc, visited)
		}

		// If not a symbolic link, call the user's function.
		return dirFunc(entryPath, info, err)
	})
}

// ReplaceTildeInDir by the user's home directory. Returns dir if it doesn't start with "~".
func ReplaceTildeInDir(dir string) string {
	if dir != "~" && !strings.HasPrefix(dir, "~/") {
		return dir
	}
	usr, _ := user.Current()
	homeDir := usr.HomeDir
	return path.Join(homeDir, dir[1:])
}
