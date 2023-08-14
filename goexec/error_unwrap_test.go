package goexec

import (
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestUnwrap(t *testing.T) {
	errorMsg, gonbError := getGonbError(t)
	assert.NotNil(t, gonbError)
	name, msg, traceback := Unwrap(gonbError)
	assert.Equal(t, name, "ERROR")
	assert.Equal(t, msg, errorMsg)
	assert.NotEmpty(t, traceback, []string{errorMsg})
}
