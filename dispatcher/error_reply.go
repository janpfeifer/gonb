package dispatcher

import (
	"github.com/janpfeifer/gonb/goexec"
	"github.com/pkg/errors"
)

func unwrap(err error) (string, string, []string) {
	var nbErr *goexec.GonbError
	if errors.As(err, nbErr); nbErr != nil {
		return nbErr.ErrorName(), nbErr.ErrorMsg(), nbErr.Traceback()
	} else {

		return "ERROR", err.Error(), []string{err.Error()}
	}
}
