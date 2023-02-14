// Package goplsclient runs `gopls` (1) in the background uses it to
// retrieve definitions of symbols and auto-complete.
//
// How to use it:
//
//  1. Construct a `*Client` with `New()`
//     It will start it, connect and initialize in the background.
//  2. Call the various services: currently only `Definition()`.
//  3. Cache of files that needed retrieving to access definitions.
//
// `gopls` runs a [Language Server Protocol](https://microsoft.github.io/language-server-protocol/overviews/lsp/overview/)
// and it's tricky to get right. Much of the communication seems to be asynchronous
// (Notify messages) and lots are just dropped for now.
//
// TODO: current implementation is as simple as it can be. No concurrency control is included.
//
// (1) https://github.com/golang/tools/tree/master/gopls
package goplsclient

import (
	"context"
	jsonrpc2 "github.com/go-language-server/jsonrpc2"
	lsp "github.com/go-language-server/protocol"
	"github.com/go-language-server/uri"
	"github.com/pkg/errors"
	"io"
	"log"
	"net"
	"os"
	"os/exec"
	"path"
	"sync"
)

type Client struct {
	dir     string // directory with contents.
	address string // where to connect to `gopls`.

	// Guard server state.
	mu sync.Mutex

	// Connection attributes.
	conn            net.Conn
	jsonConn        *jsonrpc2.Conn
	jsonHandler     *jsonrpc2Handler
	lspCapabilities lsp.ServerCapabilities

	// gopls execution
	goplsExec      *exec.Cmd
	waitConnecting bool

	// File cache.
	fileVersions map[string]int       // Every open file that has been sent to gopls has a version, that is bumped when it is sent again.
	fileCache    map[string]*FileData // Cache of files stored in disk.
}

// New returns a new Client in the directory. The returned Client does not yet start
// a `gopls` instance or connects to one. It should be followed by a call to
// `Start()` to start a new `gopls` or `Connect()` to connect to an existing `gopls`
// server.
//
//   - dir: directory to be monitored, typically where the `go.mod` of the project we are
//     monitoring resides (assuming there are only one module of interest).
func New(dir string) *Client {
	c := &Client{
		dir:          dir,
		address:      path.Join(dir, "gopls_socket"),
		fileVersions: make(map[string]int),
		fileCache:    make(map[string]*FileData),
	}
	return c
}

// Address used either to start `gopls` or to connect to it.
func (c *Client) Address() string { return c.address }

// SetAddress to be used either to start `gopls` or to connect to it.
// If the address is empty, it defaults to a unix socket configured as
// `dir+"/gopls_socket".
//
// This may have no effect if `gopls` is already started or connectingLatch to.
func (c *Client) SetAddress(address string) {
	c.address = address
}

// Shutdown closes connection and stops `gopls` (if connectingLatch/started).
func (c *Client) Shutdown() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.connCloseLocked()
	c.stopLocked()
}

// FileData retrieves the file data, including its contents.
// It uses a cache system, so files don't need to be reloaded.
func (c *Client) FileData(filePath string) (content *FileData, err error) {
	var found bool
	content, found = c.fileCache[filePath]
	if found {
		return
	}

	content = &FileData{
		URI:  uri.File(filePath),
		Path: filePath,
	}
	var f *os.File
	f, err = os.Open(filePath)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to open %q for Client.FileData", filePath)
	}
	var b []byte
	b, err = io.ReadAll(f)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to read %q for Client.FileData", filePath)
	}
	content.Content = string(b)
	if len(content.Content) == 0 {
		return
	}

	// Fine line splits.
	numLines := 1
	for ii := 0; ii < len(content.Content)-1; ii++ {
		if content.Content[ii] == '\n' {
			numLines++
		}
	}
	content.LineStarts = make([]int, numLines)
	numLines = 1
	for ii := 0; ii < len(content.Content)-1; ii++ {
		if content.Content[ii] == '\n' {
			content.LineStarts[numLines] = ii + 1
			numLines++
		}
	}
	return
}

// Definition return the definition for the identifier at the given position, rendered
// in Markdown. It returns empty if position has no identifier.
func (c *Client) Definition(ctx context.Context, filePath string, line, col int) (markdown string, err error) {
	err = c.NotifyDidOpen(ctx, filePath)
	if err != nil {
		return "", err
	}
	_, err = c.CallDefinition(ctx, filePath, line, col)
	if err != nil {
		return "", err
	}
	hover, err := c.CallHover(ctx, filePath, line, col)
	if err != nil {
		return "", err
	}
	if hover.Contents.Kind != lsp.Markdown {
		log.Printf("gopls returned 'hover' with unexpected kind %q", hover.Contents.Kind)
	}
	return hover.Contents.Value, nil
}

// Span returns the text spanning the given location (`lsp.Location` represents a range).
func (c *Client) Span(loc lsp.Location) (string, error) {
	fileData, err := c.FileData(loc.URI.Filename())
	if err != nil {
		return "", err
	}

	start := fileData.LineStarts[int(loc.Range.Start.Line)] + int(loc.Range.Start.Character)
	end := fileData.LineStarts[int(loc.Range.End.Line)] + int(loc.Range.End.Character)
	if end < start || start > len(fileData.Content) {
		return "", nil
	}
	if end > len(fileData.Content) {
		end = len(fileData.Content)
	}
	return fileData.Content[start:end], nil
}

// FileData holds information about the contents of a file. It's built by `Client.FileContents`.
type FileData struct {
	Path       string
	URI        uri.URI
	Content    string
	LineStarts []int
}
