package specialcmd

import (
	"fmt"
	"github.com/gofrs/uuid"
	. "github.com/janpfeifer/gonb/common"
	"github.com/janpfeifer/gonb/gonbui/protocol"
	"github.com/janpfeifer/gonb/internal/goexec"
	"github.com/janpfeifer/gonb/internal/kernel"
	"github.com/stretchr/testify/require"
	"os"
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

// newEmptyState returns an empty state with a temporary directory created.
func newEmptyState(t *testing.T) *goexec.State {
	uuidTmp, _ := uuid.NewV7()
	uuidStr := uuidTmp.String()
	uniqueID := uuidStr[len(uuidStr)-8:]
	s, err := goexec.New(nil, uniqueID, false, false)
	if err != nil {
		t.Fatalf("Failed to create goexec.State: %+v", err)
	}
	return s
}

func TestDirEnv(t *testing.T) {
	s := newEmptyState(t)

	// Check current directory for GoNB.
	pwd, err := os.Getwd()
	require.NoError(t, err)
	assert.Equal(t, pwd, os.Getenv(protocol.GONB_DIR_ENV))

	// Execute a "%cd /tmp" command and check env variable was set.
	var msg kernel.Message
	usedLines := MakeSet[int]()
	err = Parse(msg, s, true, []string{"%cd /tmp"}, usedLines)
	require.NoError(t, err)
	assert.Equal(t, "/tmp", os.Getenv(protocol.GONB_DIR_ENV))
	require.NoError(t, s.Stop())
}

func TestMagicWrite(t *testing.T) {
	s := newEmptyState(t)

	expected := `fmt.Println("1")
fmt.Println("2")
// !*cat main.go
`

	type TestCase struct {
		appendMode                 bool
		filename, src, fileContent string
	}
	srcGen := func(testCase *TestCase) {
		var appendArg string
		if testCase.appendMode {
			appendArg = " -a "
		}
		testCase.src = `%writefile ` + appendArg + testCase.filename + "\n" + expected + "%%\nfmt.Println(1)"
	}

	// build test cases
	testCases := []*TestCase{
		{false, "", "", expected},
		{true, "", "", strings.Repeat(expected, 2)},
		{false, "/tmp/TestMagicWrite.log", "", expected},
	}
	for _, testCase := range testCases {
		srcGen(testCase)
	}

	// run test cases
	fileClean := MakeSet[string](4)
	defer func() {
		for filename := range fileClean {
			defer os.Remove(filename)
		}
	}()
	for idx, testCase := range testCases {
		t.Run(fmt.Sprintf("test-case-%d", idx), func(t *testing.T) {
			filename := testCase.filename
			if filename == "" {
				filename = s.UniqueID + ".out"
			}
			fileClean.Insert(filename)

			var msg kernel.Message
			usedLines := MakeSet[int]()
			lines := strings.Split(testCase.src, "\n")
			err := Parse(msg, s, true, lines, usedLines)
			require.NoError(t, err)

			fileBytes, err := os.ReadFile(filename)
			require.NoError(t, err)
			assert.Equal(t, testCase.fileContent, string(fileBytes))
		})
	}
}
