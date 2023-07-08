package goplsclient

import (
	"context"
	"k8s.io/klog/v2"
	"os"
	"os/exec"
	"strings"
	"time"

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

	if c.goplsExec != nil {
		// Already running.
		return nil
	}

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
	c.goplsExec.Dir = c.dir
	klog.Infof("Starting %s", c.goplsExec)
	err = c.goplsExec.Start()
	if err != nil {
		err = errors.Wrapf(err, "failed to start %s", c.goplsExec)
		c.goplsExec = nil
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
				klog.V(2).Infof("started `gopls`, but after %s it failed to connect, stopping it.", StartTimeout)
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

	// In parallel wait for command to finish.
	go func() {
		err := c.goplsExec.Wait()
		if err != nil {
			klog.Warningf("gopls failed with: %+v", err)
		} else {
			klog.V(2).Infof("gopls terminated normally.")
		}
		c.mu.Lock()
		defer c.mu.Unlock()
		c.goplsExec = nil
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

// WaitConnection checks whether connection is up, and if not tries connecting.
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

func (c *Client) stopLocked() {
	if c.goplsExec != nil {
		c.goplsExec.Process.Kill()
		c.goplsExec = nil
		c.removeUnixSocketFile()
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
