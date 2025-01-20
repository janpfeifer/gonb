package goplsclient

import (
	"context"
	"encoding/json"
	"fmt"
	"k8s.io/klog/v2"
	"net"
	"path"
	"strings"
	"time"

	"github.com/pkg/errors"
	"go.lsp.dev/jsonrpc2"
	lsp "go.lsp.dev/protocol"
	"go.lsp.dev/uri"
)

var _ = lsp.MethodInitialize

var (
	ConnectTimeout       = 2000 * time.Millisecond
	CommunicationTimeout = 2000 * time.Millisecond
)

func (c *Client) ConnClose() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.connCloseLocked()
}

// connCloseLocked assumes Client.mu lock is acquired.
func (c *Client) connCloseLocked() {
	// Wait for any pending concurrent connection attempt.
	if c.conn != nil {
		_ = c.conn.Close()
		c.conn = nil
	}
}

// minTimeout extends (or returns a new context with extended Deadline), discarding the
// previous one -- not the correct use of context, but will do for now.
func minTimeout(ctx context.Context, timeout time.Duration) context.Context {
	minDeadline := time.Now().Add(timeout)
	if deadline, ok := ctx.Deadline(); !ok || deadline.After(minDeadline) {
		ctx, _ = context.WithDeadline(ctx, minDeadline)
	}
	return ctx
}

// Connect to the `gopls` in address given by `c.Address()`. It also starts
// a goroutine to monitor receiving requests.
func (c *Client) Connect(ctx context.Context) error {
	ctx = minTimeout(ctx, ConnectTimeout)
	c.mu.Lock()
	defer c.mu.Unlock()

	// Already connected ?
	if c.conn != nil {
		return nil
	}

	netMethod := "tcp"
	addr := c.address
	if strings.HasPrefix(addr, "/") {
		netMethod = "unix"
	} else if strings.HasPrefix(addr, "unix;") {
		netMethod = "unix"
		addr = addr[5:]
	}
	var err error
	c.conn, err = net.DialTimeout(netMethod, addr, ConnectTimeout)
	if err != nil {
		c.conn = nil
		return errors.Wrapf(err, "failed to connect to gopls in %q", addr)
	}

	jsonStream := jsonrpc2.NewStream(c.conn)
	c.jsonConn = jsonrpc2.NewConn(jsonStream)
	c.jsonConn.Go(context.Background(), c.Handler)
	go func(currentConn net.Conn) {
		// ProgramExecutor should use a non-expiring context.
		<-c.jsonConn.Done()
		klog.Infof("- gopls connection stopped")
		c.mu.Lock()
		defer c.mu.Unlock()
		if c.conn == currentConn {
			c.connCloseLocked()
		}
	}(c.conn)

	callId, err := c.jsonConn.Call(ctx, lsp.MethodInitialize, &lsp.InitializeParams{
		ProcessID: 0,
		// Capabilities:          lsp.ClientCapabilities{},
		WorkspaceFolders: []lsp.WorkspaceFolder{
			lsp.WorkspaceFolder{
				URI:  string(uri.File(c.dir)),
				Name: path.Base(c.dir),
			},
		},
	}, &c.lspCapabilities)
	_ = callId // Not used now.
	if err != nil {
		if closeErr := c.conn.Close(); closeErr != nil {
			klog.Errorf("Failed to close connection: %+v", closeErr)
		}
		c.conn = nil
		return errors.Wrapf(err, "failed \"initialize\" call to gopls in %q", addr)
	}

	err = c.jsonConn.Notify(ctx, lsp.MethodInitialized, &lsp.InitializedParams{})
	if err != nil {
		if closeErr := c.conn.Close(); closeErr != nil {
			klog.Errorf("Failed to close connection: %+v", closeErr)
		}
		c.conn = nil
		return errors.Wrapf(err, "failed \"initialized\" notification to gopls in %q", addr)
	}
	return nil
}

// NotifyDidOpenOrChange sends a notification to `gopls` to open or change a file, which also sends the
// file content (from `Client.fileCache` if available). It also handles the case when a file
// gets deleted.
//
// File version sent is incremented.
func (c *Client) NotifyDidOpenOrChange(ctx context.Context, filePath string) (err error) {
	if !c.WaitConnection(ctx) {
		// Silently do nothing, if no connection available.
		return
	}
	ctx = minTimeout(ctx, CommunicationTimeout)
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn == nil {
		return
	}
	return c.notifyDidOpenOrChangeLocked(ctx, filePath)
}

