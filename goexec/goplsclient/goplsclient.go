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
	"io"
	"log"
	"net"
	"os"
	"os/exec"
	"path"
	"sync"
	"time"

	"github.com/go-language-server/jsonrpc2"
	lsp "github.com/go-language-server/protocol"
	"github.com/go-language-server/uri"
	"github.com/golang/glog"
	"github.com/pkg/errors"
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

	// Messages: they should be reset whenever they have been consumed.
	messages []string
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

// isGoInternalOrCache returns whether if file is from Go implementation or
// cached from a versioned library, in which case it's not expected to be changed,
// and we don't need to open the file in gopls.
// TODO: implement, for now we always open all files.
func isGoInternalOrCache(filePath string) bool {
	_ = filePath
	return false
}

// Definition return the definition for the identifier at the given position, rendered
// in Markdown. It returns empty if position has no identifier.
func (c *Client) Definition(ctx context.Context, filePath string, line, col int) (markdown string, err error) {
	glog.V(2).Infof("goplsclient.Definition(ctx, %s, %d, %d)", filePath, line, col)
	// Send "go.mod", in case it changes.
	err = c.NotifyDidOpenOrChange(ctx, path.Join(c.dir, "go.mod"))
	if err != nil {
		return "", err
	}
	// Send filePath.
	err = c.NotifyDidOpenOrChange(ctx, filePath)
	if err != nil {
		return "", err
	}

	var results []lsp.Location
	results, err = c.CallDefinition(ctx, filePath, line, col)
	if err != nil {
		log.Printf("c.CallDefinition failed: %+v", err)
		return "", err
	}
	_ = results
	for _, result := range results {
		if result.URI.Filename() != filePath && !isGoInternalOrCache(result.URI.Filename()) {
			err = c.NotifyDidOpenOrChange(ctx, result.URI.Filename())
			if err != nil {
				return "", err
			}
		}
	}
	hover, err := c.CallHover(ctx, filePath, line, col)
	if err != nil {
		log.Printf("c.CallHover failed: %+v", err)
		return "", err
	}
	if hover.Contents.Kind != lsp.Markdown {
		log.Printf("gopls returned 'hover' with unexpected kind %q", hover.Contents.Kind)
	}
	return hover.Contents.Value, nil
}

// Complete request auto-complete suggestions from `gopls`. It returns the text
// of the matches and the number of characters before the cursor position that should
// be replaced by the matches (the same value for every entry).
func (c *Client) Complete(ctx context.Context, filePath string, line, col int) (matches []string, replaceLength int, err error) {
	glog.V(2).Infof("goplsclient.Complete(ctx, %s, %d, %d)", filePath, line, col)
	err = c.NotifyDidOpenOrChange(ctx, filePath)
	if err != nil {
		return
	}
	var items *lsp.CompletionList
	items, err = c.CallComplete(ctx, filePath, line, col)
	if err != nil {
		return
	}
	if items == nil {
		// No results.
		return
	}
	replaceLength = -1
	for _, item := range items.Items {
		edit := item.TextEdit
		if edit == nil {
			continue
		}
		if int(edit.Range.End.Line) != line || int(edit.Range.End.Character) != col {
			// Not exactly a complement, so we drop -- don't know what to do.
			continue
		}
		if int(edit.Range.Start.Line) != line {
			// Multiple line edit we also don't know how to handle, skip.
			continue
		}
		newReplaceLength := int(edit.Range.End.Character) - int(edit.Range.Start.Character)
		if replaceLength != -1 && newReplaceLength != replaceLength {
			// Jupyter only supports edits of one length. We take the first one always.
			continue
		}
		replaceLength = newReplaceLength
		matches = append(matches, edit.NewText)
	}
	if len(items.Items) != len(matches) {
		log.Printf("Complete found %d items, used only %d", len(items.Items), len(matches))
	}
	return
}

// Span returns the text spanning the given location (`lsp.Location` represents a range).
func (c *Client) Span(loc lsp.Location) (string, error) {
	fileData, _, err := c.FileData(loc.URI.Filename())
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
	Path        string
	URI         uri.URI
	Content     string
	ContentTime time.Time
	LineStarts  []int
}

// FileData retrieves the file data, including its contents.
// It uses a cache system, so files don't need to be reloaded.
func (c *Client) FileData(filePath string) (content *FileData, updated bool, err error) {
	content, updated = c.fileCache[filePath]
	if updated {
		// Check file hasn't changed.
		var fileInfo os.FileInfo
		fileInfo, err = os.Stat(filePath)
		if err == nil && fileInfo.ModTime() == content.ContentTime {
			// Fine not changed.
			return
		}
		glog.V(2).Infof("File %q: stored date is %s, fileInfo mod time is %s",
			filePath, content.ContentTime, fileInfo.ModTime())
	}

	content = &FileData{
		URI:  uri.File(filePath),
		Path: filePath,
	}
	fileInfo, err := os.Stat(filePath)
	if err != nil {
		return nil, false, errors.Wrapf(err, "failed to stat %q for Client.FileData", filePath)
	}
	content.ContentTime = fileInfo.ModTime()
	var f *os.File
	f, err = os.Open(filePath)
	if err != nil {
		return nil, false, errors.Wrapf(err, "failed to open %q for Client.FileData", filePath)
	}
	var b []byte
	b, err = io.ReadAll(f)
	if err != nil {
		return nil, false, errors.Wrapf(err, "failed to read %q for Client.FileData", filePath)
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
	glog.V(2).Infof("goplsclient.FileData() loaded file %q", filePath)
	c.fileCache[filePath] = content
	return
}
