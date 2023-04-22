package goexec

import (
	"fmt"
	. "github.com/janpfeifer/gonb/common"
	"github.com/stretchr/testify/require"
	"os"
	"strings"
	"testing"
)

// The tests here uses the sample code and utility functions defined in `parser_test.go`.

var sampleCellContentSuffix = `
!echo nonono

%%
fmt.Printf("Hello! %s\n", c)
fmt.Printf("1 + 3 = %d\n", sum(1, 3))
fmt.Printf("math.Pi - PI=%f\n", math.Pi - float64(PI32))
`

func TestCreateGoFileFromLines(t *testing.T) {
	// Test cursor positioning in generated lines.
	s := newEmptyState(t)
	//defer func() {
	//	err := s.Finalize()
	//	require.NoError(t, err, "Failed to finalized state")
	//}()
	fmt.Println(s.MainPath())

	content := sampleCellCode + sampleCellContentSuffix
	lines := strings.Split(content, "\n")
	skipLines := MakeSet[int]()
	for ii, line := range lines {
		if line == "!echo nonono" {
			skipLines.Insert(ii)
		}
	}

	cursorInCell := Cursor{38, 27} // "func (k *Kg) Gain(lasagna K_g) {"
	cursorLine := lines[cursorInCell.Line]
	cursorInFile, fileToCellMap, err := s.createGoFileFromLines(s.MainPath(), lines, skipLines, cursorInCell)
	require.NoErrorf(t, err, "Failed createGoFileFromLines(%q)", s.MainPath())

	// Read generated contents:
	contentBytes, err := os.ReadFile(s.MainPath())
	require.NoErrorf(t, err, "Failed os.ReadFile(%q)", s.MainPath())
	content = string(contentBytes)
	require.Contains(t, content, "func main() {")
	require.NotContains(t, content, "echo nonono", "Line should have been filtered out, since it is in skipLine.")

	originalNumLines := len(lines)
	newLines := strings.Split(content, "\n")
	newNumLines := len(newLines)
	require.Equal(t, originalNumLines+5, newNumLines, "Number of lines of generated main.go")
	require.Equal(t, cursorLine, newLines[cursorInFile.Line], "Cursor line remains the same.")

	for ii, newLine := range newLines {
		if ii >= newNumLines-8 {
			// Content of lines change (an indentation is added) so we skip these.
			break
		}
		originalLineIdx := fileToCellMap[ii]
		if originalLineIdx == NoCursorLine {
			continue
		}
		require.Equalf(t, lines[originalLineIdx], newLine, "Line mapping look wrong: file line %d --> cell line %d", ii, originalLineIdx)
	}
}
