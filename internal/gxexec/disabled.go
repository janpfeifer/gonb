//go:build !gx

package gxexec

import (
	"github.com/janpfeifer/gonb/internal/goexec"
	"github.com/janpfeifer/gonb/internal/kernel"
	"github.com/pkg/errors"
)

// Executes the %%gx cell magic command.
func ExecuteCell(msg kernel.Message, goExec *goexec.State, lines []string) error {
	return errors.Errorf("%%gx is disabled. Compile gonb with the 'gx' build tag to enable this feature.")
}
