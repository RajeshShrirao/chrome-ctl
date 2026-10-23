package daemon

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"io"
	"log"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rajeshshrirao/chrome-ctl/internal/config"
	"github.com/rajeshshrirao/chrome-ctl/internal/protocol"
)

func newMediaTestDaemon(t *testing.T) *daemon {
	t.Helper()
	dir := t.TempDir()
	return newDaemon(Config{
		Paths: config.Paths{
			Home:     dir,
			MediaDir: filepath.Join(dir, "media"),
		},
		Log: log.New(io.Discard, "", 0),
	})
}

func testCapturePNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetRGBA(x, y, color.RGBA{R: uint8(x), G: uint8(y), B: 120, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode capture PNG: %v", err)
	}
	return buf.Bytes()
}

func TestHandleLocalRPCUnknownMethod(t *testing.T) {
	d := newMediaTestDaemon(t)
	defer d.closeLocal()

	id := "unknown-1"
	resp, ok := d.handleLocalRPC(context.Background(), protocol.Request{
		JSONRPC: protocol.JSONRPC,
		ID:      &id,
		Method:  "tab.list",
	})
	if ok {
		t.Fatalf("handleLocalRPC handled %q, want false (resp %+v)", "tab.list", resp)
	}
}

func TestMediaShotWithoutExtension(t *testing.T) {
	d := newMediaTestDaemon(t)
	defer d.closeLocal()

	id := "shot-1"
	resp, ok := d.handleLocalRPC(context.Background(), protocol.Request{
		JSONRPC: protocol.JSONRPC,
		ID:      &id,
		Method:  protocol.MethodMediaShot,
		Params:  json.RawMessage(`{"som":true,"gray":true}`),
	})
	if !ok {
		t.Fatal("media.shot was not handled locally")
	}
	if resp.Error == nil || resp.Error.Code != protocol.ErrConnection {
		t.Fatalf("error = %+v, want code %d", resp.Error, protocol.ErrConnection)
	}
}

func TestMediaRecStartWithoutExtension(t *testing.T) {
	d := newMediaTestDaemon(t)
	defer d.closeLocal()

	id := "rec-1"
	resp, ok := d.handleLocalRPC(context.Background(), protocol.Request{
		JSONRPC: protocol.JSONRPC,
		ID:      &id,
		Method:  protocol.MethodMediaRecStart,
		Params:  json.RawMessage(`{"fps":2}`),
	})
	if !ok {
		t.Fatal("media.rec_start was not handled locally")
	}
	if resp.Error == nil || resp.Error.Code != protocol.ErrConnection {
		t.Fatalf("error = %+v, want code %d", resp.Error, protocol.ErrConnection)
	}
}

func TestMediaRecStopWithoutStart(t *testing.T) {
	d := newMediaTestDaemon(t)
	defer d.closeLocal()

	id := "rec-2"
	resp, ok := d.handleLocalRPC(context.Background(), protocol.Request{
		JSONRPC: protocol.JSONRPC,
		ID:      &id,
		Method:  protocol.MethodMediaRecStop,
		Params:  json.RawMessage(`{"grid":true}`),
	})
	if !ok {
		t.Fatal("media.rec_stop was not handled locally")
	}
	if resp.Error == nil || resp.Error.Code != protocol.ErrInternal {
		t.Fatalf("error = %+v, want code %d", resp.Error, protocol.ErrInternal)
	}
}