func (c *Client) notifyDidOpenOrChangeLocked(ctx context.Context, filePath string) (err error) {
	var fileData *FileData
	var fileUpdated bool
	fileData, fileUpdated, err = c.FileData(filePath)
	if err != nil {
		return err
	}
	if !fileUpdated {
		klog.V(2).Infof("goplsclient.NotifyDidOpenOrChange(ctx, %q) -- no updates", filePath)
		return
	}

	// If file got deleted since last time.
	if fileData == nil {
		klog.V(2).Infof("goplsclient.NotifyDidOpenOrChange(ctx, %q) -- file deleted", filePath)
		delete(c.fileVersions, filePath)
		params := &lsp.DidCloseTextDocumentParams{
			TextDocument: lsp.TextDocumentIdentifier{
				URI: uri.File(filePath),
			},
		}
		err = c.jsonConn.Notify(ctx, lsp.MethodTextDocumentDidClose, params)
		if err != nil {
			fmt.Printf("\n\n\n*** FAILED MethodTextDocumentDidClose ***\n")
			err = errors.Wrapf(err, "Failed Client.MethodTextDocumentDidClose notification for %q", filePath)
		}
		return
	}

	// Update version counter.
	fileVersion, previouslyOpened := c.fileVersions[filePath]
	if previouslyOpened && !fileUpdated {
		// File already opened, and it hasn't changed, nothing to do.
		return nil
	}
	fileVersion += 1
	c.fileVersions[filePath] = fileVersion

	if !previouslyOpened {
		// Notify opening a file not previously tracked.
		klog.V(2).Infof("goplsclient.NotifyDidOpenOrChange(ctx, %s) -- file opened", fileData.URI)
		params := &lsp.DidOpenTextDocumentParams{
			TextDocument: lsp.TextDocumentItem{
				URI:        fileData.URI,
				LanguageID: "go",
				Version:    int32(fileVersion),
				Text:       fileData.Content,
			}}
		err = c.jsonConn.Notify(ctx, lsp.MethodTextDocumentDidOpen, params)
		if err != nil {
			fmt.Printf("\n\n\n*** FAILED MethodTextDocumentDidOpen ***\n")
			err = errors.Wrapf(err, "Failed Client.NotifyDidOpenOrChange notification for %q", filePath)
		}
		return
	}

	// Update the contents of the file.
	klog.V(2).Infof("goplsclient.NotifyDidOpenOrChange(ctx, %s) -- file changed at %s", fileData.URI, fileData.ContentTime)
	version := uint64(fileVersion)
	params := &lsp.DidChangeTextDocumentParams{
		TextDocument: lsp.VersionedTextDocumentIdentifier{
			TextDocumentIdentifier: lsp.TextDocumentIdentifier{
				URI: fileData.URI,
			},
			Version: int32(version),
		},
		ContentChanges: []lsp.TextDocumentContentChangeEvent{
			{
				Text: fileData.Content,
			},
		},
	}
	err = c.jsonConn.Notify(ctx, lsp.MethodTextDocumentDidChange, params)
	if err != nil {
		fmt.Printf("\n\n\n*** FAILED MethodTextDocumentDidChange ***\n")
		err = errors.Wrapf(err, "Failed Client.NotifyDidOpenOrChange::change notification for %q", filePath)
	}
	return
}

// CallDefinition service in `gopls`. This returns just the range of where a symbol, under
// the cursor, is defined. See `Definition()` for the full definition service.
//
// This will automatically call NotifyDidOpenOrChange, if file hasn't been sent yet.
func (c *Client) CallDefinition(ctx context.Context, filePath string, line, col int) (results []lsp.Location, err error) {
	if !c.WaitConnection(ctx) {
		// Silently do nothing, if no connection available.
		return
	}
	ctx = minTimeout(ctx, CommunicationTimeout)
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn == nil {
		return
	}
	return c.callDefinitionLocked(ctx, filePath, line, col)
}

func (c *Client) callDefinitionLocked(ctx context.Context, filePath string, line, col int) (results []lsp.Location, err error) {
	klog.V(2).Infof("goplsclient.CallDefinition(ctx, %s, %d, %d)", uri.File(filePath), line, col)
	if _, found := c.fileVersions[filePath]; !found {
		err = c.notifyDidOpenOrChangeLocked(ctx, filePath)
		if err != nil {
			return nil, err
		}
	}

	params := &lsp.TextDocumentPositionParams{
		TextDocument: lsp.TextDocumentIdentifier{
			URI: uri.File(filePath),
		},
		Position: lsp.Position{
			Line:      uint32(line),
			Character: uint32(col),
		},
	}
	_, err = c.jsonConn.Call(ctx, lsp.MethodTextDocumentDefinition, params, &results)
	if err != nil {
		return nil, errors.Wrapf(err, "failed call to `gopls` \"definition_request\"")
	}
	if klog.V(2).Enabled() {
		for ii, r := range results {
			klog.Infof(" Result[%d].URI=%s\n", ii, r.URI)
		}
	}
	return
}

// CallHover service in `gopls`. This returns stuff ... defined here:
// https://microsoft.github.io/language-server-protocol/specifications/lsp/3.17/specification/#textDocument_hover
//
// Documentation was not very clear to me, but it's what gopls uses for Definition.
//
// This will automatically call NotifyDidOpenOrChange, if file hasn't been sent yet.
func (c *Client) CallHover(ctx context.Context, filePath string, line, col int) (hover lsp.Hover, err error) {
	if !c.WaitConnection(ctx) {
		// Silently do nothing, if no connection available.
		return
	}
	ctx = minTimeout(ctx, CommunicationTimeout)
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn == nil {
		return
	}
	return c.callHoverLocked(ctx, filePath, line, col)
}

