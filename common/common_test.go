package common

import (
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestFlagsParse(t *testing.T) {
	args := []string{
		"-a",
		"pos-arg1",
		"-b",
		"vb",
		"pos-arg2",
	}
	noValArg := MakeSet[string](2)
	noValArg.Insert("append")
	schema := map[string]string{
		"a": "append",
		"b": "block",
	}
	actual := FlagsParse(args, noValArg, schema)

	expected := map[string]string{
		"-pos1":  "pos-arg1",
		"-pos2":  "pos-arg2",
		"append": "",
		"block":  "vb",
	}
	assert.Equal(t, expected, actual)
}