func TestMediaShotAgainstFakeExtension(t *testing.T) {
	cfg := testConfig(t)
	cfg.Paths.MediaDir = filepath.Join(cfg.Paths.Home, "media")
	srv := startDaemon(t, cfg)
	token := testToken(t, cfg)
	ext := dialWS(t, wsURL(srv.URL, token))
	readReady(t, ext)

	resultCh := startRPC(t, srv, token, `{"jsonrpc":"2.0","id":"shot-1","method":"media.shot","params":{"som":true}}`)

	_ = ext.SetReadDeadline(time.Now().Add(2 * time.Second))
	var fwd protocol.Request
	if err := ext.ReadJSON(&fwd); err != nil {
		t.Fatalf("read forwarded capture: %v", err)
	}
	if fwd.Method != protocol.MethodMediaCapture {
		t.Fatalf("forwarded method = %q, want %q", fwd.Method, protocol.MethodMediaCapture)
	}
	var captureParams protocol.MediaCaptureParams
	if err := json.Unmarshal(fwd.Params, &captureParams); err != nil {
		t.Fatalf("decode capture params: %v", err)
	}
	if !captureParams.IncludeElements {
		t.Fatal("includeElements = false, want true for som")
	}

	pngBytes := testCapturePNG(t, 200, 100)
	capture := protocol.MediaCaptureResult{
		TabID:         5,
		PNGBase64:     base64.StdEncoding.EncodeToString(pngBytes),
		ViewportWidth: 200, ViewportHeight: 100, DevicePixelRatio: 2,
		Elements: []protocol.SnapElement{{ID: 1, Tag: "button", X: 10, Y: 10, W: 40, H: 20}},
	}
	raw, err := json.Marshal(capture)
	if err != nil {
		t.Fatalf("encode capture result: %v", err)
	}
	if err := ext.WriteJSON(protocol.Response{JSONRPC: protocol.JSONRPC, ID: fwd.ID, Result: raw}); err != nil {
		t.Fatalf("write capture result: %v", err)
	}

	var r rpcResult
	select {
	case r = <-resultCh:
	case <-time.After(5 * time.Second):
		t.Fatal("media.shot timed out")
	}
	if r.err != nil {
		t.Fatalf("rpc request: %v", r.err)
	}
	var resp protocol.Response
	if err := json.Unmarshal(r.body, &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}
	var shot protocol.MediaShotResult
	if err := json.Unmarshal(resp.Result, &shot); err != nil {
		t.Fatalf("decode shot result: %v", err)
	}
	if shot.Width != 200 || shot.Height != 100 {
		t.Fatalf("size = %dx%d, want 200x100", shot.Width, shot.Height)
	}
	if shot.SomBadgeCount != 1 {
		t.Fatalf("somBadgeCount = %d, want 1", shot.SomBadgeCount)
	}
	if !filepath.IsAbs(shot.Path) {
		t.Fatalf("path = %q, want absolute", shot.Path)
	}
	if dir := filepath.Dir(shot.Path); dir != cfg.Paths.MediaDir {
		t.Fatalf("path dir = %q, want %q", dir, cfg.Paths.MediaDir)
	}
	data, err := os.ReadFile(shot.Path)
	if err != nil {
		t.Fatalf("read screenshot: %v", err)
	}
	if len(data) < 12 || string(data[0:4]) != "RIFF" || string(data[8:12]) != "WEBP" {
		t.Fatalf("screenshot is not WebP (len %d)", len(data))
	}
	if int64(len(data)) != shot.FileSizeBytes {
		t.Fatalf("fileSizeBytes = %d, want %d", shot.FileSizeBytes, len(data))
	}
	info, err := os.Stat(shot.Path)
	if err != nil {
		t.Fatalf("stat screenshot: %v", err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Fatalf("mode = %o, want 600", mode)
	}
}

// TestRecorderGridAndVideo drives a real recorder against a fake extension so
// ticker shutdown, sampling, and both output formats are exercised together.
func TestRecorderGridAndVideo(t *testing.T) {
	cfg := testConfig(t)
	cfg.Paths.MediaDir = filepath.Join(cfg.Paths.Home, "media")
	// The recorder test lasts a few seconds and the fake extension sends no
	// traffic of its own, so keep the websocket idle deadline generous.
	cfg.ReadTimeout = 30 * time.Second
	d := newDaemon(cfg)
	srv := httptest.NewServer(d.handler())
	t.Cleanup(srv.Close)
	defer d.closeLocal()
	token := testToken(t, cfg)

	ext := dialWS(t, wsURL(srv.URL, token))
	readReady(t, ext)
	// readReady leaves a short client-side read deadline behind.
	_ = ext.SetReadDeadline(time.Time{})

	pngBytes := testCapturePNG(t, 320, 180)
	go func() {
		for {
			var fwd protocol.Request
			if err := ext.ReadJSON(&fwd); err != nil {
				return
			}
			if fwd.Method != protocol.MethodMediaCapture {
				continue
			}
			raw, err := json.Marshal(protocol.MediaCaptureResult{
				TabID:         9,
				PNGBase64:     base64.StdEncoding.EncodeToString(pngBytes),
				ViewportWidth: 320, ViewportHeight: 180,
			})
			if err != nil {
				return
			}
			if err := ext.WriteJSON(protocol.Response{JSONRPC: protocol.JSONRPC, ID: fwd.ID, Result: raw}); err != nil {
				return
			}
		}
	}()

	start := func() protocol.MediaRecStartResult {
		t.Helper()
		_, body := postRPC(t, srv, token, `{"jsonrpc":"2.0","id":"rec-start","method":"media.rec_start","params":{"fps":2}}`)
		var resp protocol.Response
		if err := json.Unmarshal(body, &resp); err != nil {
			t.Fatalf("decode rec_start response: %v", err)
		}
		if resp.Error != nil {
			t.Fatalf("rec_start error: %+v", resp.Error)
		}
		var result protocol.MediaRecStartResult
		if err := json.Unmarshal(resp.Result, &result); err != nil {
			t.Fatalf("decode rec_start result: %v", err)
		}
		if !result.Started || result.FPS != 2 || result.TabID != 9 {
			t.Fatalf("rec_start = %+v, want started at 2fps on tab 9", result)
		}
		return result
	}
	stop := func(params string) protocol.Response {
		t.Helper()
		_, body := postRPC(t, srv, token, `{"jsonrpc":"2.0","id":"rec-stop","method":"media.rec_stop","params":`+params+`}`)
		var resp protocol.Response
		if err := json.Unmarshal(body, &resp); err != nil {
			t.Fatalf("decode rec_stop response: %v", err)
		}
		return resp
	}

	start()
	time.Sleep(800 * time.Millisecond)
	resp := stop(`{"grid":true}`)
	if resp.Error != nil {
		t.Fatalf("rec_stop grid error: %+v", resp.Error)
	}
	var grid protocol.MediaRecStopResult
	if err := json.Unmarshal(resp.Result, &grid); err != nil {
		t.Fatalf("decode grid result: %v", err)
	}
	if grid.Format != "webp_grid" || grid.GridDimensions != "3x4" {
		t.Fatalf("grid result = %+v, want webp_grid 3x4", grid)
	}
	if grid.Width != 1024 || grid.Height != 768 {
		t.Fatalf("grid size = %dx%d, want 1024x768", grid.Width, grid.Height)
	}
	if grid.FramesCaptured < 2 || grid.FramesCaptured > 12 {
		t.Fatalf("grid frames = %d, want 2..12", grid.FramesCaptured)
	}
	gridData, err := os.ReadFile(grid.Path)
	if err != nil {
		t.Fatalf("read storyboard: %v", err)
	}
	if len(gridData) < 12 || string(gridData[0:4]) != "RIFF" || string(gridData[8:12]) != "WEBP" {
		t.Fatalf("storyboard is not WebP (len %d)", len(gridData))
	}

	start()
	time.Sleep(800 * time.Millisecond)
	resp = stop(`{}`)
	if resp.Error != nil {
		t.Fatalf("rec_stop video error: %+v", resp.Error)
	}
	var video protocol.MediaRecStopResult
	if err := json.Unmarshal(resp.Result, &video); err != nil {
		t.Fatalf("decode video result: %v", err)
	}
	if video.Format != "webm" || video.FramesCaptured < 2 {
		t.Fatalf("video result = %+v, want webm with frames", video)
	}
	if video.Width != 320 || video.Height != 180 {
		t.Fatalf("video size = %dx%d, want 320x180", video.Width, video.Height)
	}
	videoData, err := os.ReadFile(video.Path)
	if err != nil {
		t.Fatalf("read video: %v", err)
	}
	if len(videoData) < 4 || videoData[0] != 0x1A || videoData[1] != 0x45 || videoData[2] != 0xDF || videoData[3] != 0xA3 {
		t.Fatalf("video magic = % x, want EBML 1a45dfa3", videoData[:4])
	}
}
