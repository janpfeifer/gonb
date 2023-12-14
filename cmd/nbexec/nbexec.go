package main

import (
	"errors"
	"flag"
	"fmt"
	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"
	"github.com/janpfeifer/gonb/common"
	"github.com/janpfeifer/must"
	"io"
	"k8s.io/klog/v2"
	"net"
	"os"
	"os/exec"
	"path"
	"regexp"
	"strings"
	"time"
)

var (
	// panicf alias.
	panicf = common.Panicf

	// Extra arguments passed to `jupyter notebook` -- can be set multiple times.
	flagJupyterExtraFlags = common.ArrayFlag{}

	flagNotebook = flag.String("n", "", "Notebook to execute. Must be a relative path "+
		"to the current directory (or --jupyter_dir if set) and cannot use \"..\", since it is going to "+
		"be used in an URL.")
	flagJupyterDir = flag.String("jupyter_dir", "",
		"Directory where to execute `jupyter notebook`. If empty, it will use the current directory. "+
			"The notebook path is relative to this directory.")
	flagJupyterLog = flag.Bool("jupyter_log", false,
		"Set to true and it will output to stdout everything output by `jupyter notebook`")

	flagConsoleLog = flag.Bool("console_log", false,
		"Set to true to capture and output what is written in the "+
			"browser's console, using `console.log()` in javascript.")

	flagScreenshot = flag.String("screenshot", "",
		"PNG file where to save a screenshot of the page after the notebook is executed. "+
			"If left empty no screenshots are made.")

	flagClear = flag.Bool("clear", false,
		"If set to true, it will only clear all the cell outputs and not execute them.")

	flagJupyterInputs = flag.String("input_boxes", "",
		"List of inputs to feed input boxes created for Stdin in Jupyter. In GoNB these "+
			"are created by `%with_input` special command and with `gonbui.RequestInput()`. The "+
			"comma-separated list of values will be fed everytime such an input box is created. ")
)

func main() {
	klog.InitFlags(nil)
	defer klog.Flush()

	flag.Var(&flagJupyterExtraFlags, "jupyter_args",
		"Extra arguments passed to `jupyter notebook` -- can be set multiple times. "+
			"`nbexec` always sets the following arguments: "+
			"`--no-browser --expose-app-in-browser --ServerApp.port=<port>`.")
	flag.Parse()
	klog.V(1).Info("Starting")

	// Sanity checking.
	if strings.Index(*flagNotebook, "/../") >= 0 ||
		strings.Index(*flagNotebook, "../") == 0 ||
		strings.Index(*flagNotebook, "/..") == len(*flagNotebook)-3 {
		klog.Fatalf("Invalid value for notebook -n=%q: cannot use \"..\" in path")
	}
	notebookPath := *flagJupyterDir
	if notebookPath == "" {
		notebookPath = must.M1(os.Getwd())
	}
	notebookPath = path.Join(notebookPath, *flagNotebook)
	if _, err := os.Stat(notebookPath); err != nil {
		klog.Fatalf("The notebook given -n=%q resolves to %q, and I can't access it!? Error: %v")
	}

	// Values for input boxes.
	var inputBoxes []string
	if *flagJupyterInputs != "" {
		inputBoxes = strings.Split(*flagJupyterInputs, ",")
	}

	// Start Jupyter Notebook
	startJupyterNotebook()
	if jupyterDone == nil || jupyterDone.Test() {
		klog.Errorf("The program `jupyter notebook` failed to run, notebook was not executed!")
		return
	}

	go func() {
		// Start headless chromium with go-rod.
		jupyterReady.Wait()
		url := fmt.Sprintf("%snotebooks/%s?token=%s", jupyterBaseUrl, *flagNotebook, jupyterToken)
		klog.V(1).Infof("URL: %s", url)
		defer func() {
			e := recover()
			if !jupyterDone.Test() {
				jupyterStop.Trigger()
				if e != nil {
					// Wait for jupyter notebook to get killed, before re-throwing the panic.
					klog.Errorf("%v", e)
					jupyterDone.Wait()
					panic(e)
				}
			} else if e != nil {
				panic(e)
			}
		}()
		executeNotebook(url, inputBoxes)
	}()

	// Wait for `jupyter notebook` to finish.
	jupyterDone.Wait()
}

// Jupyter Notebook execution related variables.
var (
	jupyterCmd                             *exec.Cmd
	jupyterExecPath                        string
	jupyterReady, jupyterDone, jupyterStop *common.Latch

	jupyterPort                  int
	jupyterBaseUrl, jupyterToken string
)

