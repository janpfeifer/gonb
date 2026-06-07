package main

import (
	"encoding/json"
	"fmt"
	"html"
	"os"
	"regexp"
	"strings"

	"k8s.io/klog/v2"
)

// Notebook represents a Jupyter notebook JSON structure.
type Notebook struct {
	Cells []Cell `json:"cells"`
}

// Cell represents a single notebook cell.
type Cell struct {
	CellType       string   `json:"cell_type"`
	Source         []string `json:"source"`
	Outputs        []Output `json:"outputs"`
	ExecutionCount *int     `json:"execution_count"`
}

// Output represents a single output from a cell.
type Output struct {
	OutputType string         `json:"output_type"`
	Name       string         `json:"name"`
	Text       []string       `json:"text"`
	Ename      string         `json:"ename"`
	Evalue     string         `json:"evalue"`
	Traceback  []string       `json:"traceback"`
	Data       map[string]any `json:"data"`
}

var reStyleBlock = regexp.MustCompile(`(?s)<style>.*?</style>`)
var reHTMLTag = regexp.MustCompile(`<[^>]*>`)

func stripHTML(s string) string {
	s = reStyleBlock.ReplaceAllString(s, "")
	s = reHTMLTag.ReplaceAllString(s, "")
	return html.UnescapeString(s)
}

// checkCells parses the executed notebook file, checks for any failed cells,
// prints a summary report, and exits with code 1 if any failures are found.
func checkCells(notebookPath string) {
	data, err := os.ReadFile(notebookPath)
	if err != nil {
		klog.Fatalf("Failed to read notebook file %q for cell checking: %v", notebookPath, err)
	}

	var nb Notebook
	if err := json.Unmarshal(data, &nb); err != nil {
		klog.Fatalf("Failed to parse notebook JSON from %q: %v", notebookPath, err)
	}

	hasErrors := false
	for i, cell := range nb.Cells {
		if cell.CellType != "code" {
			continue
		}

		cellFailed := false
		var errType, errMsg string
		var traceback []string

		// Prioritize failure check:
		// 1. Check stderr stream for failure indicators (like exit status or panic).
		// 2. Check for rich HTML error reports (for compiler errors).
		// 3. Check for explicit error outputs (for Python or other standard errors).
		var stderrText []string
		var hasStderr bool
		var hasExplicitError bool
		var explicitErrType, explicitErrMsg string
		var explicitTraceback []string
		var hasRichError bool
		var richErrType, richErrMsg string
		var richTraceback []string

		for _, output := range cell.Outputs {
			if output.OutputType == "stream" && output.Name == "stderr" {
				hasFailure := false
				for _, line := range output.Text {
					if strings.Contains(line, "exit status ") || strings.Contains(line, "panic:") {
						hasFailure = true
						break
					}
				}
				if hasFailure {
					hasStderr = true
					stderrText = output.Text
				}
			}
			if output.OutputType == "error" {
				hasExplicitError = true
				explicitErrType = output.Ename
				explicitErrMsg = output.Evalue
				explicitTraceback = output.Traceback
			}
			if output.OutputType == "display_data" && output.Data != nil {
				if htmlVal, ok := output.Data["text/html"]; ok {
					var htmlStr string
					switch v := htmlVal.(type) {
					case string:
						htmlStr = v
					case []any:
						var sb strings.Builder
						for _, item := range v {
							if s, ok := item.(string); ok {
								sb.WriteString(s)
							}
						}
						htmlStr = sb.String()
					}
					if strings.Contains(htmlStr, "gonb-err-location") {
						hasRichError = true
						if strings.Contains(htmlStr, "exit status") {
							richErrType = "Runtime Error"
						} else {
							richErrType = "Compilation Error"
						}
						htmlStr = stripHTML(htmlStr)
						lines := strings.Split(htmlStr, "\n")
						var tb []string
						for _, line := range lines {
							trimmed := strings.TrimSpace(line)
							if trimmed != "" {
								tb = append(tb, line+"\n")
							}
						}
						richTraceback = tb
						richErrMsg = strings.Join(tb, "")
					}
				}
			}
		}

		if hasStderr {
			cellFailed = true
			errType = "Runtime Error"
			errMsg = strings.Join(stderrText, "")
			traceback = stderrText
		} else if hasRichError {
			cellFailed = true
			errType = richErrType
			errMsg = richErrMsg
			traceback = richTraceback
		} else if hasExplicitError {
			cellFailed = true
			var cellStderr []string
			for _, output := range cell.Outputs {
				if output.OutputType == "stream" && output.Name == "stderr" {
					cellStderr = output.Text
					break
				}
			}
			if len(cellStderr) > 0 && (explicitErrMsg == "exit status 1" || strings.HasPrefix(explicitErrMsg, "exit status ")) {
				errType = "Runtime Error"
				errMsg = strings.Join(cellStderr, "")
				traceback = cellStderr
			} else {
				errType = explicitErrType
				errMsg = explicitErrMsg
				traceback = explicitTraceback
			}
		}

		if cellFailed {
			hasErrors = true
			fmt.Printf("Cell %d failed execution:\n", i+1)
			fmt.Println("Source:")
			for _, line := range cell.Source {
				fmt.Print("  ", line)
			}
			if len(cell.Source) > 0 && !strings.HasSuffix(cell.Source[len(cell.Source)-1], "\n") {
				fmt.Println()
			}
			fmt.Printf("Error: %s\n", errType)
			if errType == "Runtime Error" {
				fmt.Println("Stderr Output:")
			} else {
				fmt.Printf("Message: %s\n", errMsg)
				fmt.Println("Traceback:")
			}
			for _, trace := range traceback {
				fmt.Print("  ", trace)
			}
			if len(traceback) > 0 && !strings.HasSuffix(traceback[len(traceback)-1], "\n") {
				fmt.Println()
			}
			fmt.Println(strings.Repeat("-", 40))
		}
	}

	if hasErrors {
		os.Exit(1)
	}
}