func (c *Client) callHoverLocked(ctx context.Context, filePath string, line, col int) (hover lsp.Hover, err error) {
	klog.V(2).Infof("goplsclient.CallHover(ctx, %s, %d, %d)", uri.File(filePath), line, col)
	if _, found := c.fileVersions[filePath]; !found {
		err = c.notifyDidOpenOrChangeLocked(ctx, filePath)
		if err != nil {
			return
		}
	}

	params := &lsp.TextDocumentPositionParams{
		TextDocument: lsp.TextDocumentIdentifier{
			URI: uri.File(filePath),
		},
		Position: lsp.Position{
			Line:      uint32(line),
			Character: uint32(col),
		},
	}

	_, err = c.jsonConn.Call(ctx, lsp.MethodTextDocumentHover, params, &hover)
	if err != nil {
		klog.V(2).Infof("goplsclient.CallHover(ctx, %s, %d, %d): %+v", uri.File(filePath), line, col, err)
		err = errors.Wrapf(err, "Failed Client.CallHover notification for %q", filePath)
		return
	}
	return
}

func (c *Client) CallComplete(ctx context.Context, filePath string, line, col int) (items *lsp.CompletionList, err error) {
	if !c.WaitConnection(ctx) {
		// Silently do nothing, if no connection available.
		return
	}
	ctx = minTimeout(ctx, CommunicationTimeout)
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn == nil {
		return
	}
	return c.callCompleteLocked(ctx, filePath, line, col)
}

func (c *Client) callCompleteLocked(ctx context.Context, filePath string, line, col int) (items *lsp.CompletionList, err error) {
	if _, found := c.fileVersions[filePath]; !found {
		err = c.notifyDidOpenOrChangeLocked(ctx, filePath)
		if err != nil {
			return nil, err
		}
	}

	params := &lsp.CompletionParams{
		TextDocumentPositionParams: lsp.TextDocumentPositionParams{
			TextDocument: lsp.TextDocumentIdentifier{
				URI: uri.File(filePath),
			},
			Position: lsp.Position{
				Line:      uint32(line),
				Character: uint32(col),
			},
		},
		Context: &lsp.CompletionContext{
			TriggerKind: lsp.CompletionTriggerKindInvoked,
		},
	}
	items = &lsp.CompletionList{}
	callId, err := c.jsonConn.Call(ctx, lsp.MethodTextDocumentCompletion, params, items)
	_ = callId
	if err != nil {
		return nil, errors.Wrapf(err, "failed call to `gopls` \"complete_request\"")
	}
	return
}

func (c *Client) ConsumeMessages() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	messages := c.messages
	c.messages = nil
	return messages
}

func trimString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-1] + `…`
}

// Handler implements jsonrpc2.Handler, and receives messages initiated by gopls.
func (c *Client) Handler(ctx context.Context, reply jsonrpc2.Replier, req jsonrpc2.Request) error {
	_ = ctx
	method := req.Method()
	switch method {
	case lsp.MethodWindowShowMessage:
		var params lsp.ShowMessageParams
		rawJson := req.Params()
		err := json.Unmarshal(rawJson, &params)
		if err != nil {
			klog.Errorf("Failed to parse ShowMessageParams: %v", err)
			return err
		}
		c.messages = append(c.messages, params.Message)
		klog.V(1).Infof("received gopls show message: %s", trimString(params.Message, 100))

	case lsp.MethodWindowLogMessage:
		var params lsp.LogMessageParams
		rawJson := req.Params()
		err := json.Unmarshal(rawJson, &params)
		if err != nil {
			klog.Errorf("Failed to parse LogMessageParams: %v", err)
			return err
		}
		klog.V(2).Infof("received gopls window log message: %s", params.Message)

	case lsp.MethodTextDocumentPublishDiagnostics:
		var params lsp.PublishDiagnosticsParams
		rawJson := req.Params()
		err := json.Unmarshal(rawJson, &params)
		if err != nil {
			klog.Errorf("Failed to parse LogMessageParams: %v", err)
			return err
		}
		c.messages = make([]string, 0, len(params.Diagnostics))
		for _, diag := range params.Diagnostics {
			c.messages = append(c.messages, diag.Message)
		}
		if (klog.V(2).Enabled() && len(params.Diagnostics) > 0) || klog.V(3).Enabled() {
			klog.V(2).Infof("received gopls diagnostics: %+v",
				trimString(fmt.Sprintf("%+v", params), 100))
		}
	default:
		klog.Errorf("gopls jsonrpc2 message delivered to GoNB but not handled: %q", method)
	}
	return nil
}
