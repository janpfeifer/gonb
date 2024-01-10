package main

import (
	"flag"
	"fmt"
	"github.com/gofrs/uuid"
	"github.com/janpfeifer/gonb/internal/dispatcher"
	"github.com/janpfeifer/gonb/internal/goexec"
	"github.com/janpfeifer/gonb/internal/kernel"
	"io"
	klog "k8s.io/klog/v2"
	"log"
	"os"
	"os/exec"
	"time"
)

var (
	flagInstall   = flag.Bool("install", false, "Install kernel in local config, and make it available in Jupyter")
	flagKernel    = flag.String("kernel", "", "ProgramExecutor kernel using given path for the `connection_file` provided by Jupyter client")
	flagExtraLog  = flag.String("extra_log", "", "Extra file to include in the log.")
	flagForceDeps = flag.Bool("force_deps", false, "Force install even if goimports and/or gopls are missing.")
	flagForceCopy = flag.Bool("force_copy", false, "Copy binary to the Jupyter kernel configuration location. This already happens by default is the binary is under `/tmp`.")
	flagRawError  = flag.Bool("raw_error", false, "When GoNB executes cells, force raw text errors instead of HTML errors, which facilitates command line testing of notebooks.")
	flagWork      = flag.Bool("work", false, "Print name of temporary work directory and preserve it at exit. ")
	flagCommsLog  = flag.Bool("comms_log", false, "Enable verbose logging from communication library in Javascript console.")
)

var (
	// UniqueID uniquely identifies a kernel execution. Used to create the temporary
	// directory holding the kernel code, and for logging.
	// Set by SetUpLogging.
	UniqueID string

	coloredUniqueID string

	logWriter io.Writer
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

	// Setup logging.
	if *flagExtraLog != "" {
		logFile, err := os.OpenFile(*flagExtraLog, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0644)
		if err != nil {
			klog.Fatalf("Failed to open log file %q for writing: %+v", *flagExtraLog, err)
		}
		_, _ = fmt.Fprintf(logFile, "\n\nLogging for %q (pid=%d) starting at %s\n\n", os.Args[0], os.Getpid(), time.Now())
		logWriter = logFile
		flag.VisitAll(func(f *flag.Flag) {
			if f.Name == "logtostderr" || f.Name == "alsologtostderr" {
				if f.Value.String() == "true" {
					logWriter = io.MultiWriter(logFile, os.Stderr) // Write to STDERR and the newly open logFile.
					_ = f.Value.Set("false")
				}
			}
		})
		defer func() { _ = logFile.Close() }()
	}
	SetUpLogging() // "log" package.
	SetUpKlog()    // "github.com/golang/klog" package

	if *flagInstall {
		// Install kernel in Jupyter configuration.
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
		if glogFlag := flag.Lookup("work"); glogFlag != nil && glogFlag.Value.String() != "false" {
			extraArgs = append(extraArgs, "--work")
		}
		if glogFlag := flag.Lookup("comms_log"); glogFlag != nil && glogFlag.Value.String() != "false" {
			extraArgs = append(extraArgs, "--comms_log")
		}
		err := kernel.Install(extraArgs, *flagForceDeps, *flagForceCopy)
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
	k, err := kernel.New(*flagKernel)
	klog.Infof("kernel created\n")
	if err != nil {
		log.Fatalf("Failed to start kernel: %+v", err)
	}
	k.HandleInterrupt() // Handle Jupyter interruptions and Control+C.

	// Create a Go executor.
	goExec, err := goexec.New(k, UniqueID, *flagWork, *flagRawError)
	if err != nil {
		log.Fatalf("Failed to create go executor: %+v", err)
	}
	goExec.Comms.LogWebSocket = *flagCommsLog

	// Orchestrate dispatching of messages.
	dispatcher.RunKernel(k, goExec)
	klog.V(1).Infof("Dispatcher exited.")

	// Stop gopls.
	err = goExec.Stop()
	if err != nil {
		klog.Warningf("Error during shutdown: %+v", err)
	}
	klog.V(1).Infof("goExec stopped.")

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
	if logWriter != nil {
		log.SetOutput(logWriter)
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
	if logWriter != nil {
		klog.SetOutput(logWriter)
	}
	klog.SetLogFilter(UniqueIDFilter{})
}