// startJupyterNotebook will start Jupyter Notebook. It panics if something goes wrong.
func startJupyterNotebook() {
	// Find jupyter executable.
	var err error
	jupyterExecPath, err = exec.LookPath("jupyter")
	if err != nil {
		panicf(
			"Command `jupyter` is not in path. `nbexec` needs access the `jupyter` binary -- and `notebook` "+
				"subcommand installed (usually with something like `pip install notebook`, or something similar "+
				"if using Conda. Error: %v", err)
	}
	klog.Infof("jupyter: found in %q", jupyterExecPath)

	// Find an available port -- we could set the port to 0 and `jupyter notebook` would also take a random port,
	// but unfortunately it won't report it.
	l, err := net.Listen("tcp", ":0")
	if err != nil {
		klog.Errorf("Failed to get a random port available to use for jupyter notebook: %v", err)
		return
	}
	jupyterPort = l.Addr().(*net.TCPAddr).Port
	if err = l.Close(); err != nil {
		klog.Errorf("Failed to close random port %d, temporarily opened to be used for jupyter notebook: %v", jupyterPort, err)
		return
	}

	// Execute `jupyter notebook`.
	args := []string{"notebook", "--no-browser",
		fmt.Sprintf("--ServerApp.port=%d", jupyterPort),

		// Allow access to `jupyterapp` javascript object, needed to
		// command the notebook to execute all cells and save.
		"--expose-app-in-browser",
		"--ServerApp.allow_remote_access=false"}
	for _, arg := range flagJupyterExtraFlags {
		args = append(args, arg)
	}
	jupyterCmd = exec.Command(jupyterExecPath, args...)
	if *flagJupyterDir != "" {
		jupyterCmd.Dir = *flagJupyterDir
	}

	// Latches to report state of `jupyter notebook`
	jupyterReady = common.NewLatch()
	jupyterDone = common.NewLatch()
	jupyterStop = common.NewLatch()

	// Parses the output of jupyter for indication of Jupyters' readiness.
	jpyOut := must.M1(jupyterCmd.StdoutPipe())
	jpyErr := must.M1(jupyterCmd.StderrPipe())
	const bufSize = 1024 * 1024
	reJupyterAddress := regexp.MustCompile(
		`\s+(http://127\.0\.0\.1:\d+/)tree\?token=(\w+)`)
	//`Jupyter Server is listening on (http\+unix:.*\.sock/)tree\?token=(\w+)`)
	pollingFn := func(reader io.Reader) {
		var err error
		var n int
		for {
			buf := make([]byte, bufSize)
			n, err = reader.Read(buf)
			if err != nil {
				break
			}
			if *flagJupyterLog {
				//fmt.Printf("[%q]\n", string(buf[:n]))
				_, _ = os.Stdout.Write(buf[:n])
			}
			if !jupyterReady.Test() {
				matches := reJupyterAddress.FindStringSubmatch(string(buf[:n]))
				if len(matches) > 0 {
					// We got the serving address.
					klog.V(2).Infof("matches=%q", matches)
					jupyterBaseUrl, jupyterToken = matches[1], matches[2]
					klog.V(1).Infof("jupyter notebook: url=%q, token=%q", jupyterBaseUrl, jupyterToken)
					jupyterReady.Trigger()
				}
			}
		}
		if !errors.Is(err, os.ErrClosed) && !errors.Is(err, io.EOF) {
			klog.Errorf("Failed to read output of `jupyter notebook`: %v", err)
		}
	}
	go pollingFn(jpyOut)
	go pollingFn(jpyErr)

	// Listen to jupyterStop and interrupts the program, if not yet finished.
	go func() {
		select {
		case <-jupyterDone.WaitChan():
			// Program finished, no need to stop it.
			return
		case <-jupyterStop.WaitChan():
			// Continue to stop the program.
		}
		klog.Infof("Stopping Jupyter Notebook")
		stopCmd := exec.Command(jupyterExecPath, "notebook",
			"stop", fmt.Sprintf("%d", jupyterPort))
		err := stopCmd.Run()
		if err != nil {
			klog.Errorf("Failed to stop jupyter notebook with %q: %v", stopCmd, err)
		}

		// Wait a few seconds, if notebooks doesn't die, kill it.
		select {
		case <-time.After(5 * time.Second):
			klog.Errorf("Clean exit of `jupyter notebook` timed-out, killing it.")
			err = jupyterCmd.Process.Kill()
			if err != nil {
				klog.Errorf("Killing (stopping) `jupyter notebook` failed!? Error: %v", err)
			}
		case <-jupyterDone.WaitChan():
			// Clean exit.
		}
	}()

	// Wait for the program to exit and trigger jupyterDone.
	if err := jupyterCmd.Start(); err != nil {
		jupyterDone.Trigger()
		klog.Errorf("Failed to run %q: %v", jupyterCmd, err)
		return
	}
	klog.Infof("Running: %s", jupyterCmd)
	go func() {
		err := jupyterCmd.Wait()
		if err != nil && !jupyterStop.Test() {
			klog.Errorf("`jupyter notebook` died, likely something failed. Consider re-running with --jupyter_log "+
				"to output the contents of jupyter notebook. Error: %v", err)
		}
		jupyterDone.Trigger()
	}()

	select {
	case <-jupyterDone.WaitChan():
	case <-jupyterReady.WaitChan():
		// Jupyter Notebook running, just continue below.
	}
}

