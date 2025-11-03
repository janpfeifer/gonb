package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"slices"
	"strings"
	"time"

	"github.com/gofrs/uuid"
	"github.com/janpfeifer/gonb/internal/dispatcher"
	"github.com/janpfeifer/gonb/internal/goexec"
	"github.com/janpfeifer/gonb/internal/kernel"
	"github.com/janpfeifer/gonb/version"
	klog "k8s.io/klog/v2"
)

var (
	flagInstall      = flag.Bool("install", false, "Install kernel in local config, and make it available in Jupyter")
	flagKernel       = flag.String("kernel", "", "ProgramExecutor kernel using given path for the `connection_file` provided by Jupyter client")
	flagExtraLog     = flag.String("extra_log", "", "Extra file to include in the log.")
	flagForceDeps    = flag.Bool("force_deps", false, "Force install even if goimports and/or gopls are missing.")
	flagForceCopy    = flag.Bool("force_copy", false, "Copy binary to the Jupyter kernel configuration location. This already happens by default is the binary is under `/tmp`.")
	flagRawError     = flag.Bool("raw_error", false, "When GoNB executes cells, force raw text errors instead of HTML errors, which facilitates command line testing of notebooks.")
	flagWork         = flag.Bool("work", false, "Print name of temporary work directory and preserve it at exit. ")
	flagCommsLog     = flag.Bool("comms_log", false, "Enable verbose logging from communication library in Javascript console.")
	flagShortVersion = flag.Bool("V", false, "Print version information")
	flagLongVersion  = flag.Bool("version", false, "Print detailed version information")
	flagGoWork       = flag.String("go_work", "",
		"Comma-separated paths used to start a go.work file for any new notebook. "+
			"It can be used with -install and the flag will be configured in the kernel configuration so all new "+
			"notebooks will use this initial go.work file. It's not necessary to include \".\" in this path,"+
			"it will be added automatically.")
)

var (
	// UniqueID uniquely identifies a kernel execution. Used to create the temporary
	// directory holding the kernel code, and for logging.
	// Set by setUpLogging.
	UniqueID        string
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
	flag.Parse()
	defer klog.Flush()

	if len(flag.Args()) > 0 {
		_, _ = fmt.Fprintf(os.Stderr, "No extra arguments are allowed (passed %q). Use --help for more information.\n", flag.Args())
		os.Exit(1)
	}

	// Disable -logstderr if -extra_log is set.
	logtostderrFlag := flag.Lookup("logtostderr")
	if logtostderrFlag != nil && logtostderrFlag.Value.String() == "true" && *flagExtraLog != "" {
		logtostderrFlag.Value.Set("false")
		_, _ = fmt.Fprintf(os.Stderr, "Setting value of -logtostderr to false (default is true) because -extra_log is set. Did you meant to use -alsologtostderr instead ?\n")
	}

	// --version or -V
	if printVersion() {
		return
	}

	// One of two tasks: (1) install gonb; (2) run kernel, started by JupyterServer.
	if install() {
		return
	}
	if runKernel() {
		return
	}

	_, _ = fmt.Fprintf(os.Stderr, "Use either --install to install the kernel, or if started by Jupyter the flag --kernel must be provided.\n")
	flag.PrintDefaults()
	os.Exit(1)
	return
}

// install GoNB kernel as a JupyterServer configuration.
// Returns true if installed.
// Errors are fatal.
func install() bool {
	if !*flagInstall {
		return false
	}

	// Install kernel in Jupyter configuration.
	var extraArgs []string
	if *flagExtraLog != "" {
		extraArgs = append(extraArgs, fmt.Sprintf("-extra_log=%s", *flagExtraLog))
	}

	// Pass over flags to kernel execution.
	for _, flagName := range []string{
		"vmodule", "logtostderr", "alsologtostderr", "log_file", "log_dir", "raw_error", "work", "comms_log",
		"stderrthreshold", "v", "go_work"} {
		flagDef := flag.Lookup(flagName)
		if flagDef == nil {
			continue
		}
		extraArg := fmt.Sprintf("-%s=%s", flagDef.Name, flagDef.Value.String())
		extraArgs = append(extraArgs, extraArg)
	}
	err := kernel.Install(extraArgs, *flagForceDeps, *flagForceCopy)
	if err != nil {
		log.Fatalf("Installation failed: %+v\n", err)
	}
	return true
}

