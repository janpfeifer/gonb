package specialcmd

import (
	"fmt"
	"github.com/janpfeifer/gonb/internal/goexec"
	"github.com/janpfeifer/gonb/internal/kernel"
	"k8s.io/klog/v2"
	"strings"
)

// execTrack executes the "%track" special command. The parameter `args` excludes
// "%track".
func execTrack(msg kernel.Message, goExec *goexec.State, args []string) {
	if len(args) == 0 {
		showTrackedList(msg, goExec)
		return
	}
	for _, fileOrDirPath := range args {
		err := goExec.Track(fileOrDirPath)
		if err != nil {
			err = kernel.PublishWriteStream(msg, kernel.StreamStderr, err.Error()+"\n")
		} else {
			err = kernel.PublishWriteStream(msg, kernel.StreamStdout,
				fmt.Sprintf("\tTracking %q\n", fileOrDirPath))
		}
		if err != nil {
			klog.Errorf("Failed to publish to Jupyter: %+v", err)
			return
		}
	}
}

// execUntrack executes the "%track" special command. The parameter `args` excludes
// "%untrack".
func execUntrack(msg kernel.Message, goExec *goexec.State, args []string) {
	if len(args) == 0 {
		showTrackedList(msg, goExec)
		return
	}
	for _, fileOrDirPath := range args {
		err := goExec.Untrack(fileOrDirPath)
		if err != nil {
			err = kernel.PublishWriteStream(msg, kernel.StreamStderr, err.Error()+"\n")
		} else {
			err = kernel.PublishWriteStream(msg, kernel.StreamStdout,
				fmt.Sprintf("\tUntracked %q\n", fileOrDirPath))
		}
		if err != nil {
			klog.Errorf("Failed to publish to Jupyter: %+v", err)
			return
		}
	}

}

func showTrackedList(msg kernel.Message, goExec *goexec.State) {
	tracked := goExec.ListTracked()
	htmlParts := make([]string, 0, len(tracked)+5)
	if len(tracked) == 0 {
		htmlParts = append(htmlParts, "<b>No files or directory being tracked yet</b>")
	} else {
		htmlParts = append(htmlParts, "<b>List of files/directories being tracked:</b>")
		htmlParts = append(htmlParts, "<ul>")
		for _, p := range tracked {
			htmlParts = append(htmlParts, "<li>"+p+"</li>")
		}
		htmlParts = append(htmlParts, "</ul>")
	}
	err := kernel.PublishHtml(msg, strings.Join(htmlParts, "\n")+"\n")
	if err != nil {
		klog.Errorf("Failed to publish track results back to jupyter: %+v", err)
	}
}
