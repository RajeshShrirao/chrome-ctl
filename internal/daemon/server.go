package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	"github.com/rajeshshrirao/chrome-ctl/internal/protocol"
)

const (
	// Media capture frames are base64 PNGs and can exceed 1 MiB on large displays.
	maxMessageBytes = 16 << 20
	writeTimeout    = 5 * time.Second
)

type extensionConn struct {
	ws        *websocket.Conn
	writeMu   sync.Mutex
	closeOnce sync.Once
}

func (c *extensionConn) writeJSON(v any) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	_ = c.ws.SetWriteDeadline(time.Now().Add(writeTimeout))
	return c.ws.WriteJSON(v)
}

func (c *extensionConn) close(code int, reason string) {
	c.closeOnce.Do(func() {
		c.writeMu.Lock()
		_ = c.ws.WriteControl(
			websocket.CloseMessage,
			websocket.FormatCloseMessage(code, reason),
			time.Now().Add(time.Second),
		)
		c.writeMu.Unlock()
		_ = c.ws.Close()
	})
}

type daemon struct {
	cfg     Config
	started time.Time
	log     *log.Logger

	upgrader websocket.Upgrader
	nextID   atomic.Uint64

	mu      sync.Mutex
	conn    *extensionConn
	pending map[string]chan protocol.Response
	rec     recorder
	macro   macroRecorder

	lastShotMu   sync.Mutex
	lastShotHash string

	httpSrv *http.Server
}

func newDaemon(cfg Config) *daemon {
	cfg = withDefaults(cfg)
	return &daemon{
		cfg:      cfg,
		started:  time.Now(),
		log:      cfg.Log,
		upgrader: websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }},
		pending:  make(map[string]chan protocol.Response),
	}
}

func withDefaults(cfg Config) Config {
	if cfg.ReadTimeout == 0 {
		cfg.ReadTimeout = 45 * time.Second
	}
	if cfg.ForwardTimeout == 0 {
		cfg.ForwardTimeout = 10 * time.Second
	}
	if cfg.Log == nil {
		cfg.Log = log.Default()
	}
	return cfg
}

func serve(ctx context.Context, cfg Config) error {
	d := newDaemon(cfg)
	addr := d.cfg.Addr
	if strings.HasPrefix(addr, ":") {
		addr = "127.0.0.1" + addr
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("daemon: listen %s: %w", addr, err)
	}
	d.log.Printf("daemon: listening on %s", ln.Addr())
	return d.serve(ctx, ln)
}

func (d *daemon) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", d.requireAuth(d.handleHealth))
	mux.HandleFunc("POST /rpc", d.requireAuth(d.handleRPC))
	mux.HandleFunc("GET /ws", d.requireAuth(d.handleWS))
	mux.HandleFunc("POST /pair", d.handlePair)
	return mux
}

func (d *daemon) serve(ctx context.Context, ln net.Listener) error {
	d.httpSrv = &http.Server{
		Handler:           d.handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}
	errCh := make(chan error, 1)
	go func() { errCh <- d.httpSrv.Serve(ln) }()

	select {
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("daemon: serve: %w", err)
	case <-ctx.Done():
	}

	d.shutdown()
	return nil
}

func (d *daemon) shutdown() {
	d.log.Printf("daemon: shutting down")
	d.closeLocal()
	d.closeExtension(websocket.CloseGoingAway, "daemon shutting down")
	if d.httpSrv != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = d.httpSrv.Shutdown(ctx)
	}
}

func (d *daemon) uptimeSec() int64 {
	return int64(time.Since(d.started).Seconds())
}

func (d *daemon) extension() *extensionConn {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.conn
}

func (d *daemon) register(ext *extensionConn) *extensionConn {
	d.mu.Lock()
	old := d.conn
	d.conn = ext
	pending := d.takePendingLocked()
	d.mu.Unlock()

	failPending(pending, protocol.ErrConnection, "extension connection replaced")
	return old
}

func (d *daemon) unregister(ext *extensionConn) {
	d.mu.Lock()
	if d.conn != ext {
		d.mu.Unlock()
		return
	}
	d.conn = nil
	pending := d.takePendingLocked()
	d.mu.Unlock()

	failPending(pending, protocol.ErrConnection, "chrome extension disconnected")
}

func (d *daemon) closeExtension(code int, reason string) {
	d.mu.Lock()
	ext := d.conn
	d.conn = nil
	pending := d.takePendingLocked()
	d.mu.Unlock()

	if ext != nil {
		ext.close(code, reason)
	}
	failPending(pending, protocol.ErrConnection, "daemon shutting down")
}

