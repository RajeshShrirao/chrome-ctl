package daemon

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/rajeshshrirao/chrome-ctl/internal/config"
	"github.com/rajeshshrirao/chrome-ctl/internal/media"
	"github.com/rajeshshrirao/chrome-ctl/internal/protocol"
)

const (
	defaultRecFPS = 2
	minRecFPS     = 1
	maxRecFPS     = 2
	maxRecFrames  = 240
)

type recordedFrame struct {
	data    []byte
	elapsed time.Duration
}

// recorder is the daemon-owned capture loop state.
type recorder struct {
	mu       sync.Mutex
	active   bool
	starting bool
	fps      int
	cancel   context.CancelFunc
	done     chan struct{}
	frames   []recordedFrame
	started  time.Time
	lastErr  error
	tabID    int
}

func (d *daemon) recorderState() *recorder {
	return &d.rec
}

// handleLocalRPC serves methods the daemon answers itself (media recording,
// macro playback) instead of forwarding to the extension.
func (d *daemon) handleLocalRPC(ctx context.Context, req protocol.Request) (protocol.Response, bool) {
	switch req.Method {
	case protocol.MethodMediaShot:
		return d.handleMediaShot(ctx, req), true
	case protocol.MethodMediaRecStart:
		return d.handleMediaRecStart(ctx, req), true
	case protocol.MethodMediaRecStop:
		return d.handleMediaRecStop(ctx, req), true
	case protocol.MethodMacroStart:
		return d.handleMacroStart(ctx, req), true
	case protocol.MethodMacroSave:
		return d.handleMacroSave(ctx, req), true
	case protocol.MethodMacroList:
		return d.handleMacroList(ctx, req), true
	case protocol.MethodMacroRun:
		return d.handleMacroRun(ctx, req), true
	default:
		return protocol.Response{}, false
	}
}

// closeLocal releases daemon-owned background work such as a recorder ticker.
func (d *daemon) closeLocal() {
	rec := d.recorderState()
	rec.mu.Lock()
	cancel := rec.cancel
	done := rec.done
	rec.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	if done != nil {
		<-done
	}

	rec.mu.Lock()
	rec.active = false
	rec.starting = false
	rec.cancel = nil
	rec.done = nil
	rec.frames = nil
	rec.lastErr = nil
	rec.mu.Unlock()
}

func (d *daemon) handleMediaShot(ctx context.Context, req protocol.Request) protocol.Response {
	var params protocol.MediaShotParams
	if len(req.Params) > 0 {
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return protocol.Failure(req.ID, protocol.ErrInvalidRequest, "invalid media.shot params")
		}
	}

	pngBytes, capture, rpcErr := d.capturePNG(ctx, params.Som, nil)
	if rpcErr != nil {
		return responseWithError(req.ID, rpcErr)
	}

	result, err := media.ProcessPNG(pngBytes, capture.ViewportWidth, media.ImageOptions{
		MaxWidth: params.MaxWidth,
		Quality:  params.Quality,
		Gray:     params.Gray,
		Badges:   elementsToBadges(capture.Elements),
	})
	if err != nil {
		return protocol.Failure(req.ID, protocol.ErrInternal, "process screenshot: "+err.Error())
	}

	path, err := d.resolveMediaOutput(params.OutputPath, fmt.Sprintf("shot-%d.webp", time.Now().UnixNano()))
	if err != nil {
		return protocol.Failure(req.ID, protocol.ErrInvalidRequest, err.Error())
	}
	if err := writeMediaFile(path, result.Data); err != nil {
		return protocol.Failure(req.ID, protocol.ErrInternal, "write screenshot: "+err.Error())
	}

	return protocol.Success(req.ID, protocol.MediaShotResult{
		Path:          path,
		FileSizeBytes: int64(len(result.Data)),
		Width:         result.Width,
		Height:        result.Height,
		SomBadgeCount: result.BadgeCount,
		Gray:          params.Gray,
	})
}

