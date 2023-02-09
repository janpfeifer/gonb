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

// This file implements functions related to the parsing of the Go code. It's required,
// so we can properly merge code coming from the execution of different cells.

// extractContent from files, given the golang parser's tokens.
//
// This is used to get the exact definition (string) of an element (function, variable, const, import, type, etc.)
func extractContent(filesContents map[string]string, fileSet *token.FileSet, node ast.Node) string {
	f := fileSet.File(node.Pos())
	from, to := f.Offset(node.Pos()), f.Offset(node.End())
	contents, found := filesContents[f.Name()]
	if !found {
		return fmt.Sprintf("Didn't find file %q", f.Name())
	}
	return contents[from:to]
}

// ParseImportsFromMainGo reads main.go and parses its declarations into decls -- see object Declarations.
func (s *State) ParseImportsFromMainGo(msg *kernel.Message, decls *Declarations) error {
	fileSet := token.NewFileSet()
	packages, err := parser.ParseDir(fileSet, s.TempDir, nil, parser.SkipObjectResolution|parser.AllErrors)
	if err != nil {
		if msg != nil {
			s.DisplayErrorWithContext(msg, err.Error())
		}
		return errors.Wrapf(err, "parsing go files in TempDir %s", s.TempDir)
	}
	filesContents := make(map[string]string)

	// Debugging new types of parsing:
	//  fmt.Printf("Parsing results:\n")
	//  _ = ast.Print(fileSet, packages)

	for name, pkgAst := range packages {
		if name != "main" {
			log.Printf("WARNING: found package %s while parsing imports, but we expected only package main.", name)
			continue
		}
		for _, fileObj := range pkgAst.Files {
			fmt.Printf("File: %q\n", fileObj.Name.Name)
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
				decls.Imports[importEntry.Key] = importEntry
			}

			// Enumerate various declarations.
			for _, decl := range fileObj.Decls {
				switch typedDecl := decl.(type) {
				case *ast.FuncDecl:
					// Incorporate functions.
					key := typedDecl.Name.Name
					if typedDecl.Recv != nil && len(typedDecl.Recv.List) > 0 {
						key = fmt.Sprintf("%s~%s", typedDecl.Recv.List[0].Type.(*ast.Ident).Name, key)
					}
					f := &Function{Key: key, Definition: extractContent(filesContents, fileSet, typedDecl)}
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
							// Each spec may be a list of variables (comma separated).
							vSpec := spec.(*ast.ValueSpec)
							vType := vSpec.Type
							var typeDefinition string
							if vType != nil {
								typeDefinition = extractContent(filesContents, fileSet, vType)
							}
							_ = vType
							for nameIdx, name := range vSpec.Names {
								// Incorporate variable.
								var valueDefinition string
								if len(vSpec.Values) > nameIdx {
									valueDefinition = extractContent(filesContents, fileSet, vSpec.Values[nameIdx])
								}
								if isVar {
									v := &Variable{Name: name.Name, TypeDefinition: typeDefinition, ValueDefinition: valueDefinition}
									v.Key = v.Name
									if v.Name == "_" {
										// Each un-named reference has a unique key.
										v.Key = "_~" + strconv.Itoa(rand.Int()%0xFFFF)
									}
									decls.Variables[v.Key] = v
								} else {
									c := &Constant{Key: name.Name, TypeDefinition: typeDefinition, ValueDefinition: valueDefinition}
									c.Prev = prevConstDecl
									if c.Prev != nil {
										c.Prev.Next = c
									}
									prevConstDecl = c
									decls.Constants[c.Key] = c
								}
							}
						}
					} else if typedDecl.Tok == token.TYPE {
						// There is usually only one spec for a TYPE declaration:
						for _, spec := range typedDecl.Specs {
							tSpec := spec.(*ast.TypeSpec)
							name := tSpec.Name.Name
							tDef := extractContent(filesContents, fileSet, tSpec.Type)
							decls.Types[name] = &TypeDecl{Key: name, TypeDefinition: tDef}
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

func (d *Declarations) RenderImports(writer io.Writer) (err error) {
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
		_, err = fmt.Fprintf(writer, format, args...)
	}

	w("import (\n")
	for _, key := range keys {
		importDecl := d.Imports[key]
		if importDecl.Alias != "" {
			w("\t%s %q\n", importDecl.Alias, importDecl.Path)
		} else {
			w("\t%q\n", importDecl.Path)
		}
	}
	w(")\n")
	return
}

func (d *Declarations) RenderVariables(writer io.Writer) (err error) {
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
		_, err = fmt.Fprintf(writer, format, args...)
	}

	w("var (\n")
	for _, key := range keys {
		varDecl := d.Variables[key]
		var typeStr string
		if varDecl.TypeDefinition != "" {
			typeStr = " " + varDecl.TypeDefinition
		}
		w("\t%s%s = %s\n", varDecl.Name, typeStr, varDecl.ValueDefinition)
	}
	w(")\n")
	return
}

// RenderFunctions without comments.
func (d *Declarations) RenderFunctions(writer io.Writer) (err error) {
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
		_, err = fmt.Fprintf(writer, format, args...)
	}

	for _, key := range keys {
		funcDecl := d.Functions[key]
		def := funcDecl.Definition
		if strings.HasPrefix(key, "init_") {
			def = strings.Replace(def, key, "init", 1)
		}
		w("%s\n", def)
	}
	return
}

// RenderTypes without comments.
func (d *Declarations) RenderTypes(writer io.Writer) (err error) {
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
		_, err = fmt.Fprintf(writer, format, args...)
	}

	for _, key := range keys {
		typeDecl := d.Types[key]
		w("type %s %s\n", key, typeDecl.TypeDefinition)
	}
	return
}

// RenderConstants without comments.
//
// Constants are trickier to render because when they are defined in a block,
// using `iota`, their ordering matters. So we re-render them in the same order
// and blocks as they were originally parsed.
//
// The ordering is given by the sort order of the first element of each `const` block.
func (d *Declarations) RenderConstants(writer io.Writer) (err error) {
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
		_, err = fmt.Fprintf(writer, format, args...)
	}

	for _, headKey := range headKeys {
		constDecl := d.Constants[headKey]
		if constDecl.Next == nil {
			// Render individual const declaration.
			w("const %s\n", constDecl.Render())
			continue
		}
		// Render block of constants.
		w("const (\n")
		for constDecl != nil {
			w("\t%s\n", constDecl.Render())
			constDecl = constDecl.Next
		}
		w(")\n")
	}
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
