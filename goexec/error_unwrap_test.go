package goexec

import (
	"github.com/pkg/errors"
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestUnwrap(t *testing.T) {
	s := newEmptyState(t, true)
	fileToCellLine := createTestGoMain(t, s, sampleCellCode)
	fileToCellIdAndLine := MakeFileToCellIdAndLine(-1, fileToCellLine)
	rawError := "THIS_IS_ERROR"
	sampleError := errors.New(rawError)
	errorMsg := sampleError.Error()
	gonbError := s.DisplayErrorWithContext(nil, fileToCellIdAndLine, errorMsg, sampleError)
	assert.NotNil(t, gonbError)
	name, msg, traceback := Unwrap(gonbError)
	assert.NotEmpty(t, name)
	assert.NotEmpty(t, msg)
	assert.NotEmpty(t, traceback)
}
