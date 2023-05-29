package specialcmd

import (
	"fmt"
	"github.com/janpfeifer/gonb/common"
	"github.com/janpfeifer/gonb/goexec"
	"github.com/janpfeifer/gonb/kernel"
	"k8s.io/klog/v2"
	"strings"
)

// This file handle the commands %list (or %ls), %remove (%rm) and %reset, which help manipulate
// memorized definitions.

// reset removes all definitions memorized, as if the kernel had been reset.
func resetDefinitions(msg kernel.Message, goExec *goexec.State) {
	goExec.Reset()
	err := kernel.PublishWriteStream(msg, kernel.StreamStdout, "* State reset: all memorized declarations discarded.\n")
	if err != nil {
		klog.Infof("Error while resetting kernel: %+v", err)
	}
}

func displayEnumeration(msg kernel.Message, title string, items []string) {
	if len(items) == 0 {
		return
	}
	htmlParts := make([]string, 0, len(items)+3)
	htmlParts = append(htmlParts, "<h4>"+title+"</h4>")
	htmlParts = append(htmlParts, "<ul>")
	for _, item := range items {
		htmlParts = append(htmlParts, "<li><pre>"+item+"</pre></li>")
	}
	htmlParts = append(htmlParts, "</ul>")
	err := kernel.PublishDisplayDataWithHTML(msg, strings.Join(htmlParts, "\n"))
	if err != nil {
		klog.Errorf("Failed to publish list for %q back to jupyter: %+v", title, err)
	}
}

// listDefinitions lists all memorized definitions. It implements the "%list" (or "%ls") command.
func listDefinitions(msg kernel.Message, goExec *goexec.State) {
	_ = kernel.PublishDisplayDataWithHTML(msg, "<h3>Memorized Definitions</h3>\n")
	displayEnumeration(msg, "Imports", common.SortedKeys(goExec.Definitions.Imports))
	displayEnumeration(msg, "Constants", common.SortedKeys(goExec.Definitions.Constants))
	displayEnumeration(msg, "Types", common.SortedKeys(goExec.Definitions.Types))
	displayEnumeration(msg, "Variables", common.SortedKeys(goExec.Definitions.Variables))
	displayEnumeration(msg, "Functions", common.SortedKeys(goExec.Definitions.Functions))
}

func removeDefinitionImpl[T any](msg kernel.Message, mapName string, m *map[string]*T, key string) bool {
	_, found := (*m)[key]
	if !found {
		return false
	}
	delete(*m, key)
	err := kernel.PublishWriteStream(msg, kernel.StreamStdout,
		fmt.Sprintf(". removed %s %s\n", mapName, key))
	if err != nil {
		klog.Errorf("Failed to publish back to jupyter output of removing definitions: %+v", err)
	}
	return true
}

// removeDefinitions form memorized list. It implements the "%remove" (or "%rm") command.
func removeDefinitions(msg kernel.Message, goExec *goexec.State, keys []string) {
	for _, key := range keys {
		var found bool
		found = found || removeDefinitionImpl(msg, "import", &goExec.Definitions.Imports, key)
		found = found || removeDefinitionImpl(msg, "const", &goExec.Definitions.Constants, key)
		found = found || removeDefinitionImpl(msg, "type", &goExec.Definitions.Types, key)
		found = found || removeDefinitionImpl(msg, "var", &goExec.Definitions.Variables, key)
		found = found || removeDefinitionImpl(msg, "func", &goExec.Definitions.Functions, key)
		if !found {
			err := kernel.PublishWriteStream(msg, kernel.StreamStderr,
				fmt.Sprintf(". key %q not found in any definition, not removed\n", key))
			if err != nil {
				klog.Errorf("Failed to publish back to jupyter output of removing definitions: %+v", err)
			}
		}
	}
}
