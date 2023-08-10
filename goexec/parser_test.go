package goexec

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/gofrs/uuid"
	. "github.com/janpfeifer/gonb/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newEmptyState returns an empty state with a temporary directory created.
func newEmptyState(t *testing.T) *State {
	return newEmptyStateWithRawError(t, false)
}

func newEmptyStateWithRawError(t *testing.T, rawError bool) *State {
	uuidTmp, _ := uuid.NewV7()
	uuidStr := uuidTmp.String()
	uniqueID := uuidStr[len(uuidStr)-8:]
	s, err := New(uniqueID, rawError)
	if err != nil {
		t.Fatalf("Failed to create goexec.State: %+v", err)
	}
	return s
}

// createTestGoMain prefixes the cell content with `package main` and writes it to `main.go`.
func createTestGoMain(t *testing.T, s *State, cellContent string) (fileToCellLine []int) {
	content := sampleCellCode
	lines := strings.Split(content, "\n")
	skipLines := MakeSet[int]()
	for ii, line := range lines {
		if line == "!echo nonono" {
			skipLines.Insert(ii)
		}
	}

	var err error
	_, fileToCellLine, err = s.createGoFileFromLines(s.MainPath(), lines, skipLines, NoCursor)
	require.NoErrorf(t, err, "Failed createGoFileFromLines(%q)", s.MainPath())
	return
}

var (
	sampleCellCode = `import "fmt"

// Some comment

import (
  "math"
  fmtOther    "fmt"
  "github.com/pkg/errors"
  . "gomlx/computation"
)

const PI = 3.1415

const (
	PI32 float32 = 3.1415
	E            = 2.71828
	ToBe         = "Or Not To Be"
)

var (
	x, y float32 = 1, 2
	b            = math.Sqrt(30.0 +
		34.0)
)

var z float64

type XY struct { x, y float64 }

var _ = fmt.Printf

type Kg int
type N float64

func (k *Kg) Weight() N {
	return N(k) * 9.8
}

func (k *Kg) Gain(lasagna Kg) {
	*k += lasagna
}

func (n N) Weight() N { return n }

const (
	K0 Kg = 1 << iota
	K1
	K2
)

// f calls g and adds 1.
func f(x int) {
	return g(x)+1  // g not defined in this file, but we still want to parse this.
}

var c = "blah, blah, blah"

func sum[T interface{int | float32 | float64}](a, b T) T {
	return a + b
}

func init_c() {
	c += ", blah"
}

type Model[T any] struct {
	Data T
}

!echo nonono

%%
fmt.Printf("Hello! %s\n", c)
fmt.Printf("1 + 3 = %d\n", sum(1, 3))
fmt.Printf("math.Pi - PI=%f\n", math.Pi - float64(PI32))
`
)

