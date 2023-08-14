package goexec

import (
	"strings"
	"text/template"

	"k8s.io/klog/v2"
)

type GonbError struct {
	lines  []errorLine
	errMsg string
	error  error
}

var templateTraceback = template.Must(template.New("traceback").Parse(`
An error occurred while executing the following cell:
------------------
{cell.source}
------------------
{stream_output}

{traceback}
`))

// newError creates a new Gonb Error, translating line numbers to cell IDs.
//
// Since gonb errors are context dependent, this must be done immediately after the error is generated.
func newError(s *State, fileToCellIdAndLine []CellIdAndLine, errorMsg string, error error) *GonbError {
	// Read main.go into lines.
	mainGo, err := s.readMainGo()
	if err != nil {
		klog.Errorf("DisplayErrorWithContext: %+v", err)
		return nil
	}
	codeLines := strings.Split(mainGo, "\n")

	// Parse error lines.
	lines := strings.Split(errorMsg, "\n")
	nberr := &GonbError{lines: make([]errorLine, len(lines)), errMsg: errorMsg, error: error}
	for ii, line := range lines {
		parsed := s.parseErrorLine(line, codeLines, fileToCellIdAndLine)
		nberr.lines[ii] = parsed
	}
	return nberr
}

// Unwrap returns the underlying error
func (err *GonbError) Unwrap() error {
	return err.error
}

// Error returns the error message
func (err *GonbError) Error() string {
	return err.ErrorMsg()
}



// Traceback corresponds to traceback in jupyter
func (err *GonbError) Traceback() []string {
	traceback := make([]string, len(err.lines))
	for ii, line := range err.lines {
		traceback[ii] = line.getTraceback()
	}
	return traceback
}

// ErrorMsg corresponds to evalue in jupyter
func (err *GonbError) ErrorMsg() string {
	return err.errMsg
}

// ErrorName corresponds to ename in jupyter
func (err *GonbError) ErrorName() string {
	return "ERROR"
}

