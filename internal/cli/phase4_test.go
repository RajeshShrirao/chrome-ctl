package cli

import (
	"path/filepath"
	"testing"

	"github.com/rajeshshrirao/chrome-ctl/internal/protocol"
)

func TestFormatShot(t *testing.T) {
	tests := []struct {
		name   string
		result protocol.MediaShotResult
		want   string
	}{
		{
			name:   "color",
			result: protocol.MediaShotResult{Path: "/tmp/proof.webp", FileSizeBytes: 39322, Width: 1024, Height: 576},
			want:   "OK: saved /tmp/proof.webp (1024x576, 38.4 KB)\n",
		},
		{
			name:   "grayscale",
			result: protocol.MediaShotResult{Path: "/tmp/proof.webp", FileSizeBytes: 39322, Width: 1024, Height: 576, Gray: true},
			want:   "OK: saved /tmp/proof.webp (1024x576, 38.4 KB, grayscale)\n",
		},
		{
			name:   "som badges",
			result: protocol.MediaShotResult{Path: "/tmp/som_test.webp", FileSizeBytes: 42189, Width: 1024, Height: 576, SomBadgeCount: 18},
			want:   "OK: saved /tmp/som_test.webp (1024x576, 41.2 KB, 18 badges)\n",
		},
		{
			name:   "gray and badges",
			result: protocol.MediaShotResult{Path: "/tmp/both.webp", FileSizeBytes: 42189, Width: 800, Height: 600, Gray: true, SomBadgeCount: 3},
			want:   "OK: saved /tmp/both.webp (800x600, 41.2 KB, grayscale, 3 badges)\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := formatShot(tt.result); got != tt.want {
				t.Errorf("formatShot() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestValidateShotOptions(t *testing.T) {
	for _, quality := range []int{1, 65, 100} {
		if err := validateShotOptions(quality, 1024); err != nil {
			t.Errorf("validateShotOptions(%d, 1024) error = %v, want nil", quality, err)
		}
	}
	for _, quality := range []int{0, -1, 101, 200} {
		if err := validateShotOptions(quality, 1024); err == nil {
			t.Errorf("validateShotOptions(%d, 1024) error = nil, want error", quality)
		}
	}

	for _, maxWidth := range []int{256, 1024, 4096} {
		if err := validateShotOptions(65, maxWidth); err != nil {
			t.Errorf("validateShotOptions(65, %d) error = %v, want nil", maxWidth, err)
		}
	}
	for _, maxWidth := range []int{0, 255, 4097} {
		if err := validateShotOptions(65, maxWidth); err == nil {
			t.Errorf("validateShotOptions(65, %d) error = nil, want error", maxWidth)
		}
	}
}

func TestFormatRecStart(t *testing.T) {
	result := protocol.MediaRecStartResult{Started: true, FPS: 2, TabID: 1}
	if got, want := formatRecStart(result), "OK: recording at 2fps\n"; got != want {
		t.Errorf("formatRecStart() = %q, want %q", got, want)
	}

	result.FPS = 1
	if got, want := formatRecStart(result), "OK: recording at 1fps\n"; got != want {
		t.Errorf("formatRecStart(1fps) = %q, want %q", got, want)
	}
}

func TestValidateFPS(t *testing.T) {
	for _, fps := range []int{1, 2} {
		if err := validateFPS(fps); err != nil {
			t.Errorf("validateFPS(%d) error = %v, want nil", fps, err)
		}
	}
	for _, fps := range []int{0, -1, 3, 30} {
		if err := validateFPS(fps); err == nil {
			t.Errorf("validateFPS(%d) error = nil, want error", fps)
		}
	}
}

func TestFormatRecStop(t *testing.T) {
	grid := protocol.MediaRecStopResult{
		Path:           "/tmp/storyboard.webp",
		Format:         "webp",
		FramesCaptured: 12,
		GridDimensions: "3x4",
		Width:          3072,
		Height:         2304,
		FileSizeBytes:  55501,
		DurationMs:     6000,
	}
	if got, want := formatRecStop(grid, true), "OK: saved /tmp/storyboard.webp (12 frames, 3x4 grid, 54.2 KB)\n"; got != want {
		t.Errorf("formatRecStop(grid) = %q, want %q", got, want)
	}

	webm := protocol.MediaRecStopResult{
		Path:           "/tmp/task_recording.webm",
		Format:         "webm",
		FramesCaptured: 8,
		Width:          1024,
		Height:         576,
		FileSizeBytes:  430080,
		DurationMs:     2100,
	}
	if got, want := formatRecStop(webm, false), "OK: saved /tmp/task_recording.webm (8 frames, 2.1s, 420.0 KB)\n"; got != want {
		t.Errorf("formatRecStop(webm) = %q, want %q", got, want)
	}
}

func TestResolveOutputPath(t *testing.T) {
	if got, err := resolveOutputPath(""); err != nil || got != "" {
		t.Errorf("resolveOutputPath(\"\") = %q, %v; want empty, nil", got, err)
	}

	got, err := resolveOutputPath(filepath.Join("tmp", "out.webp"))
	if err != nil {
		t.Fatalf("resolveOutputPath(relative) error = %v", err)
	}
	want, err := filepath.Abs(filepath.Join("tmp", "out.webp"))
	if err != nil {
		t.Fatal(err)
	}
	if got != want || !filepath.IsAbs(got) {
		t.Errorf("resolveOutputPath(relative) = %q, want absolute %q", got, want)
	}

	abs := filepath.Join(t.TempDir(), "proof.webp")
	if got, err := resolveOutputPath(abs); err != nil || got != abs {
		t.Errorf("resolveOutputPath(absolute) = %q, %v; want %q, nil", got, err, abs)
	}
}
