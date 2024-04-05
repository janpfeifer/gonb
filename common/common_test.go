package common

import (
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"os"
	"testing"
)

func TestReplaceEnvVars(t *testing.T) {
	require.NoError(t, os.Setenv("X", "foo"))
	require.NoError(t, os.Setenv("Y", "bar"))
	require.NoError(t, os.Setenv("SOME_VAR", "blah"))

	str := "a/${X}${Y}/$MISSING/$SOME_VAR"
	want := "a/foobar//blah"
	assert.Equal(t, want, ReplaceEnvVars(str))

	str = "${X}"
	want = "foo"
	assert.Equal(t, want, ReplaceEnvVars(str))
}
