package goexec

import (
	"fmt"
	"github.com/janpfeifer/gonb/kernel"
	"github.com/pkg/errors"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"log"
	"math/rand"
	"os"
	"path"
	"sort"
	"strconv"
	"strings"
)

// This file implements functions related to the parsing of the Go sampleCellCode.
// It is used to properly merge sampleCellCode coming from the execution of different cells.

// WriterWithCursor keep tabs of current line/col of the file (presumably)
// being written.
type WriterWithCursor struct {
	w         io.Writer
	err       error // If err != nil, nothing is written anymore.
	Line, Col int
}

// NewWriterWithCursor that keeps tabs of current line/col of the file (presumably)
// being written.
func NewWriterWithCursor(w io.Writer) *WriterWithCursor {
	return &WriterWithCursor{w: w}
}

// Error returns first error that happened during writing.
func (w *WriterWithCursor) Error() error { return w.err }

// Format write with formatted text. Errors can be retrieved with Error.
func (w *WriterWithCursor) Format(format string, args ...any) {
	if w.err != nil {
		return
	}
	text := fmt.Sprintf(format, args...)
	w.Str(text)
}

// Str writes the given content and keeps track of cursor. Errors can be retrieved with Error.
func (w *WriterWithCursor) Str(content string) {
	if w.err != nil {
		return
	}
	var n int
	n, w.err = w.w.Write([]byte(content))
	if n != len(content) {
		w.err = errors.Errorf("failed to write %q, %d bytes: wrote only %d", content, len(content), n)
	}
	if w.err != nil {
		return
	}

	// Update cursor position.
	parts := strings.Split(content, "\n")
	if len(parts) == 1 {
		w.Col += len(parts[0])
	} else {
		w.Line += len(parts) - 1
		w.Col = len(parts[len(parts)-1])
	}
}

// CursorInFile returns a cursor pointing in file, given the current position in the file
// (stored in w) and the position of the relativeCursor in the definition to come.
func (w *WriterWithCursor) CursorInFile(relativeCursor Cursor) (fileCursor Cursor) {
	fileCursor.Line = w.Line + relativeCursor.Line
	if relativeCursor.Line > 0 {
		fileCursor.Col = relativeCursor.Col
	} else {
		fileCursor.Col = w.Col + relativeCursor.Col
	}
	return
}

// parseInfo holds the information needed for parsing and some key helper methods.
type parseInfo struct {
	cursor        Cursor
	fileSet       *token.FileSet
	filesContents map[string]string
}

// getCursor returns the cursor position within this declaration, if the original cursor falls in there.
func (pi *parseInfo) getCursor(node ast.Node) Cursor {
	from, to := node.Pos(), node.End()
	if !pi.cursor.HasCursor() {
		return NoCursor
	}
	fromPos, toPos := pi.fileSet.Position(from), pi.fileSet.Position(to)
	for line := fromPos.Line; line <= toPos.Line; line++ {
		// Notice that parser lines are 1-based, we keep them 0-based in the cursor.
		if line-1 == pi.cursor.Line {
			// Column is set either relative to the start of the definition, if in the
			// same start line, or the definition column.
			col := pi.cursor.Col
			if line == fromPos.Line && col < fromPos.Column-1 {
				// Cursor before node.
				//log.Printf("Cursor before node: %q", pi.extractContentOfNode(node))
				return NoCursor
			}
			if line == toPos.Line && col >= toPos.Column-1 {
				// Cursor after node.
				//log.Printf("Cursor after node: %q", pi.extractContentOfNode(node))
				return NoCursor
			}
			if line == fromPos.Line {
				col = col - (fromPos.Column - 1) // fromPos column is 1-based.
			}
			c := Cursor{line - fromPos.Line, col}
			//log.Printf("Found cursor at %v in definition, (%d, %d):\n%s", c, fromPos.Line, fromPos.Column,
			//	pi.extractContentOfNode(node))
			return c
		}
	}
	return NoCursor
}

// extractContentOfNode from files, given the golang parser's tokens.
//
// Currently, we generate all the content in file `main.go`, so fileContents will only have
// one entry.
//
// This is used to get the exact definition (string) of an element (function, variable, const, import, type, etc.)
func (pi *parseInfo) extractContentOfNode(node ast.Node) string {
	f := pi.fileSet.File(node.Pos())
	from, to := f.Offset(node.Pos()), f.Offset(node.End())
	contents, found := pi.filesContents[f.Name()]
	if !found {
		return fmt.Sprintf("Didn't find file %q", f.Name())
	}
	return contents[from:to]
}

