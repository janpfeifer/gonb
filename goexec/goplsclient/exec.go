package goplsclient

import (
	"context"
	"github.com/pkg/errors"
	"log"
	"os"
	"os/exec"
	"strings"
	"time"
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
	log.Printf("Starting %s", c.goplsExec)
	err = c.goplsExec.Start()
	if err != nil {
		err = errors.Wrapf(err, "failed to start %s", c.goplsExec)
		c.goplsExec = nil
		return err
	}

	c.waitConnecting = true

	// In parallel tries to connect.
	go func() {
		log.Printf("Polling connection with %s", c.goplsExec)
		onDeadline := time.After(StartTimeout)
		ctx := context.Background()
		for {
			// Wait a bit (for gopls to startup), and try to connect.
			select {
			case <-onDeadline:
				// Still failing to connect, kill job.
				log.Printf("started `gopls`, but after %s it failed to connect, stopping it.", StartTimeout)
				c.Stop()
				return
			case <-time.After(ConnectTimeout):
				// Wait before trying to connect (again)
			}
			log.Printf("* trying to connect to %s", c.Address())
			err := c.Connect(ctx)
			if err == nil {
				// Connected!
				c.waitConnecting = false
				return
			}
		}
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
		c.waitConnecting = false
		c.removeUnixSocketFile()
	}
	return
}

// removeUnixSocketFile if it exists.
func (c *Client) removeUnixSocketFile() {
	addr := c.Address()
	if strings.HasPrefix(addr, "unix;") {
		addr = addr[5:]
	}
	if strings.HasPrefix(addr, "/") {
		// Remove unix socket file, if it exists.
		os.Remove(addr)
	}
}
