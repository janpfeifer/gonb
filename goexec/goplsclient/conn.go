package goplsclient

import (
	"context"
	"encoding/json"
	"fmt"
	"k8s.io/klog/v2"
	"net"
	"strings"
	"time"

	"github.com/go-language-server/jsonrpc2"
	lsp "github.com/go-language-server/protocol"
	"github.com/go-language-server/uri"
	"github.com/pkg/errors"
)

var _ = lsp.MethodInitialize

var (
	ConnectTimeout       = 2000 * time.Millisecond
	CommunicationTimeout = 2000 * time.Millisecond
)

// jsonrpc2Handler implements jsonrpc2.Handler, listening to incoming events.
type jsonrpc2Handler struct {
	client *Client
}

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

	jsonStream := jsonrpc2.NewStream(c.conn, c.conn)
	c.jsonConn = jsonrpc2.NewConn(jsonStream)
	c.jsonHandler = &jsonrpc2Handler{client: c}
	c.jsonConn.AddHandler(c.jsonHandler)
	go func(currentConn net.Conn) {
		// Exec should use a non-expiring context.
		ctx := context.Background()
		_ = c.jsonConn.Run(ctx)
		klog.Infof("- gopls connection stopped")
		c.mu.Lock()
		defer c.mu.Unlock()
		if c.conn == currentConn {
			c.connCloseLocked()
		}
	}(c.conn)

	err = c.jsonConn.Call(ctx, lsp.MethodInitialize, &lsp.InitializeParams{
		ProcessID: 0,
		RootURI:   uri.File(c.dir),
		// Capabilities:          lsp.ClientCapabilities{},
	}, &c.lspCapabilities)
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

// NotifyDidOpenOrChange sends a notification to `gopls` with the open file, which also sends the
// file content (from `Client.fileCache` if available).
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
	fileVersion, previouslyOpened := c.fileVersions[filePath]
	if previouslyOpened && !fileUpdated {
		// File already opened, and it hasn't changed, nothing to do.
		return nil
	}
	fileVersion += 1
	c.fileVersions[filePath] = fileVersion

	if !previouslyOpened {
		klog.V(2).Infof("goplsclient.NotifyDidOpenOrChange(ctx, %s) -- file opened", fileData.URI)
		params := &lsp.DidOpenTextDocumentParams{
			TextDocument: lsp.TextDocumentItem{
				URI:        fileData.URI,
				LanguageID: "go",
				Version:    float64(fileVersion),
				Text:       fileData.Content,
			}}
		err = c.jsonConn.Notify(ctx, lsp.MethodTextDocumentDidOpen, params)
		if err != nil {
			return errors.Wrapf(err, "Failed Client.NotifyDidOpenOrChange notification for %q", filePath)
		}
	} else {
		klog.V(2).Infof("goplsclient.NotifyDidOpenOrChange(ctx, %s) -- file changed at %s", fileData.URI, fileData.ContentTime)
		version := uint64(fileVersion)
		params := &lsp.DidChangeTextDocumentParams{
			TextDocument: lsp.VersionedTextDocumentIdentifier{
				TextDocumentIdentifier: lsp.TextDocumentIdentifier{
					URI: fileData.URI,
				},
				Version: &version,
			},
			ContentChanges: []lsp.TextDocumentContentChangeEvent{
				{
					Text: fileData.Content,
				},
			},
		}
		err = c.jsonConn.Notify(ctx, lsp.MethodTextDocumentDidChange, params)
		if err != nil {
			return errors.Wrapf(err, "Failed Client.NotifyDidOpenOrChange::change notification for %q", filePath)
		}
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
			Line:      float64(line),
			Character: float64(col),
		},
	}
	err = c.jsonConn.Call(ctx, lsp.MethodTextDocumentDefinition, params, &results)
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
			Line:      float64(line),
			Character: float64(col),
		},
	}

	err = c.jsonConn.Call(ctx, lsp.MethodTextDocumentHover, params, &hover)
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
				Line:      float64(line),
				Character: float64(col),
			},
		},
		Context: &lsp.CompletionContext{
			TriggerKind: lsp.Invoked,
		},
	}
	items = &lsp.CompletionList{}
	err = c.jsonConn.Call(ctx, lsp.MethodTextDocumentCompletion, params, items)
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

