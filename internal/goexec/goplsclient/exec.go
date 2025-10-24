package goplsclient

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"github.com/janpfeifer/gonb/internal/util"
	"k8s.io/klog/v2"

	"github.com/pkg/errors"
)

// This file implements the management of (re-)starting `gopls`
// as a server for the duration of the kernel.

var StartTimeout = 5 * time.Second

// Start `gopls` as a server, on `Client.Address()` port. It is started
// asynchronously (so `Start()` returns immediately) and is followed up
// by automatically connecting to it.
//
// While it is not started the various services return empty results.
func (c *Client) Start() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	klog.Infof("gopls.Client.Start()")
	if !c.IsStopped() {
		klog.Errorf("attempting to start gopls, but it is still running")
		return nil
	}

	c.stop = make(chan struct{})
	c.removeUnixSocketFile()
	goplsPath, err := exec.LookPath("gopls")
	if err != nil {
		return errors.Wrapf(err, "cannot file `gopls` binary in path")
	}

	addr := c.Address()
	if strings.HasPrefix(addr, "/") {
		addr = "unix;" + addr
	}
	c.goplsExec = exec.Command(goplsPath, "-listen", addr)

	// Start on its own process group, to avoid receiving the `sigint` that
	// the kernel receives from Jupyter and dying.
	// Not sure on the status of MacOS:
	// https://stackoverflow.com/questions/43364958/start-command-with-new-process-group-id-golang
	c.goplsExec.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pgid: 0}
	c.goplsExec.Dir = c.dir
	klog.Infof("Executing %q", c.goplsExec)
	err = c.goplsExec.Start()
	if err != nil {
		err = errors.Wrapf(err, "failed to start %s", c.goplsExec)
		close(c.stop)
		return err
	}

	c.waitConnecting = true

	// In parallel tries to connect.
	go func() {
		klog.V(2).Infof("Polling connection with %s", c.goplsExec)
		onDeadline := time.After(StartTimeout)
		ctx := context.Background()
		for {
			// Wait a bit (for gopls to startup), and try to connect.
			select {
			case <-onDeadline:
				// Still failing to connect, kill job.
				klog.Errorf("started `gopls`, but after %s it failed to connect, stopping it.", StartTimeout)
				c.Stop()
				return
			case <-time.After(ConnectTimeout):
				// Wait before trying to connect (again)
			}
			klog.V(2).Infof("* trying to connect to %s", c.Address())
			err := c.Connect(ctx)
			if err == nil {
				// Connected!
				c.waitConnecting = false
				return
			}
		}
	}()

	// In parallel wait for the command to finish.
	go func() {
		err := c.goplsExec.Wait()
		if err != nil {
			klog.Warningf("gopls failed with: %+v", err)
		} else {
			klog.V(2).Infof("gopls terminated normally.")
		}
		c.mu.Lock()
		defer c.mu.Unlock()
		close(c.stop)
		c.removeUnixSocketFile()
		c.stopLocked()
	}()

	return nil
}

// Stop current `gopls` execution.
func (c *Client) Stop() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.stopLocked()
}

// WaitConnection checks whether a connection is up, and if it is not, tries connecting.
// Returns true if good to proceed.
func (c *Client) WaitConnection(ctx context.Context) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	for {
		if c.conn != nil {
			return true
		}
		if !c.waitConnecting {
			return false
		}

		// Wait before checking again.
		c.mu.Unlock()
		<-time.After(ConnectTimeout)
		c.mu.Lock()
	}
}

func (c *Client) IsStopped() bool {
	if c.stop == nil {
		return true
	}
	select {
	case <-c.stop:
		return true
	default:
		return false
	}
}

func (c *Client) stopLocked() {
	if !c.IsStopped() {
		klog.Infof("killing gopls: \n%s", util.GetStackTrace())
		util.ReportError(c.goplsExec.Process.Kill())
		c.removeUnixSocketFile()
		// Client will be marked as stopped once the gopls process exits.
	}
	c.waitConnecting = false
	return
}

// removeUnixSocketFile if it exists.
func (c *Client) removeUnixSocketFile() {
	addr := c.Address()
	if strings.HasPrefix(addr, "unix;") {
		addr = addr[5:]
	}
	if strings.HasPrefix(addr, "/") {
		klog.V(2).Infof("Removing %s", addr)
		// Remove unix socket file, if it exists -- we ignore any errors.
		_ = os.Remove(addr)
	}
}
