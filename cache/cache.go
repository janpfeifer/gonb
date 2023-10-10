// Package cache is library that allows one to easily cache results
// of presumably expensive (or slow) computations.
//
// Example, you may have a line like this:
//
//	var lotsOfData = LoadOverTheInternet("https://.....")
//
// Instead of every time any cell is run in GoNB having to load the data,
// you can do the following:
//
//	var lotsOfData = cache.Cache("my_data", func() *Data { return LoadOverTheInternet("https://.....") })
//
// This will save the results of `LoadOverTheInternet` in the first call, and re-use
// it later.
//
// # A few considerations to keep in mind
//
//   - Serialization/Deserialization: it uses `encoding/gob` by default, but
//     alternatively if the type implements the cache.Serializable interface,
//     it is used instead.
//   - Where/How to save it: the default is to create a temporary subdirectory
//     (under `/tmp` by default) and store/re-read from there. But alternatively
//     a new Storage object can be created which can store info anywhere.
//   - Error handling: Storage by default panics if an error occur when loading or
//     saving cached results, but it can be ignored and fallback to regenerate the
//     value. If the function being cached returns an error, see CacheErr.
//   - Concurrency: Storage can work concurrently for different keys but has
//     no safety checks (mutex) for the same key, and concurrent access to the
//     same key leads to undefined behavior.
//
// Example where one uses a hidden subdirectory in the current subdirectory as storage.
//
//	var (
//		myCache = cache.MustNewHidden()
//		lotsOfData = cache.CacheWith(myCache, "my_data", func() *Data { return LoadOverTheInternet("https://.....") })
//	)
//
// Example: if in a GoNB notebook one wants to reset the data to force it to be re-generated, one
// can write a small cell like:
//
//	%%
//	cache.ResetKey("my_data")
//
// This will reset the value associated with the `my_data` key, so next execution it will be generated
// again.
package cache

import (
	"encoding/gob"
	"fmt"
	"github.com/pkg/errors"
	"hash/fnv"
	"io"
	"log"
	"os"
	"path"
	"strings"
)

// Storage provides the storage for the caching functionality.
//
// See New, NewInTmp and NewHidden functions to create a new Storage object, or use the
// pre-built Default.
//
// Concurrency: Storage can work concurrently for different keys but has
// no safety checks (mutex) for the same key, and concurrent access to the
// same key leads to undefined behavior.
type Storage struct {
	dir string
}

// New creates a new Storage object in the given directory. Directory is created if it doesn't yet
// exist.
func New(dir string) (*Storage, error) {
	stat, err := os.Stat(dir)
	if os.IsNotExist(err) {
		err = os.MkdirAll(dir, 0700)
	} else if err == nil && !stat.IsDir() {
		return nil, errors.Errorf("cache.Storage location %q is not a directory", dir)
	}
	if err != nil {
		return nil, errors.WithMessagef(err, "failed to create cache.Storate in directory %q", dir)
	}
	return &Storage{dir: dir}, nil
}

// AssertNoError will `log.Fatal` if err is not nil.
func AssertNoError(err error) {
	if err != nil {
		log.Fatalf("%+v", err)
	}
}

// MustNew is similar to New, but will log.Fatal if New fails to create the Storage for any reasons.
func MustNew(dir string) *Storage {
	s, err := New(dir)
	AssertNoError(err)
	return s
}

