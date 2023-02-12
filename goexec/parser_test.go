package goexec

import (
	"bytes"
	"fmt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"os"
	"path"
	"testing"
)

func createTestGoMain(content string) (dirPath string, err error) {
	dirPath, err = os.MkdirTemp("", "parser_test_")
	if err != nil {
		return
	}
	var f *os.File
	f, err = os.Create(path.Join(dirPath, "main.go"))
	if err != nil {
		return
	}
	_, err = f.WriteString(content)
	if err != nil {
		return
	}
	err = f.Close()
	if err != nil {
		return
	}
	fmt.Printf("Create test data in %q\n", dirPath)
	return
}

func emptyState() *State {
	return &State{
		Decls: NewDeclarations(),
	}
}

func TestState_ParseImportsFromMainGo(t *testing.T) {
	code := `package main

import "fmt"

// Some comment

import (
  "math"
  fmtOther "fmt"
  "github.com/pkg/errors"
  . "gomlx/computation"
)

const PI = 3.1415

const (
	PI32 float32 = 3.1415
	E = 2.71828
	ToBe = "Or Not To Be"
)

var (
	x, y float32 = 1, 2
	b = math.Sqrt(30.0 +
		34.0)
)

type XY struct { x, y float64 }

var _ = fmt.Printf

type Kg int
type N float64

func (k Kg) Weight() N {
	return N(k) * 9.8
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
`
	s := emptyState()
	var err error
	s.TempDir, err = createTestGoMain(code)
	if err != nil {
		t.Fatalf("Failed to create main.go: %+v", err)
	}
	err = s.ParseImportsFromMainGo(nil, NoCursor, s.Decls)
	if err != nil {
		t.Fatalf("Failed to parse imports from main.go: %+v", err)
	}
	fmt.Printf("\ttest imports: %+v\n", s.Decls.Imports)
	assert.Lenf(t, s.Decls.Imports, 5, "Expected 5 imports, got %+v", s.Decls.Imports)
	assert.Contains(t, s.Decls.Imports, "fmt")
	assert.Contains(t, s.Decls.Imports, "math")
	assert.Contains(t, s.Decls.Imports, "fmtOther")
	assert.Contains(t, s.Decls.Imports, "errors")
	assert.Contains(t, s.Decls.Imports, ".~gomlx/computation")

	fmt.Printf("\ttest functions: %+v\n", s.Decls.Functions)
	assert.Lenf(t, s.Decls.Functions, 5, "Expected 2 functions, got %+v", s.Decls.Functions)
	assert.Contains(t, s.Decls.Functions, "f")
	assert.Contains(t, s.Decls.Functions, "sum")
	assert.Contains(t, s.Decls.Functions, "init_c")
	assert.Contains(t, s.Decls.Functions, "Kg~Weight")
	assert.Contains(t, s.Decls.Functions, "N~Weight")

	fmt.Printf("\ttest variables: %+v\n", s.Decls.Variables)
	assert.Lenf(t, s.Decls.Variables, 5, "Expected 4 variables, got %+v", s.Decls.Variables)
	assert.Contains(t, s.Decls.Variables, "x")
	assert.Contains(t, s.Decls.Variables, "y")
	assert.Contains(t, s.Decls.Variables, "b")
	assert.Contains(t, s.Decls.Variables, "c")
	// The 5th var is "_", which gets a random key.

	fmt.Printf("\ttest types: %+v\n", s.Decls.Types)
	assert.Lenf(t, s.Decls.Types, 3, "Expected 3 types, got %+v", s.Decls.Types)
	assert.Contains(t, s.Decls.Types, "XY")
	assert.Contains(t, s.Decls.Types, "Kg")
	assert.Contains(t, s.Decls.Types, "N")
	assert.Equal(t, "struct { x, y float64 }", s.Decls.Types["XY"].TypeDefinition)

	fmt.Printf("\ttest constants: %+v\n", s.Decls.Constants)
	assert.Lenf(t, s.Decls.Constants, 7, "Expected 7 Constants, got %+v", s.Decls.Constants)
	assert.Contains(t, s.Decls.Constants, "E")
	assert.Contains(t, s.Decls.Constants, "PI")
	assert.Contains(t, s.Decls.Constants, "PI32")
	assert.Contains(t, s.Decls.Constants, "ToBe")
	assert.Equal(t, "\"Or Not To Be\"", s.Decls.Constants["ToBe"].ValueDefinition)
	assert.Contains(t, s.Decls.Constants, "K0")
	assert.Contains(t, s.Decls.Constants, "K1")
	assert.Contains(t, s.Decls.Constants, "K2")
	assert.Equal(t, "K0", s.Decls.Constants["K1"].Prev.Key)
	assert.Equal(t, "K2", s.Decls.Constants["K1"].Next.Key)

	// Check imports rendering.
	wantImportsRendering := `import (
	. "gomlx/computation"
	"github.com/pkg/errors"
	"fmt"
	fmtOther "fmt"
	"math"
)
`
	buf := bytes.NewBuffer(make([]byte, 0, 512))
	_, _, err = s.Decls.RenderImports(0, buf)
	require.NoErrorf(t, err, "Declarations.RenderImports()")
	assert.Equal(t, wantImportsRendering, buf.String())

	// Checks variables rendering.
	wantVariablesRendering := `var (
	_ = fmt.Printf
	b = math.Sqrt(30.0 +
		34.0)
	c = "blah, blah, blah"
	x float32 = 1
	y float32 = 2
)
`
	buf = bytes.NewBuffer(make([]byte, 0, 512))
	_, _, err = s.Decls.RenderVariables(0, buf)
	require.NoErrorf(t, err, "Declarations.RenderVariables()")
	assert.Equal(t, wantVariablesRendering, buf.String())

	// Checks functions rendering.
	wantFunctionsRendering := `func (k Kg) Weight() N {
	return N(k) * 9.8
}
func (n N) Weight() N { return n }
func f(x int) {
	return g(x)+1  // g not defined in this file, but we still want to parse this.
}
func init() {
	c += ", blah"
}
func sum[T interface{int | float32 | float64}](a, b T) T {
	return a + b
}
`
	buf = bytes.NewBuffer(make([]byte, 0, 1024))
	_, _, err = s.Decls.RenderFunctions(0, buf)
	require.NoErrorf(t, err, "Declarations.RenderFunctions()")
	assert.Equal(t, wantFunctionsRendering, buf.String())

	// Checks types rendering.
	wantTypesRendering := `type Kg int
type N float64
type XY struct { x, y float64 }
`
	buf = bytes.NewBuffer(make([]byte, 0, 1024))
	_, _, err = s.Decls.RenderTypes(0, buf)
	require.NoErrorf(t, err, "Declarations.RenderTypes()")
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
	_, _, err = s.Decls.RenderConstants(0, buf)
	require.NoErrorf(t, err, "Declarations.RenderConstants()")
	assert.Equal(t, wantConstantsRendering, buf.String())
	//fmt.Printf("Constants:\n%s\n", buf.String())
}
