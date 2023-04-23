package specialcmd

import (
	"fmt"
	. "github.com/janpfeifer/gonb/common"
	"github.com/stretchr/testify/require"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestJoinLine(t *testing.T) {
	lines := strings.Split("a\nb c\\\nd\\\ne\nf", "\n")
	updatedLines := MakeSet[int]()
	got := joinLine(lines, 1, updatedLines)
	assert.Equal(t, "b c d e", got, "Joining consecutive lines ended in '\\'")
	var empty = struct{}{}
	assert.EqualValues(t, map[int]struct{}{1: empty, 2: empty, 3: empty}, updatedLines, "Joining consecutive lines ended in '\\'")
}

func TestSplitCmd(t *testing.T) {
	parts := splitCmd("--msg=\"hello world\" \t\n --msg2=\"it replied \\\"\\nhello\\t\\\"\" \"")
	fmt.Printf("Parts=%+q\n", parts)
	require.Len(t, parts, 3)
	assert.Equal(t, "--msg=hello world", parts[0])
	assert.Equal(t, "--msg2=it replied \"\nhello\t\"", parts[1])
	assert.Equal(t, "", parts[2])
}
