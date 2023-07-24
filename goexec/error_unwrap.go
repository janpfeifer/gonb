package goexec

import (
	"github.com/pkg/errors"
)

func Unwrap(err error) (string, string, []string) {
	var nbErr *GonbError
	if errors.As(err, &nbErr) {
		return nbErr.ErrorName(), nbErr.ErrorMsg(), nbErr.Traceback()
	} else {
		return "ERROR", err.Error(), []string{err.Error()}
	}
}
