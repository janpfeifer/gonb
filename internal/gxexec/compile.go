//go:build gx

package gxexec

// Infrastructure for compiling GX source code
// This is quite raw, and will only compile GX source code that's
// fully correct. Ie, no automatic handling of `package` statements,
// or opther niceties that we'll want to add for notebooks.

import (
	"github.com/gx-org/gx/build/builder"
	"github.com/pkg/errors"
)

// Compiles the given gx source code.
// Returns an error if compilation fails.
// TODO: we need to figure oout how to load the GX base packages
// such as math. Currently, trying to use them will result in
// compilation errors.
func compile(src string) error {
	bld := builder.New(NewLoader())
	pkg := bld.NewIncrementalPackage("gxnb")

	err := pkg.Build(src)
	return err
}

// An implementation of builder.Loader that looks up built packages
// from a map.
type nbLoader struct {
	pkgs map[string]builder.Package
}

func NewLoader() *nbLoader {
	return &nbLoader{
		pkgs: make(map[string]builder.Package),
	}
}

func (loader *nbLoader) Load(builder *builder.Builder, path string) (builder.Package, error) {
	pkg, ok := loader.pkgs[path]
	if !ok {
		return nil, errors.Errorf("package %s has not been built", path)
	}
	return pkg, nil
}