// ParseFromMainGo reads main.go and parses its declarations into decls -- see object Declarations.
func (s *State) ParseFromMainGo(msg kernel.Message, cursor Cursor, decls *Declarations) error {
	pi := &parseInfo{
		cursor:  cursor,
		fileSet: token.NewFileSet(),
	}
	packages, err := parser.ParseDir(pi.fileSet, s.TempDir, nil, parser.SkipObjectResolution|parser.AllErrors)
	if err != nil {
		if msg != nil {
			s.DisplayErrorWithContext(msg, err.Error())
		}
		return errors.Wrapf(err, "parsing go files in TempDir %s", s.TempDir)
	}
	pi.filesContents = make(map[string]string)

	// Debugging new types of parsing:
	//  fmt.Printf("Parsing results:\n")
	//  _ = ast.Print(fileSet, packages)

	for name, pkgAst := range packages {
		if name != "main" {
			log.Printf("WARNING: found package %s while parsing imports, but we expected only package main.", name)
			continue
		}
		for _, fileObj := range pkgAst.Files {
			// Currently, there is only `main.go` file.
			//fmt.Printf("File: %q\n", fileObj.Name.Name)
			filePath := path.Join(s.TempDir, fileObj.Name.Name) + ".go"
			content, err := os.ReadFile(filePath)
			if err != nil {
				return errors.Wrapf(err, "Failed to read %q", fileObj.Name)
			}
			pi.filesContents[filePath] = string(content)
			// Incorporate Imports
			for _, entry := range fileObj.Imports {
				pi.ParseImportEntry(decls, entry)
			}

			// Enumerate various declarations.
			for _, decl := range fileObj.Decls {
				switch typedDecl := decl.(type) {
				case *ast.FuncDecl:
					pi.ParseFuncEntry(decls, typedDecl)
				case *ast.GenDecl:
					if typedDecl.Tok == token.IMPORT {
						// Imports are handled above.
						continue
					} else if typedDecl.Tok == token.VAR {
						pi.ParseVarEntry(decls, typedDecl)
					} else if typedDecl.Tok == token.CONST {
						pi.ParseConstEntry(decls, typedDecl)
					} else if typedDecl.Tok == token.TYPE {
						pi.ParseTypeEntry(decls, typedDecl)
					} else {
						fmt.Printf("Dropped unknown generic declaration of type %s\n", typedDecl.Tok)
					}
				default:
					fmt.Printf("Dropped unknown declaration type\n")
				}
			}
		}
	}
	return nil
}

// ParseImportEntry registers a new Import declaration based on the ast.ImportSpec. See State.ParseFromMainGo
func (pi *parseInfo) ParseImportEntry(decls *Declarations, entry *ast.ImportSpec) {
	var alias string
	if entry.Name != nil {
		alias = entry.Name.Name
	}
	value := entry.Path.Value
	value = value[1 : len(value)-1] // Remove quotes.
	importEntry := NewImport(value, alias)

	// Find if and where the cursor may be.
	if c := pi.getCursor(entry.Path); c.HasCursor() {
		importEntry.CursorInPath = true
		importEntry.Cursor = c
	} else if entry.Name != nil {
		if c := pi.getCursor(entry.Name); c.HasCursor() {
			importEntry.CursorInAlias = true
			importEntry.Cursor = c
		}
	}
	decls.Imports[importEntry.Key] = importEntry
}

// ParseFuncEntry registers a new `func` declaration based on the ast.FuncDecl. See State.ParseFromMainGo
func (pi *parseInfo) ParseFuncEntry(decls *Declarations, funcDecl *ast.FuncDecl) {
	// Incorporate functions.
	key := funcDecl.Name.Name
	if funcDecl.Recv != nil && len(funcDecl.Recv.List) > 0 {
		typeName := "unknown"
		switch fieldType := funcDecl.Recv.List[0].Type.(type) {
		case *ast.Ident:
			typeName = fieldType.Name
		case *ast.StarExpr:
			typeName = pi.extractContentOfNode(fieldType.X)
		}
		key = fmt.Sprintf("%s~%s", typeName, key)
	}
	f := &Function{Key: key, Definition: pi.extractContentOfNode(funcDecl)}
	f.Cursor = pi.getCursor(funcDecl)
	decls.Functions[f.Key] = f
}

