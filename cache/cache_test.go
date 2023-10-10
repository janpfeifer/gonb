package cache

import (
	"encoding/gob"
	"fmt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"io"
	"os"
	"sort"
	"strconv"
	"testing"
)

type Simple struct {
	X float64
}

type SimpleSerializable struct {
	X int64
}

func (s *SimpleSerializable) CacheSerialize(writer io.Writer) error {
	enc := gob.NewEncoder(writer)
	return enc.Encode(s)
}

func (s *SimpleSerializable) CacheDeserialize(reader io.Reader) (any, error) {
	// s is nil, it's used only as a type parameter.
	s2 := &SimpleSerializable{}
	dec := gob.NewDecoder(reader)
	err := dec.Decode(s2)
	return s2, err
}

func testCacheForStorage(t *testing.T, s *Storage) {
	fmt.Printf("Storage at %s:\n", s.dir)

	// Empty cache, if there was any left overs from a previous run.
	require.NoError(t, s.Reset())
	keys, err := s.ListKeys()
	require.NoError(t, err)
	require.Empty(t, keys)

	callCount := 0 // Number of calls made to our test functions.

	// Scalar values.
	gen1 := func() int {
		callCount++
		return callCount
	}
	gotForA := CacheWith(s, "a", gen1)
	assert.Equal(t, 1, gotForA)
	gotForB := CacheWith(s, "b", gen1)
	assert.Equal(t, 2, gotForB)
	gotForA = CacheWith(s, "a", gen1)
	assert.Equal(t, 1, gotForA)
	assert.Equal(t, 2, callCount)

	// Slices:
	gen3 := func() []float32 {
		callCount += 3
		return []float32{float32(callCount - 2), float32(callCount - 1), float32(callCount)}
	}
	gotForC := CacheWith(s, "c", gen3)
	assert.Equal(t, 5, callCount)
	assert.Equal(t, []float32{3, 4, 5}, gotForC)
	gotForC = CacheWith(s, "c", gen3)
	assert.Equal(t, 5, callCount)
	assert.Equal(t, []float32{3, 4, 5}, gotForC)

	// struct:
	genStruct := func() Simple {
		callCount++
		return Simple{X: float64(callCount)}
	}
	gotForD := CacheWith(s, "d", genStruct)
	assert.Equal(t, 6, callCount)
	assert.Equal(t, 6.0, gotForD.X)
	gotForD = CacheWith(s, "d", genStruct)
	assert.Equal(t, 6, callCount)
	assert.Equal(t, 6.0, gotForD.X)

	// *struct:
	genNewStruct := func() *Simple {
		callCount++
		return &Simple{X: float64(callCount)}
	}
	gotForE := CacheWith(s, "e", genNewStruct)
	require.Equal(t, 7, callCount)
	require.Equal(t, 7.0, gotForE.X)
	gotForE = CacheWith(s, "e", genNewStruct)
	require.Equal(t, 7, callCount)
	require.Equal(t, 7.0, gotForE.X)

	// maps:
	genMap := func() map[string]bool {
		callCount++
		return map[string]bool{strconv.Itoa(callCount): true}
	}
	gotForF := CacheWith(s, "f", genMap)
	assert.Equal(t, 8, callCount)
	assert.Contains(t, gotForF, "8")
	gotForF = CacheWith(s, "f", genMap)
	assert.Equal(t, 8, callCount)
	assert.Contains(t, gotForF, "8")

	// Serializable:
	genSerializable := func() *SimpleSerializable {
		callCount++
		return &SimpleSerializable{X: int64(callCount)}
	}
	gotForG := CacheWith(s, "g", genSerializable)
	assert.Equal(t, 9, callCount)
	assert.Equal(t, int64(9), gotForG.X)
	gotForG = CacheWith(s, "g", genSerializable)
	assert.Equal(t, 9, callCount)
	assert.Equal(t, int64(9), gotForG.X)

	// Test that cache is by-passed when key == "".
	gotForEmpty := CacheWith(s, "", gen1)
	assert.Equal(t, 10, gotForEmpty)
	assert.Equal(t, 10, callCount)
	gotForEmpty = CacheWith(s, "", gen1)
	assert.Equal(t, 11, gotForEmpty)
	assert.Equal(t, 11, callCount)

	// Check ListKeys.
	keys, err = s.ListKeys()
	require.NoError(t, err)
	sort.Strings(keys)
	assert.ElementsMatch(t, []string{"a", "b", "c", "d", "e", "f", "g"}, keys)

	// ResetKey:
	require.NoError(t, s.ResetKey("f"))
	keys, err = s.ListKeys()
	require.NoError(t, err)
	sort.Strings(keys)
	assert.ElementsMatch(t, []string{"a", "b", "c", "d", "e", "g"}, keys)

	// Reset:
	require.NoError(t, s.Reset())
	keys, err = s.ListKeys()
	require.NoError(t, err)
	assert.Empty(t, keys)
}

func TestCache(t *testing.T) {
	testCacheForStorage(t, Default) // Default uses NewInTmp.
	testCacheForStorage(t, MustNewHidden())

	dir, err := os.MkdirTemp("", "cache_test")
	require.NoError(t, err, "Failed to create temporary directory")
	require.NoError(t, os.Remove(dir)) // Remove it, and let the library recreate it.

	testCacheForStorage(t, MustNew(dir))
}
