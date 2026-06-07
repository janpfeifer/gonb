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

		// First pass: look for explicit error or rich HTML error reports
		hasRichError := false
		var richErrType, richErrMsg string
		var richTraceback []string

		for _, output := range cell.Outputs {
			if output.OutputType == "error" {
				cellFailed = true
				errType = output.Ename
				errMsg = output.Evalue
				traceback = output.Traceback
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
						// Convert HTML to clean plain text lines
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

		if hasRichError {
			cellFailed = true
			if errType == "" || errType == "ERROR" {
				if errMsg == "exit status 1" {
					errType = "Runtime Error"
				} else {
					errType = richErrType
				}
			}
			errMsg = richErrMsg
			traceback = richTraceback
		}

		// Second pass: if no explicit error was found, check stderr stream for exit status
		if !cellFailed {
			for _, output := range cell.Outputs {
				if output.OutputType == "stream" && output.Name == "stderr" {
					// Check if any of the lines indicate a failure (exit status or panic)
					hasExitStatus := false
					for _, line := range output.Text {
						if strings.Contains(line, "exit status ") || strings.Contains(line, "panic:") {
							hasExitStatus = true
							break
						}
					}
					if hasExitStatus {
						cellFailed = true
						errType = "Runtime Error"
						errMsg = strings.Join(output.Text, "")
						traceback = output.Text
						break
					}
				}
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
