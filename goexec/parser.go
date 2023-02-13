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

// This file implements functions related to the parsing of the Go code.
// It is used to properly merge code coming from the execution of different cells.

// extractContentOfNode from files, given the golang parser's tokens.
//
// Currently, we generate all the content in file `main.go`, so fileContents will only have
// one entry.
//
// This is used to get the exact definition (string) of an element (function, variable, const, import, type, etc.)
func extractContentOfNode(filesContents map[string]string, fileSet *token.FileSet, node ast.Node) string {
	f := fileSet.File(node.Pos())
	from, to := f.Offset(node.Pos()), f.Offset(node.End())
	contents, found := filesContents[f.Name()]
	if !found {
		return fmt.Sprintf("Didn't find file %q", f.Name())
	}
	return contents[from:to]
}

// countLines returns the number of "\n" in the string.
func countLines(definition string) int {
	return strings.Count(definition, "\n")
}

// ParseImportsFromMainGo reads main.go and parses its declarations into decls -- see object Declarations.
func (s *State) ParseImportsFromMainGo(msg kernel.Message, cursor Cursor, decls *Declarations) error {
	fileSet := token.NewFileSet()
	packages, err := parser.ParseDir(fileSet, s.TempDir, nil, parser.SkipObjectResolution|parser.AllErrors)
	if err != nil {
		if msg != nil {
			s.DisplayErrorWithContext(msg, err.Error())
		}
		return errors.Wrapf(err, "parsing go files in TempDir %s", s.TempDir)
	}
	filesContents := make(map[string]string)

	if cursor.HasCursor() {
		log.Printf("Cursor=%+v", cursor)
	}

	// getCursor returns the cursor position within this declaration, if the original cursor falls in there.
	getCursor := func(node ast.Node) Cursor {
		from, to := node.Pos(), node.End()
		if !cursor.HasCursor() {
			return NoCursor
		}
		fromPos, toPos := fileSet.Position(from), fileSet.Position(to)
		for line := fromPos.Line; line <= toPos.Line; line++ {
			// Notice that parser lines are 1-based, we keep them 0-based in the cursor.
			if int32(line-1) == cursor.Line {
				c := Cursor{int32(line - fromPos.Line), cursor.Col}
				//log.Printf("Found cursor at %v in definition:\n%s", c,
				//	extractContentOfNode(filesContents, fileSet, node))
				return c
			}
		}
		return NoCursor
	}

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
			filesContents[filePath] = string(content)
			// Incorporate Imports
			for _, entry := range fileObj.Imports {
				var alias string
				if entry.Name != nil {
					alias = entry.Name.Name
				}
				value := entry.Path.Value
				value = value[1 : len(value)-1] // Remove quotes.
				importEntry := NewImport(value, alias)
				importEntry.Cursor = getCursor(entry)
				decls.Imports[importEntry.Key] = importEntry
			}

			// Enumerate various declarations.
			for _, decl := range fileObj.Decls {
				switch typedDecl := decl.(type) {
				case *ast.FuncDecl:
					// Incorporate functions.
					key := typedDecl.Name.Name
					if typedDecl.Recv != nil && len(typedDecl.Recv.List) > 0 {
						typeName := "unknown"
						switch fieldType := typedDecl.Recv.List[0].Type.(type) {
						case *ast.Ident:
							typeName = fieldType.Name
						case *ast.StarExpr:
							typeName = extractContentOfNode(filesContents, fileSet, fieldType.X)
						}
						key = fmt.Sprintf("%s~%s", typeName, key)
					}
					f := &Function{Key: key, Definition: extractContentOfNode(filesContents, fileSet, typedDecl)}
					f.Cursor = getCursor(typedDecl)
					decls.Functions[f.Key] = f
				case *ast.GenDecl:
					if typedDecl.Tok == token.IMPORT {
						// Imports are handled above.
						continue
					} else if typedDecl.Tok == token.VAR || typedDecl.Tok == token.CONST {
						// Loop over variable/const definitions.
						isVar := typedDecl.Tok == token.VAR
						var prevConstDecl *Constant

						for _, spec := range typedDecl.Specs {
							newCursor := getCursor(spec)

							// Each spec may be a list of variables (comma separated).
							vSpec := spec.(*ast.ValueSpec)
							vType := vSpec.Type
							var typeDefinition string
							if vType != nil {
								typeDefinition = extractContentOfNode(filesContents, fileSet, vType)
							}
							_ = vType
							for nameIdx, name := range vSpec.Names {
								// Incorporate variable.
								var valueDefinition string
								if len(vSpec.Values) > nameIdx {
									valueDefinition = extractContentOfNode(filesContents, fileSet, vSpec.Values[nameIdx])
								}
								if isVar {
									v := &Variable{Name: name.Name, TypeDefinition: typeDefinition, ValueDefinition: valueDefinition}
									v.Key = v.Name
									if v.Name == "_" {
										// Each un-named reference has a unique key.
										v.Key = "_~" + strconv.Itoa(rand.Int()%0xFFFF)
									}
									v.Cursor = newCursor // TODO: Needs to adjust column position, if multiple definitions in the same line.
									decls.Variables[v.Key] = v
								} else {
									c := &Constant{Key: name.Name, TypeDefinition: typeDefinition, ValueDefinition: valueDefinition}
									c.Prev = prevConstDecl
									if c.Prev != nil {
										c.Prev.Next = c
									}
									prevConstDecl = c
									c.Cursor = newCursor // TODO: Needs to adjust column position, if multiple definitions in the same line.
									decls.Constants[c.Key] = c
								}
							}
						}
					} else if typedDecl.Tok == token.TYPE {
						// There is usually only one spec for a TYPE declaration:
						for _, spec := range typedDecl.Specs {
							tSpec := spec.(*ast.TypeSpec)
							name := tSpec.Name.Name
							tDef := extractContentOfNode(filesContents, fileSet, tSpec.Type)
							tDecl := &TypeDecl{Key: name, TypeDefinition: tDef}
							tDecl.Cursor = getCursor(spec)
							decls.Types[name] = tDecl
						}
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

// RenderImports writes out `import ( ... )` for all imports in Declarations.
func (d *Declarations) RenderImports(lineNum int, writer io.Writer) (newLineNum int, cursor Cursor, err error) {
	cursor = NoCursor
	newLineNum = lineNum
	if len(d.Imports) == 0 {
		return
	}

	// Enumerate imports sorted by keys.
	keys := make([]string, 0, len(d.Imports))
	for key := range d.Imports {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	// Convenience function to write one line and handle error.
	w := func(format string, args ...any) {
		if err != nil {
			return
		}
		strBuf := fmt.Sprintf(format, args...)
		lineNum += countLines(strBuf)
		_, err = fmt.Fprint(writer, strBuf)
	}

	w("import (\n")
	for _, key := range keys {
		importDecl := d.Imports[key]
		if importDecl.HasCursor() {
			cursor = importDecl.Cursor
			cursor.Line += int32(lineNum)
		}
		if importDecl.Alias != "" {
			w("\t%s %q\n", importDecl.Alias, importDecl.Path)
		} else {
			w("\t%q\n", importDecl.Path)
		}
	}
	w(")\n\n")
	newLineNum = lineNum
	return
}

// RenderVariables writes out `var ( ... )` for all variables in Declarations.
func (d *Declarations) RenderVariables(lineNum int, writer io.Writer) (newLineNum int, cursor Cursor, err error) {
	cursor = NoCursor
	newLineNum = lineNum
	if len(d.Variables) == 0 {
		return
	}

	// Enumerate variables sorted by keys.
	keys := make([]string, 0, len(d.Variables))
	for key := range d.Variables {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	// Convenience function to write one line and handle error.
	w := func(format string, args ...any) {
		if err != nil {
			return
		}
		strBuf := fmt.Sprintf(format, args...)
		lineNum += countLines(strBuf)
		_, err = fmt.Fprint(writer, strBuf)
	}

	w("var (\n")
	for _, key := range keys {
		varDecl := d.Variables[key]
		var typeStr string
		if varDecl.TypeDefinition != "" {
			typeStr = " " + varDecl.TypeDefinition
		}
		if varDecl.HasCursor() {
			cursor = varDecl.Cursor
			cursor.Line += int32(lineNum)
		}
		w("\t%s%s = %s\n", varDecl.Name, typeStr, varDecl.ValueDefinition)
	}
	w(")\n\n")
	newLineNum = lineNum
	return
}

// RenderFunctions without comments, for all functions in Declarations.
func (d *Declarations) RenderFunctions(lineNum int, writer io.Writer) (newLineNum int, cursor Cursor, err error) {
	cursor = NoCursor
	newLineNum = lineNum
	if len(d.Functions) == 0 {
		return
	}

	// Enumerate variables sorted by keys.
	keys := make([]string, 0, len(d.Functions))
	for key := range d.Functions {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	// Convenience function to write one line and handle error.
	w := func(format string, args ...any) {
		if err != nil {
			return
		}
		strBuf := fmt.Sprintf(format, args...)
		lineNum += countLines(strBuf)
		_, err = fmt.Fprint(writer, strBuf)
	}

	for _, key := range keys {
		funcDecl := d.Functions[key]
		def := funcDecl.Definition
		if funcDecl.HasCursor() {
			cursor = funcDecl.Cursor
			cursor.Line += int32(lineNum)
		}
		if strings.HasPrefix(key, "init_") {
			def = strings.Replace(def, key, "init", 1)
			if funcDecl.HasCursor() && cursor.Line == int32(lineNum) && cursor.Col >= 9 {
				// Shift the cursor position the characters removed from the key.
				cursor.Col -= int32(len(key) - len("init"))
			}
		}
		w("%s\n\n", def)
	}
	newLineNum = lineNum
	return
}

// RenderTypes without comments.
func (d *Declarations) RenderTypes(lineNum int, writer io.Writer) (newLineNum int, cursor Cursor, err error) {
	cursor = NoCursor
	newLineNum = lineNum
	if len(d.Types) == 0 {
		return
	}

	// Enumerate variables sorted by keys.
	keys := make([]string, 0, len(d.Types))
	for key := range d.Types {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	// Convenience function to write one line and handle error.
	w := func(format string, args ...any) {
		if err != nil {
			return
		}
		strBuf := fmt.Sprintf(format, args...)
		lineNum += countLines(strBuf)
		_, err = fmt.Fprint(writer, strBuf)
	}

	for _, key := range keys {
		typeDecl := d.Types[key]
		if typeDecl.HasCursor() {
			cursor = typeDecl.Cursor
			cursor.Line += int32(lineNum)
		}
		w("type %s %s\n", key, typeDecl.TypeDefinition)
	}
	w("\n")
	newLineNum = lineNum
	return
}

// RenderConstants without comments for all constants in Declarations.
//
// Constants are trickier to render because when they are defined in a block,
// using `iota`, their ordering matters. So we re-render them in the same order
// and blocks as they were originally parsed.
//
// The ordering is given by the sort order of the first element of each `const` block.
func (d *Declarations) RenderConstants(lineNum int, writer io.Writer) (newLineNum int, cursor Cursor, err error) {
	cursor = NoCursor
	newLineNum = lineNum
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

	// Convenience function to write one line and handle error.
	w := func(format string, args ...any) {
		if err != nil {
			return
		}
		strBuf := fmt.Sprintf(format, args...)
		lineNum += countLines(strBuf)
		_, err = fmt.Fprint(writer, strBuf)
	}

	for _, headKey := range headKeys {
		constDecl := d.Constants[headKey]
		if constDecl.Next == nil {
			// Render individual const declaration.
			if constDecl.HasCursor() {
				cursor = constDecl.Cursor
				cursor.Line += int32(lineNum)
			}
			w("const %s\n\n", constDecl.Render())
			continue
		}
		// Render block of constants.
		w("const (\n")
		for constDecl != nil {
			if constDecl.HasCursor() {
				cursor = constDecl.Cursor
				cursor.Line += int32(lineNum)
			}
			w("\t%s\n", constDecl.Render())
			constDecl = constDecl.Next
		}
		w(")\n\n")
	}
	newLineNum = lineNum
	return
}

// Render Constant declaration (without the `const` keyword).
func (c *Constant) Render() string {
	r := c.Key
	if c.TypeDefinition != "" {
		r += " " + c.TypeDefinition
	}
	if c.ValueDefinition != "" {
		r += " = " + c.ValueDefinition
	}
	return r
}