// printVersion returns whether version printing was requested.
func printVersion() bool {
	if *flagShortVersion {
		fmt.Println(version.AppVersion.String())
		return true
	} else if *flagLongVersion {
		version.AppVersion.Print()
		return true
	}
	return false
}

var (
	ColorReset    = "\033[0m"
	ColorYellow   = "\033[33m"
	ColorBgYellow = "\033[7;39;32m"
)

// setUpExtraLog
func setUpExtraLog() *os.File {
	if *flagExtraLog == "" {
		return nil
	}

	logFile, err := os.OpenFile(*flagExtraLog, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0644)
	if err != nil {
		klog.Fatalf("Failed to open log file %q for writing: %+v", *flagExtraLog, err)
	}
	_, _ = fmt.Fprintf(logFile, "\n\nLogging for %q (pid=%d) starting at %s\n", os.Args[0], os.Getpid(), time.Now())
	return logFile
}

// UniqueIDFilter prepends the UniqueID for every log line.
type UniqueIDFilter struct{}

// prepend value to slice.
func prepend[T any](slice []T, element T) []T {
	slice = append(slice, element) // It will be overwritten.
	copy(slice[1:], slice)         // Shift to the "right"
	slice[0] = element
	return slice
}

// Filter implements klog.LogFilter interface.
func (UniqueIDFilter) Filter(args []interface{}) []interface{} {
	return prepend(args, any(coloredUniqueID))
}

// FilterF implements klog.LogFilter interface.
func (UniqueIDFilter) FilterF(format string, args []interface{}) (string, []interface{}) {
	return "%s" + format, prepend(args, any(coloredUniqueID))
}

// FilterS implements klog.LogFilter interface.
func (UniqueIDFilter) FilterS(msg string, keysAndValues []interface{}) (string, []interface{}) {
	return coloredUniqueID + msg, keysAndValues
}

// setUpKlog to include prefix with kernel's UniqueID.
func setUpKlog(logWriter io.Writer) {
	log.SetPrefix(coloredUniqueID)
	klog.SetLogFilter(UniqueIDFilter{})
	if logWriter != nil {
		klog.Info("Logging to extra logger configured")
		klog.SetOutputBySeverity("INFO", logWriter)
		klog.Info("Info message")       // 1x in /tmp/gonb.out
		klog.Warning("Warning message") // 1x in /tmp/gonb.out
		klog.Error("Error message")     // 1x in /tmp/gonb.out
	}
}

// runKernel if --kernel is set. Returns whether the kernel was run.
// Errors are fatal.
func runKernel() bool {
	if *flagKernel == "" {
		return false
	}

	// Logging.
	extraLogFile := setUpExtraLog() // --extra_log.
	if extraLogFile != nil {
		defer func() { _ = extraLogFile.Close() }()
	}
	setUpKlog(extraLogFile)

	// Report about profiling test coverage.
	if gocoverdir, found := os.LookupEnv("GOCOVERDIR"); found {
		klog.Infof("GOCOVERDIR=%q", gocoverdir)
	}

	// Find Go path.
	_, err := exec.LookPath("go")
	if err != nil {
		klog.Exitf("Failed to find path for the `go` program: %+v\n\nCurrent PATH=%q", err, os.Getenv("PATH"))
	}

	// Create a kernel.
	k, err := kernel.New(*flagKernel)
	klog.Infof("kernel created\n")
	if err != nil {
		klog.Fatalf("Failed to start kernel: %+v", err)
	}
	k.HandleInterrupt() // Handle Jupyter interruptions and Control+C.

	// Create a Go executor.
	goExec, err := goexec.New(k, UniqueID, *flagWork, *flagRawError)
	if err != nil {
		klog.Fatalf("Failed to create go executor: %+v", err)
	}
	goExec.Comms.LogWebSocket = *flagCommsLog

	// Initialize go.work if -go_work flag was given.
	if *flagGoWork != "" {
		paths := strings.Split(*flagGoWork, ",")
		paths = slices.DeleteFunc(paths, func(path string) bool { return path == "" })
		if !slices.Contains(paths, ".") {
			paths = append(paths, ".")
		}
		err = goExec.InitializeGoWork(paths)
		if err != nil {
			klog.Fatalf("Failed to initialize go.work: %+v", err)
		}
	}

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
	return true
}
