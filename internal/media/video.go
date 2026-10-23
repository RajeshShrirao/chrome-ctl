package media

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image/png"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	// VideoMaxWidth is the maximum encoded video width in pixels.
	VideoMaxWidth = 1280
	// videoCRF is the VP9 constant-quality level (higher = smaller/lossier).
	videoCRF     = 40
	videoTimeout = 120 * time.Second
)

// Frame is one recorded viewport PNG plus its offset from recording start.
type Frame struct {
	Data    []byte
	Elapsed time.Duration
}

// VideoResult reports the encoded dimensions of a WebM artifact.
type VideoResult struct {
	Width  int
	Height int
}

// FFmpegPath resolves the ffmpeg binary, honoring CHROME_CTL_FFMPEG first.
func FFmpegPath() (string, error) {
	if override := os.Getenv("CHROME_CTL_FFMPEG"); override != "" {
		info, err := os.Stat(override)
		if err != nil || info.IsDir() {
			return "", fmt.Errorf("ffmpeg not found at CHROME_CTL_FFMPEG=%s; install it or use rec stop --grid", override)
		}
		return override, nil
	}
	path, err := exec.LookPath("ffmpeg")
	if err != nil {
		return "", errors.New("ffmpeg not found on PATH; install it or use rec stop --grid")
	}
	return path, nil
}

// BuildWebM writes a VP9 WebM from captured PNG frames. The output parent
// directory must already exist; this function only creates a private temp dir
// for the intermediate frames.
func BuildWebM(frames []Frame, fps int, outputPath string) (VideoResult, error) {
	if len(frames) == 0 {
		return VideoResult{}, errors.New("no frames to encode")
	}
	switch {
	case fps <= 0:
		fps = 2
	case fps < 1:
		fps = 1
	case fps > 2:
		fps = 2
	}

	ffmpeg, err := FFmpegPath()
	if err != nil {
		return VideoResult{}, err
	}

	cfg, err := png.DecodeConfig(bytes.NewReader(frames[0].Data))
	if err != nil {
		return VideoResult{}, fmt.Errorf("decode first frame: %w", err)
	}
	if cfg.Width <= 0 || cfg.Height <= 0 {
		return VideoResult{}, errors.New("first frame has empty dimensions")
	}

	out, err := filepath.Abs(outputPath)
	if err != nil {
		return VideoResult{}, fmt.Errorf("resolve output path: %w", err)
	}

	tmpDir, err := os.MkdirTemp("", "chrome-ctl-rec-*")
	if err != nil {
		return VideoResult{}, fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	for i, frame := range frames {
		name := filepath.Join(tmpDir, fmt.Sprintf("frame-%04d.png", i+1))
		if err := os.WriteFile(name, frame.Data, 0o600); err != nil {
			return VideoResult{}, fmt.Errorf("write frame %d: %w", i+1, err)
		}
	}

	args := []string{
		"-y",
		"-loglevel", "error",
		"-framerate", strconv.Itoa(fps),
		"-i", "frame-%04d.png",
		// ffmpeg's filter parser splits unquoted commas, so the expression is
		// quoted exactly as it would be on a shell command line.
		"-vf", "scale='min(1280,iw)':-2",
		"-c:v", "libvpx-vp9",
		"-b:v", "0",
		"-crf", strconv.Itoa(videoCRF),
		"-an",
		"-pix_fmt", "yuv420p",
		out,
	}

	ctx, cancel := context.WithTimeout(context.Background(), videoTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, ffmpeg, args...)
	cmd.Dir = tmpDir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return VideoResult{}, fmt.Errorf("ffmpeg timed out after %s", videoTimeout)
		}
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return VideoResult{}, fmt.Errorf("ffmpeg failed: %w: %s", err, msg)
		}
		return VideoResult{}, fmt.Errorf("ffmpeg failed: %w", err)
	}

	if err := os.Chmod(out, 0o600); err != nil {
		return VideoResult{}, fmt.Errorf("secure output file: %w", err)
	}

	width := cfg.Width
	if width > VideoMaxWidth {
		width = VideoMaxWidth
	}
	height := int(math.Round(float64(cfg.Height) * float64(width) / float64(cfg.Width)))
	if height < 2 {
		height = 2
	}
	if height%2 != 0 {
		// ffmpeg's -2 scale height rounds to an even value for yuv420p.
		height--
	}
	return VideoResult{Width: width, Height: height}, nil
}
