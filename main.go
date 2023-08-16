package main

import (
	"flag"
	"fmt"
	"github.com/gofrs/uuid"
	"github.com/janpfeifer/gonb/dispatcher"
	"github.com/janpfeifer/gonb/goexec"
	"github.com/janpfeifer/gonb/kernel"
	"io"
	klog "k8s.io/klog/v2"
	"log"
	"os"
	"os/exec"
)

var (
	flagInstall  = flag.Bool("install", false, "Install kernel in local config, and make it available in Jupyter")
	flagKernel   = flag.String("kernel", "", "Exec kernel using given path for the `connection_file` provided by Jupyter client")
	flagExtraLog = flag.String("extra_log", "", "Extra file to include in the log.")
	flagForce    = flag.Bool("force", false, "Force install even if goimports and/or gopls are missing.")
	flagRawError = flag.Bool("raw_error", false, "When GoNB executes cells, force raw text errors instead of HTML errors, which facilitates command line testing of notebooks")
)

var (
	// UniqueID uniquely identifies a kernel execution. Used to create the temporary
	// directory holding the kernel code, and for logging.
	// Set by SetUpLogging.
	UniqueID string

	coloredUniqueID string
)

func init() {
	// Sets UniqueID.
	uuidTmp, _ := uuid.NewV7()
	uuidStr := uuidTmp.String()
	UniqueID = uuidStr[len(uuidStr)-8:]
	coloredUniqueID = fmt.Sprintf("%s[%s]%s ", ColorBgYellow, UniqueID, ColorReset)
}

func main() {
	klog.InitFlags(nil)
	defer klog.Flush()

	flag.Parse()
	SetUpLogging() // "log" package.
	SetUpKlog()    // "github.com/golang/klog" package

	if *flagInstall {
		// Simply install kernel in Jupyter configuration.
		var extraArgs []string
		if *flagExtraLog != "" {
			extraArgs = []string{"--extra_log", *flagExtraLog}
		}
		if glogFlag := flag.Lookup("vmodule"); glogFlag != nil && glogFlag.Value.String() != "" {
			extraArgs = append(extraArgs, fmt.Sprintf("--vmodule=%s", glogFlag.Value.String()))
		}
		if glogFlag := flag.Lookup("logtostderr"); glogFlag != nil && glogFlag.Value.String() != "false" {
			extraArgs = append(extraArgs, "--logtostderr")
		}
		if glogFlag := flag.Lookup("alsologtostderr"); glogFlag != nil && glogFlag.Value.String() != "false" {
			extraArgs = append(extraArgs, "--alsologtostderr")
		}
		if glogFlag := flag.Lookup("raw_error"); glogFlag != nil && glogFlag.Value.String() != "false" {
			extraArgs = append(extraArgs, "--raw_error")
		}
		err := kernel.Install(extraArgs, *flagForce)
		if err != nil {
			log.Fatalf("Installation failed: %+v\n", err)
		}
		return
	}

	if *flagKernel == "" {
		_, _ = fmt.Fprintf(os.Stderr, "Use either --install to install the kernel, or if started by Jupyter the flag --kernel must be provided.\n")
		flag.PrintDefaults()
		os.Exit(1)
	}

	gocoverdir := os.Getenv("GOCOVERDIR")
	if gocoverdir != "" {
		klog.Infof("GOCOVERDIR=%s", gocoverdir)
	}

	_, err := exec.LookPath("go")
	if err != nil {
		klog.Exitf("Failed to find path for the `go` program: %+v\n\nCurrent PATH=%q", err, os.Getenv("PATH"))
	}

	// Create a kernel.
	k, err := kernel.NewKernel(*flagKernel)
	klog.Infof("kernel created\n")
	if err != nil {
		log.Fatalf("Failed to start kernel: %+v", err)
	}
	k.HandleInterrupt() // Handle Jupyter interruptions and Control+C.

	// Create a Go executor.
	goExec, err := goexec.New(UniqueID, *flagRawError)
	if err != nil {
		log.Fatalf("Failed to create go executor: %+v", err)
	}

	// Orchestrate dispatching of messages.
	dispatcher.RunKernel(k, goExec)

	// Stop gopls.
	goExec.Stop()

	// Wait for all polling goroutines.
	k.ExitWait()
	klog.Infof("Exiting...")
}

var (
	ColorReset    = "\033[0m"
	ColorYellow   = "\033[33m"
	ColorBgYellow = "\033[7;39;32m"
)

// SetUpLogging creates a UniqueID, uses it as a prefix for logging, and sets up --extra_log if
// requested.
func SetUpLogging() {
	log.SetPrefix(coloredUniqueID)
	if *flagExtraLog != "" {
		f, err := os.OpenFile(*flagExtraLog, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0644)
		if err != nil {
			log.Fatalf("Failed to open log file %q for writing: %+v", *flagExtraLog, err)
		}
		_, _ = f.Write([]byte("\n\n"))
		w := io.MultiWriter(f, os.Stderr) // Write to STDERR and the newly open f.
		log.SetOutput(w)
	}
}

// UniqueIDFilter prepends the UniqueID for every log line.
type UniqueIDFilter struct{}

// prepend value to slice: it makes a copy of the slice.
func prepend[T any](slice []T, value T) []T {
	newSlice := make([]T, len(slice)+1)
	if len(slice) > 0 {
		copy(newSlice[1:], slice)
	}
	newSlice[0] = value
	return newSlice
}

// Filter implements klog.LogFilter interface.
func (_ UniqueIDFilter) Filter(args []interface{}) []interface{} {
	return prepend(args, any(coloredUniqueID))
}

// FilterF implements klog.LogFilter interface.
func (_ UniqueIDFilter) FilterF(format string, args []interface{}) (string, []interface{}) {
	return "%s" + format, prepend(args, any(coloredUniqueID))
}

// FilterS implements klog.LogFilter interface.
func (_ UniqueIDFilter) FilterS(msg string, keysAndValues []interface{}) (string, []interface{}) {
	return coloredUniqueID + msg, keysAndValues
}

// SetUpKlog to include prefix with kernel's UniqueID.
func SetUpKlog() {
	klog.SetLogFilter(UniqueIDFilter{})
}