func (d *daemon) handleMediaRecStart(_ context.Context, req protocol.Request) protocol.Response {
	var params protocol.MediaRecStartParams
	if len(req.Params) > 0 {
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return protocol.Failure(req.ID, protocol.ErrInvalidRequest, "invalid media.rec_start params")
		}
	}
	fps := params.FPS
	switch {
	case fps <= 0:
		fps = defaultRecFPS
	case fps < minRecFPS:
		fps = minRecFPS
	case fps > maxRecFPS:
		fps = maxRecFPS
	}

	rec := d.recorderState()
	rec.mu.Lock()
	if rec.active || rec.starting {
		rec.mu.Unlock()
		return protocol.Failure(req.ID, protocol.ErrInternal, "already recording; call media.rec_stop before starting again")
	}
	recCtx, cancel := context.WithCancel(context.Background())
	rec.active = true
	rec.starting = true
	rec.fps = fps
	rec.started = time.Now()
	rec.cancel = cancel
	rec.done = make(chan struct{})
	rec.frames = nil
	rec.lastErr = nil
	rec.tabID = 0
	done := rec.done
	rec.mu.Unlock()

	pngBytes, capture, rpcErr := d.capturePNG(recCtx, false, nil)
	if rpcErr != nil {
		rec.mu.Lock()
		rec.active = false
		rec.starting = false
		if recCtx.Err() == nil {
			// A cancellation means rec_stop/closeLocal interrupted startup;
			// that is not a capture failure worth surfacing later.
			rec.lastErr = errors.New(rpcErr.Message)
		}
		cancel()
		close(done)
		rec.mu.Unlock()
		return responseWithError(req.ID, rpcErr)
	}

	rec.mu.Lock()
	rec.tabID = capture.TabID
	rec.frames = append(rec.frames, recordedFrame{data: pngBytes})
	rec.starting = false
	rec.mu.Unlock()

	interval := time.Second / time.Duration(fps)
	go d.recordLoop(recCtx, rec, done, interval)

	return protocol.Success(req.ID, protocol.MediaRecStartResult{
		Started: true,
		FPS:     fps,
		TabID:   capture.TabID,
	})
}

// recordLoop captures frames until the recorder is stopped, an error occurs,
// or the frame cap is reached. The ticker is deliberately stopped on error so
// rec_stop can surface the failure instead of silently truncating.
func (d *daemon) recordLoop(ctx context.Context, rec *recorder, done chan struct{}, interval time.Duration) {
	defer close(done)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}

		rec.mu.Lock()
		active := rec.active
		tabID := rec.tabID
		rec.mu.Unlock()
		if !active {
			return
		}

		var target *int
		if tabID > 0 {
			t := tabID
			target = &t
		}

		captureCtx, cancel := context.WithTimeout(ctx, mediaCaptureTimeout)
		pngBytes, _, rpcErr := d.capturePNG(captureCtx, false, target)
		cancel()
		if rpcErr != nil {
			if ctx.Err() != nil {
				// Cancelled by rec_stop/closeLocal; not a recording failure.
				return
			}
			rec.mu.Lock()
			rec.lastErr = errors.New(rpcErr.Message)
			rec.mu.Unlock()
			return
		}

		rec.mu.Lock()
		if !rec.active {
			rec.mu.Unlock()
			return
		}
		rec.frames = append(rec.frames, recordedFrame{
			data:    pngBytes,
			elapsed: time.Since(rec.started),
		})
		count := len(rec.frames)
		rec.mu.Unlock()
		if count >= maxRecFrames {
			return
		}
	}
}

func (d *daemon) handleMediaRecStop(_ context.Context, req protocol.Request) protocol.Response {
	var params protocol.MediaRecStopParams
	if len(req.Params) > 0 {
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return protocol.Failure(req.ID, protocol.ErrInvalidRequest, "invalid media.rec_stop params")
		}
	}

	frames, fps, lastErr, wasActive := d.stopRecorder()
	if !wasActive {
		return protocol.Failure(req.ID, protocol.ErrInternal, "no recording in progress; call media.rec_start first")
	}
	if lastErr != nil {
		return protocol.Failure(req.ID, protocol.ErrInternal, "recording failed: "+lastErr.Error())
	}
	if len(frames) == 0 {
		return protocol.Failure(req.ID, protocol.ErrInternal, "no frames captured")
	}

	durationMs := frames[len(frames)-1].elapsed.Milliseconds()

	if params.Grid {
		sheet, err := media.BuildContactSheet(toMediaFrames(frames))
		if err != nil {
			return protocol.Failure(req.ID, protocol.ErrInternal, "build contact sheet: "+err.Error())
		}
		path, err := d.resolveMediaOutput(params.OutputPath, fmt.Sprintf("storyboard-%d.webp", time.Now().UnixNano()))
		if err != nil {
			return protocol.Failure(req.ID, protocol.ErrInvalidRequest, err.Error())
		}
		if err := writeMediaFile(path, sheet.Data); err != nil {
			return protocol.Failure(req.ID, protocol.ErrInternal, "write storyboard: "+err.Error())
		}
		return protocol.Success(req.ID, protocol.MediaRecStopResult{
			Path:           path,
			Format:         "webp_grid",
			FramesCaptured: sheet.Tiles,
			GridDimensions: sheet.GridDimensions,
			Width:          sheet.Width,
			Height:         sheet.Height,
			FileSizeBytes:  int64(len(sheet.Data)),
			DurationMs:     durationMs,
		})
	}

	path, err := d.resolveMediaOutput(params.OutputPath, fmt.Sprintf("recording-%d.webm", time.Now().UnixNano()))
	if err != nil {
		return protocol.Failure(req.ID, protocol.ErrInvalidRequest, err.Error())
	}
	video, err := media.BuildWebM(toMediaFrames(frames), fps, path)
	if err != nil {
		return protocol.Failure(req.ID, protocol.ErrInternal, err.Error())
	}
	info, err := os.Stat(path)
	if err != nil {
		return protocol.Failure(req.ID, protocol.ErrInternal, "stat recording: "+err.Error())
	}
	return protocol.Success(req.ID, protocol.MediaRecStopResult{
		Path:           path,
		Format:         "webm",
		FramesCaptured: len(frames),
		Width:          video.Width,
		Height:         video.Height,
		FileSizeBytes:  info.Size(),
		DurationMs:     durationMs,
	})
}

