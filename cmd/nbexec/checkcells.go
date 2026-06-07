package main

import (
	"encoding/json"
	"fmt"
	"os"
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
	OutputType string   `json:"output_type"`
	Name       string   `json:"name"`
	Text       []string `json:"text"`
	Ename      string   `json:"ename"`
	Evalue     string   `json:"evalue"`
	Traceback  []string `json:"traceback"`
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

		for _, output := range cell.Outputs {
			if output.OutputType == "error" {
				cellFailed = true
				errType = output.Ename
				errMsg = output.Evalue
				traceback = output.Traceback
				break
			}
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