// ParseVarEntry registers a new `var` declaration based on the ast.GenDecl. See State.ParseFromMainGo
func (pi *parseInfo) ParseVarEntry(decls *Declarations, genDecl *ast.GenDecl) {
	// Multiple declarations in the same line may share the cursor (e.g: `var a, b int` if the cursor
	// is in the `int` token). Only the first definition ('a' in the example) takes the cursor.
	cursorFound := false
	for _, spec := range genDecl.Specs {
		vSpec := spec.(*ast.ValueSpec)
		vType := vSpec.Type
		var typeDefinition string
		cursorInType := NoCursor
		if vType != nil {
			typeDefinition = pi.extractContentOfNode(vType)
			cursorInType = pi.getCursor(vType)
		}
		// Each spec may be a list of variables (comma separated).
		for nameIdx, name := range vSpec.Names {
			v := &Variable{Name: name.Name, TypeDefinition: typeDefinition}
			if !cursorFound {
				if c := pi.getCursor(name); c.HasCursor() {
					v.CursorInName = true
					v.Cursor = c
					cursorFound = true
				}
			}
			if !cursorFound && cursorInType.HasCursor() {
				v.CursorInType = true
				v.Cursor = cursorInType
				cursorFound = true
			}
			if len(vSpec.Values) > nameIdx {
				v.ValueDefinition = pi.extractContentOfNode(vSpec.Values[nameIdx])
				if !cursorFound {
					if c := pi.getCursor(vSpec.Values[nameIdx]); c.HasCursor() {
						v.CursorInValue = true
						v.Cursor = c
						cursorFound = true
					}
				}
			}

			v.Key = v.Name
			if v.Name == "_" {
				// Each un-named reference has a unique key.
				v.Key = "_~" + strconv.Itoa(rand.Int()%0xFFFF)
			}
			decls.Variables[v.Key] = v
		}
	}
}

// ParseConstEntry registers a new `const` declaration based on the ast.GenDecl. See State.ParseFromMainGo
func (pi *parseInfo) ParseConstEntry(decls *Declarations, typedDecl *ast.GenDecl) {
	var prevConstDecl *Constant
	// Multiple declarations in the same line may share the cursor (e.g: `var a, b int` if the cursor
	// is in the `int` token). Only the first definition ('a' in the example) takes the cursor.
	cursorFound := false
	for _, spec := range typedDecl.Specs {
		vSpec := spec.(*ast.ValueSpec)
		vType := vSpec.Type
		var typeDefinition string
		cursorInType := NoCursor
		if vType != nil {
			typeDefinition = pi.extractContentOfNode(vType)
			cursorInType = pi.getCursor(vType)
		}
		// Each spec may be a list of variables (comma separated).
		for nameIdx, name := range vSpec.Names {
			c := &Constant{Cursor: NoCursor, Key: name.Name, TypeDefinition: typeDefinition}
			c.Prev = prevConstDecl
			if c.Prev != nil {
				c.Prev.Next = c
			}
			prevConstDecl = c

			if !cursorFound {
				if cursor := pi.getCursor(name); cursor.HasCursor() {
					c.CursorInKey = true
					c.Cursor = cursor
					cursorFound = true
				}
			}
			if !cursorFound && cursorInType.HasCursor() {
				c.CursorInType = true
				c.Cursor = cursorInType
				cursorFound = true
			}
			if len(vSpec.Values) > nameIdx {
				c.ValueDefinition = pi.extractContentOfNode(vSpec.Values[nameIdx])
				if !cursorFound {
					if cursor := pi.getCursor(vSpec.Values[nameIdx]); cursor.HasCursor() {
						c.CursorInValue = true
						c.Cursor = cursor
						cursorFound = true
					}
				}
			}
			decls.Constants[c.Key] = c
		}
	}
}

func (pi *parseInfo) ParseTypeEntry(decls *Declarations, typedDecl *ast.GenDecl) {
	// There is usually only one spec for a TYPE declaration:
	for _, spec := range typedDecl.Specs {
		tSpec := spec.(*ast.TypeSpec)
		name := tSpec.Name.Name
		tDef := pi.extractContentOfNode(tSpec.Type)
		tDecl := &TypeDecl{Key: name, TypeDefinition: tDef}
		if c := pi.getCursor(tSpec.Name); c.HasCursor() {
			tDecl.Cursor = c
			tDecl.CursorInKey = true
		} else if c := pi.getCursor(tSpec.Type); c.HasCursor() {
			tDecl.Cursor = c
			tDecl.CursorInType = true
		}
		decls.Types[name] = tDecl
	}
}

// sortedKeys enumerate keys and sort them.
func sortedKeys[T any](m map[string]T) (keys []string) {
	keys = make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return
}

// RenderImports writes out `import ( ... )` for all imports in Declarations.
func (d *Declarations) RenderImports(w *WriterWithCursor) (cursor Cursor) {
	cursor = NoCursor
	if len(d.Imports) == 0 {
		return
	}

	w.Str("import (\n")
	for _, key := range sortedKeys(d.Imports) {
		importDecl := d.Imports[key]
		w.Str("\t")
		if importDecl.Alias != "" {
			if importDecl.CursorInAlias {
				cursor = w.CursorInFile(importDecl.Cursor)
			}
			w.Format("%s ", importDecl.Alias)
		}
		if importDecl.CursorInPath {
			cursor = w.CursorInFile(importDecl.Cursor)
		}
		w.Format("%q\n", importDecl.Path)
	}
	w.Str(")\n\n")
	return
}

