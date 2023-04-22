package main

import (
	"flag"
	"fmt"
	"github.com/gofrs/uuid"
	"github.com/janpfeifer/gonb/dispatcher"
	"github.com/janpfeifer/gonb/goexec"
	"github.com/janpfeifer/gonb/kernel"
	"io"
	"log"
	"os"
)

var (
	flagInstall  = flag.Bool("install", false, "Install kernel in local config, and make it available in Jupyter")
	flagKernel   = flag.String("kernel", "", "Run kernel using given path for the `connection_file` provided by Jupyter client")
	flagExtraLog = flag.String("extra_log", "", "Extra file to include in the log.")
	flagForce    = flag.Bool("force", false, "Force install even if goimports and/or gopls are missing.")
)

// UniqueID uniquely identifies a kernel execution. Used for logging and creating temporary directories.
// Set by SetUpLogging.
var UniqueID string

func main() {
	flag.Parse()
	SetUpLogging() // Also sets UniqueID

	if *flagInstall {
		// Simply install kernel in Jupyter configuration.
		var extraArgs []string
		if *flagExtraLog != "" {
			extraArgs = []string{"--extra_log", *flagExtraLog}
		}
		if glogFlag := flag.Lookup("vmodule"); glogFlag != nil && glogFlag.Value.String() != "" {
			extraArgs = append(extraArgs, "--vmodule", glogFlag.Value.String())
		}
		if glogFlag := flag.Lookup("logtostderr"); glogFlag != nil && glogFlag.Value.String() != "false" {
			extraArgs = append(extraArgs, "--logtostderr", glogFlag.Value.String())
		}
		if glogFlag := flag.Lookup("alsologtostderr"); glogFlag != nil && glogFlag.Value.String() != "false" {
			extraArgs = append(extraArgs, "--alsologtostderr", glogFlag.Value.String())
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

	// Create a kernel.
	k, err := kernel.NewKernel(*flagKernel)
	log.Printf("kernel created\n")
	if err != nil {
		log.Fatalf("Failed to start kernel: %+v", err)
	}
	k.HandleInterrupt() // Handle Jupyter interruptions and Control+C.

	// Create a Go executor.
	goExec, err := goexec.New(UniqueID)
	if err != nil {
		log.Fatalf("Failed to create go executor: %+v", err)
	}

	// Orchestrate dispatching of messages.
	dispatcher.RunKernel(k, goExec)

	// Wait for all polling goroutines.
	k.ExitWait()
	log.Printf("Exiting...")
}

var (
	ColorReset    = "\033[0m"
	ColorYellow   = "\033[33m"
	ColorBgYellow = "\033[7;39;32m"
)

// SetUpLogging creates a UniqueID, uses it as a prefix for logging, and sets up --extra_log if
// requested.
func SetUpLogging() {
	uuidTmp, _ := uuid.NewV7()
	uuidStr := uuidTmp.String()
	UniqueID = uuidStr[len(uuidStr)-8:]
	log.SetPrefix(fmt.Sprintf("%s[%s]%s ", ColorBgYellow, UniqueID, ColorReset))
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