func (d *daemon) takePendingLocked() map[string]chan protocol.Response {
	pending := d.pending
	d.pending = make(map[string]chan protocol.Response)
	return pending
}

func failPending(pending map[string]chan protocol.Response, code int, message string) {
	resp := protocol.Response{
		JSONRPC: protocol.JSONRPC,
		Error:   &protocol.RPCError{Code: code, Message: message},
	}
	for _, ch := range pending {
		ch <- resp
	}
}

func (d *daemon) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, struct {
		Status    string `json:"status"`
		Version   string `json:"version"`
		UptimeSec int64  `json:"uptimeSec"`
	}{
		Status:    "ok",
		Version:   d.cfg.Version,
		UptimeSec: d.uptimeSec(),
	})
}

func (d *daemon) handleWS(w http.ResponseWriter, r *http.Request) {
	ws, err := d.upgrader.Upgrade(w, r, nil)
	if err != nil {
		d.log.Printf("daemon: websocket upgrade failed: %v", err)
		return
	}
	ext := &extensionConn{ws: ws}
	ws.SetReadLimit(maxMessageBytes)
	_ = ws.SetReadDeadline(time.Now().Add(d.cfg.ReadTimeout))
	ws.SetPongHandler(func(string) error {
		return ws.SetReadDeadline(time.Now().Add(d.cfg.ReadTimeout))
	})

	if old := d.register(ext); old != nil {
		old.close(websocket.ClosePolicyViolation, "replaced")
	}
	d.log.Printf("daemon: extension connected")
	defer func() {
		d.unregister(ext)
		ext.close(websocket.CloseNormalClosure, "")
		d.log.Printf("daemon: extension disconnected")
	}()

	if err := d.sendReady(ext); err != nil {
		d.log.Printf("daemon: send system.ready: %v", err)
		return
	}
	d.readLoop(ext)
}

func (d *daemon) sendReady(ext *extensionConn) error {
	params, err := json.Marshal(protocol.SystemReadyParams{
		Version:         d.cfg.Version,
		DaemonUptimeSec: d.uptimeSec(),
		AttachedTabs:    []int{},
	})
	if err != nil {
		return err
	}
	return ext.writeJSON(protocol.Request{
		JSONRPC: protocol.JSONRPC,
		Method:  protocol.MethodSystemReady,
		Params:  params,
	})
}

func (d *daemon) readLoop(ext *extensionConn) {
	for {
		_, msg, err := ext.ws.ReadMessage()
		if err != nil {
			return
		}
		_ = ext.ws.SetReadDeadline(time.Now().Add(d.cfg.ReadTimeout))
		d.handleExtensionMessage(ext, msg)
	}
}

func (d *daemon) handleExtensionMessage(ext *extensionConn, msg []byte) {
	var req protocol.Request
	if err := json.Unmarshal(msg, &req); err != nil {
		d.log.Printf("daemon: invalid extension message: %v", err)
		return
	}
	if req.Method != "" {
		if req.Method == protocol.MethodSystemPing {
			d.replyPong(ext, req)
		}
		return
	}

	var resp protocol.Response
	if err := json.Unmarshal(msg, &resp); err != nil {
		d.log.Printf("daemon: invalid extension response: %v", err)
		return
	}
	if resp.ID != nil {
		d.routeResponse(resp)
	}
}

func (d *daemon) replyPong(ext *extensionConn, req protocol.Request) {
	var params protocol.PingParams
	if len(req.Params) > 0 {
		if err := json.Unmarshal(req.Params, &params); err != nil {
			d.log.Printf("daemon: invalid system.ping params")
		}
	}
	if err := ext.writeJSON(protocol.Success(req.ID, protocol.PongResult{
		Status:    "pong",
		Timestamp: params.Timestamp,
	})); err != nil {
		d.log.Printf("daemon: send pong: %v", err)
	}
}

func (d *daemon) routeResponse(resp protocol.Response) {
	if resp.ID == nil {
		return
	}
	d.mu.Lock()
	ch, ok := d.pending[*resp.ID]
	if ok {
		delete(d.pending, *resp.ID)
	}
	d.mu.Unlock()
	if ok {
		ch <- resp
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	data, err := json.Marshal(v)
	if err != nil {
		http.Error(w, "encoding error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(data)
}