func TestState_Parse(t *testing.T) {
	s := newEmptyState(t)
	fileToCellLine := createTestGoMain(t, s, sampleCellCode)
	fmt.Printf("Code:\t%s\n", s.MainPath())
	fileToCellIdAndLine := MakeFileToCellIdAndLine(-1, fileToCellLine)

	var err error
	cellId := NoCursorLine // Transient cellId.
	s.Definitions, err = s.parseFromMainGo(nil, cellId, NoCursor, fileToCellIdAndLine)
	require.NoErrorf(t, err, "Failed to parse %q", s.MainPath())

	fmt.Printf("\ttest imports: %+v\n", s.Definitions.Imports)
	assert.Lenf(t, s.Definitions.Imports, 5, "Expected 5 imports, got %+v", s.Definitions.Imports)
	assert.Contains(t, s.Definitions.Imports, "fmt")
	assert.Contains(t, s.Definitions.Imports, "math")
	assert.Contains(t, s.Definitions.Imports, "fmtOther")
	assert.Contains(t, s.Definitions.Imports, "errors")
	assert.Contains(t, s.Definitions.Imports, ".~gomlx/computation")
	assert.ElementsMatch(t, []int{7}, s.Definitions.Imports["errors"].CellLines.Lines,
		"Index to line numbers in original cell don't match.")

	fmt.Printf("\ttest functions: %+v\n", s.Definitions.Functions)
	// Notice `func main` will be automatically included.
	assert.Lenf(t, s.Definitions.Functions, 7, "Expected 6 functions, got %d", len(s.Definitions.Functions))
	assert.Contains(t, s.Definitions.Functions, "f")
	assert.Contains(t, s.Definitions.Functions, "sum")
	assert.Contains(t, s.Definitions.Functions, "init_c")
	assert.Contains(t, s.Definitions.Functions, "Kg~Weight")
	assert.Contains(t, s.Definitions.Functions, "Kg~Gain")
	assert.Contains(t, s.Definitions.Functions, "N~Weight")
	assert.Contains(t, s.Definitions.Functions, "main")
	assert.ElementsMatch(t, []int{71, 71, 72, 73, 74, 75, -1, -1}, s.Definitions.Functions["main"].CellLines.Lines,
		"Index to line numbers in original cell don't match.")

	fmt.Printf("\ttest variables: %+v\n", s.Definitions.Variables)
	assert.Lenf(t, s.Definitions.Variables, 6, "Expected 4 variables, got %+v", s.Definitions.Variables)
	assert.Contains(t, s.Definitions.Variables, "x")
	assert.Contains(t, s.Definitions.Variables, "y")
	assert.Contains(t, s.Definitions.Variables, "z")
	assert.Contains(t, s.Definitions.Variables, "b")
	assert.Contains(t, s.Definitions.Variables, "c")
	// The 5th var is "_", which gets a random key.
	assert.ElementsMatch(t, []int{21, 22}, s.Definitions.Variables["b"].CellLines.Lines,
		"Index to line numbers in original cell don't match.")

	fmt.Printf("\ttest types: %+v\n", s.Definitions.Types)
	assert.Lenf(t, s.Definitions.Types, 4, "Expected 4 types, got %+v", s.Definitions.Types)
	assert.Contains(t, s.Definitions.Types, "XY")
	assert.Contains(t, s.Definitions.Types, "Kg")
	assert.Contains(t, s.Definitions.Types, "N")
	assert.Contains(t, s.Definitions.Types, "Model")
	assert.Equal(t, "XY struct { x, y float64 }", s.Definitions.Types["XY"].TypeDefinition)
	assert.Equal(t, "Model[T any] struct {\n\tData T\n}", s.Definitions.Types["Model"].TypeDefinition)
	assert.ElementsMatch(t, []int{27}, s.Definitions.Types["XY"].CellLines.Lines,
		"Index to line numbers in original cell don't match.")

	fmt.Printf("\ttest constants: %+v\n", s.Definitions.Constants)
	assert.Lenf(t, s.Definitions.Constants, 7, "Expected 7 Constants, got %+v", s.Definitions.Constants)
	assert.Contains(t, s.Definitions.Constants, "E")
	assert.Contains(t, s.Definitions.Constants, "PI")
	assert.Contains(t, s.Definitions.Constants, "PI32")
	assert.Contains(t, s.Definitions.Constants, "ToBe")
	assert.Equal(t, "\"Or Not To Be\"", s.Definitions.Constants["ToBe"].ValueDefinition)
	assert.Contains(t, s.Definitions.Constants, "K0")
	assert.Contains(t, s.Definitions.Constants, "K1")
	assert.Contains(t, s.Definitions.Constants, "K2")
	assert.Equal(t, "K0", s.Definitions.Constants["K1"].Prev.Key)
	assert.Equal(t, "K2", s.Definitions.Constants["K1"].Next.Key)
	assert.ElementsMatch(t, []int{45}, s.Definitions.Constants["K0"].CellLines.Lines,
		"Index to line numbers in original cell don't match.")

	// Check imports rendering.
	wantImportsRendering := `import (
	. "gomlx/computation"
	"github.com/pkg/errors"
	"fmt"
	fmtOther "fmt"
	"math"
)

`
	buf := bytes.NewBuffer(make([]byte, 0, 1024))
	w := &WriterWithCursor{w: buf}
	cursor, fileToCellIdAndLine := s.Definitions.RenderImports(w, nil)
	assert.False(t, cursor.HasCursor())
	require.NoErrorf(t, w.Error(), "Declarations.RenderImports()")
	assert.Equal(t, wantImportsRendering, buf.String())
	require.ElementsMatch(t, []CellIdAndLine{
		{cellId, NoCursorLine},
		{cellId, 8},
		{cellId, 7},
		{cellId, 0},
		{cellId, 6},
		{cellId, 5},
	}, fileToCellIdAndLine, "Line numbers in cell code don't match")

	// Checks variables rendering.
	wantVariablesRendering := `var (
	_ = fmt.Printf
	b = math.Sqrt(30.0 +
		34.0)
	c = "blah, blah, blah"
	x float32 = 1
	y float32 = 2
	z float64
)

`
	buf = bytes.NewBuffer(make([]byte, 0, 1024))
	w = &WriterWithCursor{w: buf}
	cursor, fileToCellIdAndLine = s.Definitions.RenderVariables(w, nil)
	assert.False(t, cursor.HasCursor())
	require.NoErrorf(t, w.Error(), "Declarations.RenderVariables()")
	assert.Equal(t, wantVariablesRendering, buf.String())
	require.ElementsMatch(t, []CellIdAndLine{
		{cellId, NoCursorLine},
		{cellId, 29},
		{cellId, 21},
		{cellId, 22},
		{cellId, 55},
		{cellId, 20},
		{cellId, 20},
		{cellId, 25},
	}, fileToCellIdAndLine, "Line numbers in cell code don't match")

	// Checks functions rendering.
	wantFunctionsRendering := `func (k *Kg) Gain(lasagna Kg) {
	*k += lasagna
}

func (k *Kg) Weight() N {
	return N(k) * 9.8
}

func (n N) Weight() N { return n }

func f(x int) {
	return g(x)+1  // g not defined in this file, but we still want to parse this.
}

func init() {
	c += ", blah"
}

func main() {
	flag.Parse()
	fmt.Printf("Hello! %s\n", c)
	fmt.Printf("1 + 3 = %d\n", sum(1, 3))
	fmt.Printf("math.Pi - PI=%f\n", math.Pi - float64(PI32))


}

func sum[T interface{int | float32 | float64}](a, b T) T {
	return a + b
}

`
	buf = bytes.NewBuffer(make([]byte, 0, 1024))
	w = &WriterWithCursor{w: buf}
	cursor, _ = s.Definitions.RenderFunctions(w, nil)
	assert.False(t, cursor.HasCursor())
	require.NoErrorf(t, w.Error(), "Declarations.RenderFunctions()")
	assert.Equal(t, wantFunctionsRendering, buf.String())

	// Checks types rendering.
	wantTypesRendering := `type Kg int
type Model[T any] struct {
	Data T
}
type N float64
type XY struct { x, y float64 }

`
	buf = bytes.NewBuffer(make([]byte, 0, 1024))
	w = &WriterWithCursor{w: buf}
	cursor, _ = s.Definitions.RenderTypes(w, nil)
	assert.False(t, cursor.HasCursor())
	require.NoErrorf(t, w.Error(), "Declarations.RenderTypes()")
	assert.Equal(t, wantTypesRendering, buf.String())

	// Checks constants rendering.
	wantConstantsRendering := `const (
	K0 Kg = 1 << iota
	K1
	K2
)

const PI = 3.1415

const (
	PI32 float32 = 3.1415
	E = 2.71828
	ToBe = "Or Not To Be"
)

`
	buf = bytes.NewBuffer(make([]byte, 0, 1024))
	w = &WriterWithCursor{w: buf}
	cursor, _ = s.Definitions.RenderConstants(w, nil)
	assert.False(t, cursor.HasCursor())
	require.NoErrorf(t, err, "Declarations.RenderConstants()")
	assert.Equal(t, wantConstantsRendering, buf.String())
	//fmt.Printf("Constants:\n%s\n", buf.String())
}