// stopRecorder cancels the capture loop and waits for it to exit. It reports
// whether a recording was in progress so callers can distinguish a real stop
// from a stray rec_stop.
func (d *daemon) stopRecorder() (frames []recordedFrame, fps int, lastErr error, wasActive bool) {
	rec := d.recorderState()
	rec.mu.Lock()
	if !rec.active && !rec.starting {
		rec.mu.Unlock()
		return nil, 0, nil, false
	}
	cancel := rec.cancel
	done := rec.done
	fps = rec.fps
	rec.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	if done != nil {
		<-done
	}

	rec.mu.Lock()
	frames = append([]recordedFrame(nil), rec.frames...)
	lastErr = rec.lastErr
	if rec.fps != 0 {
		fps = rec.fps
	}
	rec.active = false
	rec.starting = false
	rec.cancel = nil
	rec.done = nil
	rec.frames = nil
	rec.lastErr = nil
	rec.started = time.Time{}
	rec.tabID = 0
	rec.mu.Unlock()
	return frames, fps, lastErr, true
}

func (d *daemon) capturePNG(ctx context.Context, includeElements bool, tabID *int) ([]byte, protocol.MediaCaptureResult, *protocol.RPCError) {
	raw, err := json.Marshal(protocol.MediaCaptureParams{TabID: tabID, IncludeElements: includeElements})
	if err != nil {
		return nil, protocol.MediaCaptureResult{}, &protocol.RPCError{
			Code:    protocol.ErrInternal,
			Message: "encode media.capture params",
		}
	}

	result, rpcErr := d.forward(ctx, protocol.Request{
		JSONRPC: protocol.JSONRPC,
		Method:  protocol.MethodMediaCapture,
		Params:  raw,
	})
	if rpcErr != nil {
		return nil, protocol.MediaCaptureResult{}, rpcErr
	}

	var capture protocol.MediaCaptureResult
	if err := json.Unmarshal(result, &capture); err != nil {
		return nil, capture, &protocol.RPCError{
			Code:    protocol.ErrInternal,
			Message: "invalid media.capture response",
		}
	}
	pngBytes, err := base64.StdEncoding.DecodeString(capture.PNGBase64)
	if err != nil || len(pngBytes) == 0 {
		return nil, capture, &protocol.RPCError{
			Code:    protocol.ErrInternal,
			Message: "media.capture returned invalid PNG data",
		}
	}
	return pngBytes, capture, nil
}

func elementsToBadges(elements []protocol.SnapElement) []media.Badge {
	if len(elements) == 0 {
		return nil
	}
	badges := make([]media.Badge, 0, len(elements))
	for _, el := range elements {
		badges = append(badges, media.Badge{ID: el.ID, X: el.X, Y: el.Y, W: el.W, H: el.H})
	}
	return badges
}

func toMediaFrames(frames []recordedFrame) []media.Frame {
	out := make([]media.Frame, len(frames))
	for i, f := range frames {
		out[i] = media.Frame{Data: f.data, Elapsed: f.elapsed}
	}
	return out
}

func (d *daemon) mediaDir() string {
	if d.cfg.Paths.MediaDir != "" {
		return d.cfg.Paths.MediaDir
	}
	return config.DefaultPaths().MediaDir
}

// resolveMediaOutput turns an empty or relative output path into an absolute
// path rooted at the media dir. The media dir is created on demand; any other
// parent directory must already exist.
func (d *daemon) resolveMediaOutput(outputPath, defaultName string) (string, error) {
	dir := d.mediaDir()
	path := outputPath
	if path == "" {
		path = filepath.Join(dir, defaultName)
	} else if !filepath.IsAbs(path) {
		path = filepath.Join(dir, path)
	}

	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve output path: %w", err)
	}
	parent := filepath.Dir(abs)

	mediaAbs, err := filepath.Abs(dir)
	if err == nil && parent == mediaAbs {
		if err := os.MkdirAll(mediaAbs, 0o700); err != nil {
			return "", fmt.Errorf("create media directory: %w", err)
		}
		return abs, nil
	}

	info, err := os.Stat(parent)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", fmt.Errorf("output directory does not exist: %s", parent)
		}
		return "", fmt.Errorf("stat output directory: %w", err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("output parent is not a directory: %s", parent)
	}
	return abs, nil
}

func writeMediaFile(path string, data []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if err := f.Chmod(0o600); err != nil {
		_ = f.Close()
		return err
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}
