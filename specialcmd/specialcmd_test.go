package specialcmd

import (
	"fmt"
	"github.com/stretchr/testify/require"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestJoinLine(t *testing.T) {
	lines := strings.Split("a\nb c\\\nd\\\ne\nf", "\n")
	updatedLines := make(map[int]bool)
	got := joinLine(lines, 1, updatedLines)
	assert.Equal(t, "b c d e", got, "Joining consecutive lines ended in '\\'")
	assert.EqualValues(t, map[int]bool{1: true, 2: true, 3: true}, updatedLines, "Joining consecutive lines ended in '\\'")
}

func TestSplitCmd(t *testing.T) {
	parts := splitCmd("--msg=\"hello world\" \t\n --msg2=\"it replied \\\"\\nhello\\t\\\"\" \"")
	fmt.Printf("Parts=%+q\n", parts)
	require.Len(t, parts, 3)
	assert.Equal(t, "--msg=hello world", parts[0])
	assert.Equal(t, "--msg2=it replied \"\nhello\t\"", parts[1])
	assert.Equal(t, "", parts[2])
}