// Deliver implements jsonrpc2.Handler.
func (h *jsonrpc2Handler) Deliver(ctx context.Context, r *jsonrpc2.Request, delivered bool) bool {
	_ = ctx
	_ = delivered
	switch r.Method {
	case lsp.MethodWindowShowMessage:
		var params lsp.ShowMessageParams
		err := json.Unmarshal(*r.WireRequest.Params, &params)
		if err != nil {
			klog.Errorf("Failed to parse ShowMessageParams: %v", err)
			return true
		}
		h.client.messages = append(h.client.messages, params.Message)
		klog.V(1).Infof("received gopls show message: %s", trimString(params.Message, 100))
		return true
	case lsp.MethodWindowLogMessage:
		var params lsp.LogMessageParams
		err := json.Unmarshal(*r.WireRequest.Params, &params)
		if err != nil {
			klog.Errorf("Failed to parse LogMessageParams: %v", err)
			return true
		}
		klog.V(2).Infof("received gopls window log message: %q", trimString(params.Message, 100))
		return true
	case lsp.MethodTextDocumentPublishDiagnostics:
		var params lsp.PublishDiagnosticsParams
		err := json.Unmarshal(*r.WireRequest.Params, &params)
		if err != nil {
			klog.Errorf("Failed to parse LogMessageParams: %v", err)
			return true
		}
		h.client.messages = make([]string, 0, len(params.Diagnostics))
		for _, diag := range params.Diagnostics {
			h.client.messages = append(h.client.messages, diag.Message)
		}
		klog.V(2).Infof("received gopls diagnostics: %+v", trimString(fmt.Sprintf("%+v", params), 100))
		return true
	default:
		klog.Errorf("gopls jsonrpc2 delivered but not handled: %q", r.Method)
	}
	return false
}

// Cancel implements jsonrpc2.Handler.
func (h *jsonrpc2Handler) Cancel(ctx context.Context, conn *jsonrpc2.Conn, id jsonrpc2.ID, canceled bool) bool {
	_ = ctx
	_ = conn
	_ = canceled
	klog.Warningf("- jsonrpc2 cancelled request id=%+v", id)
	return false
}

// Request implements jsonrpc2.Handler.
func (h *jsonrpc2Handler) Request(ctx context.Context, conn *jsonrpc2.Conn, direction jsonrpc2.Direction, r *jsonrpc2.WireRequest) context.Context {
	_ = conn
	_ = direction
	klog.V(2).Infof("jsonrpc2 Request(direction=%s) %q", direction, r.Method)
	return ctx
}

// Response implements jsonrpc2.Handler.
func (h *jsonrpc2Handler) Response(ctx context.Context, conn *jsonrpc2.Conn, direction jsonrpc2.Direction, r *jsonrpc2.WireResponse) context.Context {
	_ = conn
	var content string
	if r.Result != nil && len(*r.Result) > 0 {
		content = trimString(string(*r.Result), 100)
	}
	klog.V(2).Infof("- jsonrpc2 Response(direction=%s) id=%+v, content=%s", direction, r.ID, content)
	return ctx
}

// Done implements jsonrpc2.Handler.
func (h *jsonrpc2Handler) Done(context.Context, error) {}

// Read implements jsonrpc2.Handler.
func (h *jsonrpc2Handler) Read(ctx context.Context, _ int64) context.Context { return ctx }

// Write implements jsonrpc2.Handler.
func (h *jsonrpc2Handler) Write(ctx context.Context, _ int64) context.Context { return ctx }

// Error implements jsonrpc2.Handler.
func (h *jsonrpc2Handler) Error(ctx context.Context, err error) {
	klog.Errorf("- jsonrpc2 Error: %+v", err)
}
