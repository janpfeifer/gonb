package goexec

import (
	"github.com/janpfeifer/gonb/gonbui/protocol"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"os"
	"testing"
)

func TestDirEnv(t *testing.T) {
	// Create an empty state.
	s := newEmptyState(t)
	defer func() {
		err := s.Stop()
		require.NoError(t, err, "Failed to finalized state")
	}()
	assert.Equal(t, s.TempDir, os.Getenv(protocol.GONB_TMP_DIR_ENV))

	pwd, err := os.Getwd()
	require.NoError(t, err)
	assert.Equal(t, pwd, os.Getenv(protocol.GONB_DIR_ENV))
}