// Notebook instrumenting using go-rod.

func executeNotebook(url string, inputBoxes []string) {
	page := rod.New().MustConnect().MustPage(url)
	klog.V(1).Infof("Waiting for opening of page %q", url)
	page.MustWaitStable()

	if *flagConsoleLog {
		// Listen for all events of console output.
		go page.EachEvent(func(e *proto.RuntimeConsoleAPICalled) {
			fmt.Printf("[console] %q\n", page.MustObjectsToJSON(e.Args))
		})()
	}

	klog.V(1).Info("Executing clear-all")
	page.MustEval(`() => {
	let jupyterapp = globalThis.jupyterapp;
	jupyterapp.commands.execute("editmenu:clear-all", null).
		then(() => { console.log("Finished clearing all outputs!"); });
}`)
	page.MustWaitStable()

	if !*flagClear {
		// Execute all cells (if --clear is not set)
		klog.V(1).Info("Executing run-all")
		page.MustEval(`() => {
	let jupyterapp = globalThis.jupyterapp;
	jupyterapp.commands.execute("runmenu:run-all", null).
		then(() => { console.log("Finished executing!"); });
}`)
		klog.V(1).Infof("Started Javascript to execute cells.")
		checkForInputBoxes(page, inputBoxes)
		klog.V(1).Infof("Notebook execution finished.")
	}

	page.MustEval(`() => {
	let jupyterapp = globalThis.jupyterapp;
	jupyterapp.commands.execute("docmanager:save", null).
		then(() => { console.log("Finished saving!"); });
}`)
	klog.V(1).Infof("Started Javascript to save notebook, waiting to stabilize.")
	page.MustWaitStable()
	klog.V(1).Infof("page.MustWaitStable() finished.")

	if *flagScreenshot != "" {
		klog.Infof("Screenshot to %q", *flagScreenshot)
		page.MustScreenshot(*flagScreenshot)
	}

	//common.Pause()
}

const maxInputBoxes = 20 // After those many repeats, give up.

// checkForInputBoxes checks whether an input box is created, and if created, feed
// the next value in `valuesToFeed`, or an empty value if no more values are given.
//
// Input boxes are created by the `%with_inputs` special command or with `gonbui.RequestInput()`
func checkForInputBoxes(page *rod.Page, valuesToFeed []string) {
	// Concurrently check for page being done.
	done := common.NewLatch()
	go func() {
		defer done.Trigger()
		page.MustWaitStable()
	}()

	// Poll for input boxes.
	for count := maxInputBoxes; count > 0; count-- {

		var nextInputValue string
		if len(valuesToFeed) > 0 {
			nextInputValue = valuesToFeed[0]
			valuesToFeed = valuesToFeed[1:]
		}

		inputBoxEntered := common.NewLatch()
		go func() {
			for {
				// Interrupt if already done.
				if done.Test() {
					return
				}
				time.Sleep(time.Second) // Poll every second.
				klog.V(1).Infof("Checking for input boxes")
				if page.MustEval(fmt.Sprintf(`() => {
	let inputBox = globalThis.document.querySelector('input.jp-Stdin-input');
	if (inputBox === null) {
		return false;
	} else {
		inputBox.value = %q;
		let enterEvent = new Event('keydown');
		enterEvent.keycode=13;
		enterEvent.key="Enter";
		inputBox.dispatchEvent(enterEvent)
		console.log("Found input box!");
		return true;
	}
}`, nextInputValue)).Bool() {
					// Input box filled successfully.
					klog.V(1).Infof("Value %q filled in input box", nextInputValue)
					inputBoxEntered.Trigger()
					return
				}
			}
		}() // Concurrently poll for input boxes.

		select {
		case <-done.WaitChan():
			// Page finished.
			return
		case <-inputBoxEntered.WaitChan():
			// Input box value fed, simply continue the loop.
		}
	}
	panicf("Max number of input box values %d fed, assuming an infinite loop, and stopping!", maxInputBoxes)
}