// RenderVariables writes out `var ( ... )` for all variables in Declarations.
func (d *Declarations) RenderVariables(w *WriterWithCursor) (cursor Cursor) {
	cursor = NoCursor
	if len(d.Variables) == 0 {
		return
	}

	w.Str("var (\n")
	for _, key := range sortedKeys(d.Variables) {
		varDecl := d.Variables[key]
		w.Str("\t")
		if varDecl.CursorInName {
			cursor = w.CursorInFile(varDecl.Cursor)
		}
		w.Str(varDecl.Name)
		if varDecl.TypeDefinition != "" {
			w.Str(" ")
			if varDecl.CursorInType {
				cursor = w.CursorInFile(varDecl.Cursor)
			}
			w.Str(varDecl.TypeDefinition)
		}
		if varDecl.ValueDefinition != "" {
			w.Str(" = ")
			if varDecl.CursorInValue {
				cursor = w.CursorInFile(varDecl.Cursor)
			}
			w.Str(varDecl.ValueDefinition)
		}
		w.Str("\n")
	}
	w.Str(")\n\n")
	return
}

// RenderFunctions without comments, for all functions in Declarations.
func (d *Declarations) RenderFunctions(w *WriterWithCursor) (cursor Cursor) {
	cursor = NoCursor
	if len(d.Functions) == 0 {
		return
	}

	for _, key := range sortedKeys(d.Functions) {
		funcDecl := d.Functions[key]
		def := funcDecl.Definition
		if funcDecl.HasCursor() {
			cursor = w.CursorInFile(funcDecl.Cursor)
		}
		if strings.HasPrefix(key, "init_") {
			// TODO: this will not work if there is a comment before the function
			//       which also has the string key. We need something more sophisticated.
			def = strings.Replace(def, key, "init", 1)
		}
		w.Format("%s\n\n", def)
	}
	return
}

// RenderTypes without comments.
func (d *Declarations) RenderTypes(w *WriterWithCursor) (cursor Cursor) {
	cursor = NoCursor
	if len(d.Types) == 0 {
		return
	}

	for _, key := range sortedKeys(d.Types) {
		typeDecl := d.Types[key]
		w.Str("type ")
		if typeDecl.CursorInKey {
			cursor = w.CursorInFile(typeDecl.Cursor)
		}
		w.Format("%s ", key)
		if typeDecl.CursorInType {
			cursor = w.CursorInFile(typeDecl.Cursor)
		}
		w.Format("%s\n", typeDecl.TypeDefinition)
	}
	w.Str("\n")
	return
}

// RenderConstants without comments for all constants in Declarations.
//
// Constants are trickier to render because when they are defined in a block,
// using `iota`, their ordering matters. So we re-render them in the same order
// and blocks as they were originally parsed.
//
// The ordering is given by the sort order of the first element of each `const` block.
func (d *Declarations) RenderConstants(w *WriterWithCursor) (cursor Cursor) {
	cursor = NoCursor
	if len(d.Constants) == 0 {
		return
	}

	// Enumerate heads of const blocks.
	headKeys := make([]string, 0, len(d.Constants))
	for key, constDecl := range d.Constants {
		if constDecl.Prev == nil {
			// Head of the const block.
			headKeys = append(headKeys, key)
		}
	}
	sort.Strings(headKeys)

	for _, headKey := range headKeys {
		constDecl := d.Constants[headKey]
		if constDecl.Next == nil {
			// Render individual const declaration.
			w.Str("const ")
			constDecl.Render(w, &cursor)
			w.Str("\n\n")
			continue
		}
		// Render block of constants.
		w.Str("const (\n")
		for constDecl != nil {
			w.Str("\t")
			constDecl.Render(w, &cursor)
			w.Str("\n")
			constDecl = constDecl.Next
		}
		w.Str(")\n\n")
	}
	return
}

// Render Constant declaration (without the `const` keyword).
func (c *Constant) Render(w *WriterWithCursor, cursor *Cursor) {
	if c.CursorInKey {
		*cursor = w.CursorInFile(c.Cursor)
	}
	w.Str(c.Key)
	if c.TypeDefinition != "" {
		w.Str(" ")
		if c.CursorInType {
			*cursor = w.CursorInFile(c.Cursor)
		}
		w.Str(c.TypeDefinition)
	}
	if c.ValueDefinition != "" {
		w.Str(" = ")
		if c.CursorInValue {
			*cursor = w.CursorInFile(c.Cursor)
		}
		w.Str(c.ValueDefinition)
	}
}