// NewInTmp creates a Storage object using a temporary directory whose name is a hash of the current
// directory -- so it will use the same if run on the same location every time.
//
// The temporary directory is created under `os.TempDir()`.
func NewInTmp() (*Storage, error) {
	// Create unique name based on hash of current directory.
	currentDir, err := os.Getwd()
	if err != nil {
		return nil, errors.WithMessage(err, "failed to create cache.Storage: unknown current directory")
	}
	hasher := fnv.New64a()
	_, err = hasher.Write([]byte(currentDir))
	if err != nil {
		return nil, errors.WithMessage(err, "failed to create cache.Storage")
	}
	hash64 := hasher.Sum64()
	uniqueName := fmt.Sprintf("gonb_cache_%X", hash64)

	// Create Storage using the unique name for a temporary directory.
	dir := path.Join(os.TempDir(), uniqueName)
	var s *Storage
	s, err = New(dir)
	if err != nil {
		return nil, errors.WithMessage(err, "failed to create cache.Storage")
	}

	// Save current directory or make sure it is the same as before.
	wdPath := path.Join(s.dir, "current_directory.txt")
	if wdRead, err := os.ReadFile(wdPath); err == nil {
		if string(wdRead) != currentDir {
			return nil, errors.Errorf("cache.Storage hash collision(!?): tried using directory %q, but it was already by another program in %q",
				s.dir, wdRead)
		}
	} else if os.IsNotExist(err) {
		err = os.WriteFile(wdPath, []byte(currentDir), 0700)
	} else {
		return nil, errors.WithMessagef(err, "failed to create cache.Storage: failed to save current directory name in %q", wdPath)
	}
	return s, nil
}

// MustNewInTmp is similar to NewInTmp, but will log.Fatal if it fails to create the Storage for any reasons.
func MustNewInTmp() *Storage {
	s, err := NewInTmp()
	AssertNoError(err)
	return s
}

// HiddenCacheSubdirectory is the then name of the subdirectory used by NewHidden.
const HiddenCacheSubdirectory = ".gonb_cache"

// NewHidden crates a Storage object with the name `.gonb_cache` (HiddenCacheSubdirectory) in the current directory.
func NewHidden() (*Storage, error) {
	currentDir, err := os.Getwd()
	if err != nil {
		return nil, errors.WithMessage(err, "failed to create cache.Storage: unknown current directory")
	}
	dir := path.Join(currentDir, HiddenCacheSubdirectory)
	return New(dir)
}

// MustNewHidden is similar to NewHidden, but will log.Fatal if it fails to create the Storage for any reasons.
func MustNewHidden() *Storage {
	s, err := NewHidden()
	AssertNoError(err)
	return s
}

// Serializable interface defines a serialization/deserialization interface. It allows custom
// serialization if `encoding.gob` won't work for caching.
type Serializable interface {
	// CacheSerialize should serialize the contents of the object to the writer. Used by the
	// `cache` package.
	CacheSerialize(writer io.Writer) error

	// CacheDeserialize should deserialize the object from the given `io.Reader`. It should
	// return either the same object or a new one of the same type with the deserialized content.
	//
	// In particular, it should work well with pointers: when one pass a nil pointer -- it should
	// return a pointer to the newly allocated content.
	CacheDeserialize(reader io.Reader) (any, error)
}

// writeSerialized serializes value into the given io.Writer
func writeSerialized(w io.Writer, value any) error {
	if serializable, ok := value.(Serializable); ok {
		return serializable.CacheSerialize(w)
	}
	enc := gob.NewEncoder(w)
	err := enc.Encode(value)
	if err != nil {
		return errors.Wrapf(err, "failed to serialize using `encoding/gob`")
	}
	return nil
}

// readAndDeserialize and returns the deserialized value, or an error. It expects value to
// be a pointer to whatever is being deserialized.
func readAndDeserialize[T any](r io.Reader) (value T, err error) {
	valueAny := any(value)
	if serializable, ok := valueAny.(Serializable); ok {
		valueAny, err = serializable.CacheDeserialize(r)
		if err != nil {
			err = errors.Wrapf(err, "failed to CacheDeserialize")
		} else {
			value = valueAny.(T)
		}
		return
	}
	dec := gob.NewDecoder(r)
	err = dec.Decode(&value)
	return
}

// cacheFilesSuffix is used in every file storing a cache value.
const cacheFilesSuffix = ".bin"

func (s *Storage) pathForKey(key string) (string, error) {
	if key == "." || key == ".." || strings.IndexAny(key, "/\n\r\t\000*") != -1 {
		return "", errors.Errorf("invalid key %q: keys must be valid file names", key)
	}
	return path.Join(s.dir, key) + cacheFilesSuffix, nil
}

