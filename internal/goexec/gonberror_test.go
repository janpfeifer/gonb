package goexec

import (
	"github.com/pkg/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"testing"
)

func getError(t *testing.T, rawError bool) (string, error) {
	s := newEmptyStateWithRawError(t, rawError)
	defer func() {
		err := s.Stop()
		require.NoError(t, err, "Failed to finalized state")
	}()
	fileToCellLine := createTestGoMain(t, s, sampleCellCode)
	fileToCellIdAndLine := MakeFileToCellIdAndLine(-1, fileToCellLine)
	errorMsg := "THIS_IS_ERROR"
	sampleError := errors.New(errorMsg)
	gonbError := s.DisplayErrorWithContext(nil, fileToCellIdAndLine, errorMsg, sampleError)
	return errorMsg, gonbError
}

func getGonbError(t *testing.T) (string, error) {
	return getError(t, true)
}
func getWrappedError(t *testing.T) (string, error) {
	errorMsg, gonbError := getGonbError(t)
	return errorMsg, errors.Wrapf(gonbError, "WRAPPER")
}
func TestRawError(t *testing.T) {
	var gonbError *GonbError
	_, err := getGonbError(t)
	assert.NotNil(t, err)
	assert.True(t, errors.As(err, &gonbError))
	_, err = getError(t, false)
	assert.NotNil(t, err)
	assert.False(t, errors.As(err, &gonbError))
	_, err = getWrappedError(t)
	assert.NotNil(t, err)
	assert.True(t, errors.As(err, &gonbError))

}