func TestCursorPositioning(t *testing.T) {
	// Test cursor positioning in generated lines.
	s := newEmptyState(t, false)
	defer func() {
		err := s.Finalize()
		if err != nil {
			t.Fatalf("Failed to finalized state: %+v", err)
		}
	}()
	fileToCellLine := createTestGoMain(t, s, sampleCellCode)
	fileToCellIdAndLine := MakeFileToCellIdAndLine(-1, fileToCellLine)
	var err error
	testLines := []struct {
		cursor Cursor
		Line   string
	}{
		// Imports lines.
		{Cursor{Line: 2, Col: 7}, `	‸"fmt"`},
		{Cursor{Line: 8, Col: 3}, `	f‸mtOther "fmt"`},
		{Cursor{Line: 8, Col: 16}, `	fmtOther "f‸mt"`},

		// Variables lines:
		{Cursor{Line: 18, Col: 3}, `	To‸Be = "Or Not To Be"`},
		{Cursor{Line: 24, Col: 3}, `		3‸4.0)`},

		// Constants lines:
		{Cursor{Line: 47, Col: 15}, `	K0 Kg = 1 << i‸ota`},
		{Cursor{Line: 47, Col: 4}, `	K0 ‸Kg = 1 << iota`},
		{Cursor{Line: 48, Col: 1}, `	‸K1`},

		// Types lines:
		{Cursor{Line: 29, Col: 6}, `type X‸Y struct { x, y float64 }`},
		{Cursor{Line: 29, Col: 23}, `type XY struct { x, y f‸loat64 }`},

		// Functions lines:
		{Cursor{Line: 59, Col: 12}, `func sum[T i‸nterface{int | float32 | float64}](a, b T) T {`},
	}
	for _, testLine := range testLines {
		buf := bytes.NewBuffer(make([]byte, 0, 16384))
		s.Definitions, err = s.parseFromMainGo(nil, -1, testLine.cursor, fileToCellIdAndLine)
		if err != nil {
			t.Fatalf("Failed to parse imports from main.go: %+v", err)
		}

		cursorInFile, fileToCellIdAndLine, err := s.createGoContentsFromDecls(buf, s.Definitions, nil)
		_ = fileToCellIdAndLine
		require.NoError(t, err)
		content := buf.String()
		l := lineWithCursor(content, cursorInFile)
		assert.Equalf(t, testLine.Line, l, "Missed cursor, got:\n\tIn cell: %v\n\tIn file: %v\n\tLine got: [%s]\n\tLine wanted: [%s]\n",
			testLine.cursor, cursorInFile, l, testLine.Line)
	}
}

func TestAdjustCursorForFunctionIdentifier(t *testing.T) {
	cursor := adjustCursorForFunctionIdentifier([]string{"f(x,)"}, nil, Cursor{0, 3})
	assert.True(t, cursor.HasCursor())
	assert.Equal(t, 0, cursor.Col) // "‸f(x,)"
	cursor = adjustCursorForFunctionIdentifier([]string{"f(x,)"}, nil, Cursor{0, 4})
	assert.True(t, cursor.HasCursor())
	assert.Equal(t, 0, cursor.Col) // "‸f(x,)"

	cursor = adjustCursorForFunctionIdentifier([]string{"f(x)", "", "", ""}, nil, Cursor{2, 0})
	assert.True(t, cursor.HasCursor())
	assert.Equal(t, 0, cursor.Line) // "‸f(x,)"
	assert.Equal(t, 0, cursor.Col)  // "‸f(x,)"
}