// Save the value using the given key.
//
// Returns an error if anything goes wrong.
func (s *Storage) Save(key string, value any) error {
	keyPath, err := s.pathForKey(key)
	if err != nil {
		return err
	}
	f, err := os.Create(keyPath)
	if err != nil {
		return errors.Wrapf(err, "failed to create file for key %q", key)
	}
	err = writeSerialized(f, value)
	if err != nil {
		return errors.WithMessagef(err, "failed to create file for key %q", key)
	}
	err = f.Close()
	if err != nil {
		return errors.WithMessagef(err, "failed to close file for key %q", key)
	}
	return nil
}

// Reader returns a file reader from the storage for the given key.
//
// Return os.ErrNotExist if key does not exist in storage.
func (s *Storage) Reader(key string) (io.Reader, error) {
	keyPath, err := s.pathForKey(key)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(keyPath)
	if os.IsNotExist(err) {
		return nil, os.ErrNotExist
	}
	return f, nil
}

// ResetKey after which Storage doesn't know anything about the key.
func (s *Storage) ResetKey(key string) error {
	keyPath, err := s.pathForKey(key)
	if err != nil {
		return err
	}
	err = os.Remove(keyPath)
	if os.IsNotExist(err) {
		// No file for key already, all good.
		return nil
	}
	if err != nil {
		err = errors.Wrapf(err, "failed ResetForKey(%q)", key)
	}
	return err
}

// ResetKey after which the Default storage doesn't know anything about the key.
func ResetKey(key string) error {
	return Default.ResetKey(key)
}

// ListKeys returns the known keys for the storage.
func (s *Storage) ListKeys() ([]string, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		err = errors.Wrapf(err, "failed ListKeys(%q)", s.dir)
		return nil, err
	}
	if len(entries) == 0 {
		return nil, nil
	}
	keys := make([]string, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, cacheFilesSuffix) {
			continue
		}
		keys = append(keys, name[:len(name)-len(cacheFilesSuffix)])
	}
	return keys, nil
}

// ListKeys returns the known keys for the Default storage.
func ListKeys() ([]string, error) {
	return Default.ListKeys()
}

// Reset removes information about all keys for this storage.
func (s *Storage) Reset() error {
	keys, err := s.ListKeys()
	if err != nil {
		return err
	}
	for _, key := range keys {
		err = s.ResetKey(key)
		if err != nil {
			return err
		}
	}
	return nil
}

// Reset removes information about all keys for the Default storage.
func Reset() error {
	return Default.Reset()
}

// Default caching storage, created in a temporary directory with NewInTmp -- so it gets
// cleaned up whenever the system is rebooted.
var Default = MustNewInTmp()

// CacheWith first checks if a value for `key` has already been saved at previous time,
// in which case it is deserialized and returned. if not, `fn` is called, its result
// is first saved using `key` and then returned.
//
// The special case when `key` is empty ("") will not use any cache, and `fn` will always
// be called
//
// The saving and loading is implemented by the given Storage object.
func CacheWith[T any](s *Storage, key string, fn func() T) T {
	if key == "" {
		// Bypass cache.
		return fn()
	}

	r, err := s.Reader(key)
	var value T
	if err == nil {
		// Value ready to be read.
		value, err = readAndDeserialize[T](r)
		AssertNoError(err)
	} else if err == os.ErrNotExist {
		// Entry not in cache, generate it with `fn`.
		value = fn()
		AssertNoError(s.Save(key, value))
	} else {
		AssertNoError(err)
	}
	return value
}

// Cache first checks if a value for `key` has already been saved at previous time,
// in which case it is deserialized and returned. if not, `fn` is called, its result
// is first saved using `key` and then returned.
//
// The special case when `key` is empty ("") will not use any cache, and `fn` will always
// be called
//
// It uses Default for caching, it's equivalent to calling `CacheWith(Default, key, fn)`.
func Cache[T any](key string, fn func() T) T {
	return CacheWith[T](Default, key, fn)
}
