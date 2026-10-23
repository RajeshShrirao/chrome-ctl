package media

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gen2brain/webp"
)

func testPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetRGBA(x, y, color.RGBA{
				R: uint8(x * 255 / w),
				G: uint8(y * 255 / h),
				B: uint8((x + y) * 255 / (w + h)),
				A: 255,
			})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode test PNG: %v", err)
	}
	return buf.Bytes()
}

func decodeWebP(t *testing.T, data []byte) image.Image {
	t.Helper()
	img, err := webp.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("decode WebP: %v", err)
	}
	return img
}

func checkWebPMagic(t *testing.T, data []byte) {
	t.Helper()
	if len(data) < 12 {
		t.Fatalf("WebP data too short: %d bytes", len(data))
	}
	if string(data[0:4]) != "RIFF" || string(data[8:12]) != "WEBP" {
		t.Fatalf("WebP magic = %q/%q, want RIFF/WEBP", data[0:4], data[8:12])
	}
}

func TestProcessPNGDownscalesAndEncodes(t *testing.T) {
	src := testPNG(t, 1600, 900)
	result, err := ProcessPNG(src, 1600, ImageOptions{})
	if err != nil {
		t.Fatalf("ProcessPNG: %v", err)
	}
	if result.Width != 1024 {
		t.Fatalf("width = %d, want 1024", result.Width)
	}
	if result.Height != 576 {
		t.Fatalf("height = %d, want 576 (aspect preserved)", result.Height)
	}
	checkWebPMagic(t, result.Data)

	img := decodeWebP(t, result.Data)
	if got := img.Bounds().Dx(); got != 1024 {
		t.Fatalf("decoded width = %d, want 1024", got)
	}
	if got := img.Bounds().Dy(); got != 576 {
		t.Fatalf("decoded height = %d, want 576", got)
	}
}

func TestProcessPNGNeverUpscales(t *testing.T) {
	src := testPNG(t, 640, 480)
	result, err := ProcessPNG(src, 640, ImageOptions{MaxWidth: 2048})
	if err != nil {
		t.Fatalf("ProcessPNG: %v", err)
	}
	if result.Width != 640 || result.Height != 480 {
		t.Fatalf("size = %dx%d, want 640x480", result.Width, result.Height)
	}
}

func TestProcessPNGClampsSmallWidth(t *testing.T) {
	src := testPNG(t, 1600, 900)
	result, err := ProcessPNG(src, 1600, ImageOptions{MaxWidth: 10})
	if err != nil {
		t.Fatalf("ProcessPNG: %v", err)
	}
	if result.Width != MinMaxWidth {
		t.Fatalf("width = %d, want clamped %d", result.Width, MinMaxWidth)
	}
	if result.Height != 144 {
		t.Fatalf("height = %d, want 144 (aspect preserved)", result.Height)
	}
}

func TestProcessPNGGray(t *testing.T) {
	src := testPNG(t, 800, 600)
	result, err := ProcessPNG(src, 800, ImageOptions{Gray: true})
	if err != nil {
		t.Fatalf("ProcessPNG: %v", err)
	}
	img := decodeWebP(t, result.Data)
	var sumDelta, samples int
	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y += 3 {
		for x := b.Min.X; x < b.Max.X; x += 3 {
			r, g, bl, _ := img.At(x, y).RGBA()
			sumDelta += absInt(int(r>>8)-int(g>>8)) + absInt(int(g>>8)-int(bl>>8))
			samples += 2
		}
	}
	if samples == 0 {
		t.Fatal("no samples decoded")
	}
	if avg := float64(sumDelta) / float64(samples); avg > 10 {
		t.Fatalf("average channel delta = %.2f, want a gray-ish artifact", avg)
	}
}

func TestProcessPNGBadges(t *testing.T) {
	src := testPNG(t, 1600, 900)

	plain, err := ProcessPNG(src, 1600, ImageOptions{})
	if err != nil {
		t.Fatalf("ProcessPNG plain: %v", err)
	}
	withBadges, err := ProcessPNG(src, 1600, ImageOptions{
		Badges: []Badge{
			{ID: 1, X: 100, Y: 100, W: 120, H: 40},
			{ID: 2, X: 700, Y: 300, W: 90, H: 30},
			// Entirely outside the viewport: must be skipped.
			{ID: 3, X: 2000, Y: 100, W: 40, H: 20},
		},
	})
	if err != nil {
		t.Fatalf("ProcessPNG badges: %v", err)
	}
	if withBadges.BadgeCount != 2 {
		t.Fatalf("badge count = %d, want 2", withBadges.BadgeCount)
	}

	plainImg := decodeWebP(t, plain.Data)
	badgeImg := decodeWebP(t, withBadges.Data)
	if plainImg.Bounds() != badgeImg.Bounds() {
		t.Fatalf("bounds differ: %v vs %v", plainImg.Bounds(), badgeImg.Bounds())
	}
	changed := 0
	b := plainImg.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			pr, pg, pb, _ := plainImg.At(x, y).RGBA()
			br, bg, bb, _ := badgeImg.At(x, y).RGBA()
			if absInt(int(pr>>8)-int(br>>8)) > 24 ||
				absInt(int(pg>>8)-int(bg>>8)) > 24 ||
				absInt(int(pb>>8)-int(bb>>8)) > 24 {
				changed++
			}
		}
	}
	if changed < 100 {
		t.Fatalf("changed pixels = %d, want badges to alter the image", changed)
	}
}

func TestDrawBadgesCollisionShift(t *testing.T) {
	dst := image.NewRGBA(image.Rect(0, 0, 400, 300))
	n := drawBadges(dst, []Badge{
		{ID: 1, X: 10, Y: 10, W: 40, H: 20},
		{ID: 2, X: 10, Y: 10, W: 40, H: 20},
	}, 1)
	if n != 2 {
		t.Fatalf("drawn = %d, want 2", n)
	}
}

func TestBuildContactSheet(t *testing.T) {
	frames := []Frame{
		{Data: testPNG(t, 640, 360), Elapsed: 0},
		{Data: testPNG(t, 640, 360), Elapsed: 1500 * time.Millisecond},
		{Data: testPNG(t, 640, 360), Elapsed: 3 * time.Second},
	}
	sheet, err := BuildContactSheet(frames)
	if err != nil {
		t.Fatalf("BuildContactSheet: %v", err)
	}
	if sheet.Width != 1024 || sheet.Height != 768 {
		t.Fatalf("size = %dx%d, want 1024x768", sheet.Width, sheet.Height)
	}
	if sheet.Tiles != 3 {
		t.Fatalf("tiles = %d, want 3", sheet.Tiles)
	}
	if sheet.GridDimensions != "3x4" {
		t.Fatalf("grid = %q, want 3x4", sheet.GridDimensions)
	}
	checkWebPMagic(t, sheet.Data)
	img := decodeWebP(t, sheet.Data)
	if img.Bounds().Dx() != 1024 || img.Bounds().Dy() != 768 {
		t.Fatalf("decoded size = %v, want 1024x768", img.Bounds())
	}
}

func TestSampleFramesEvenly(t *testing.T) {
	frames := make([]Frame, 24)
	for i := range frames {
		frames[i] = Frame{Elapsed: time.Duration(i) * time.Second}
	}
	sampled := sampleFrames(frames, 12)
	if len(sampled) != 12 {
		t.Fatalf("sampled %d frames, want 12", len(sampled))
	}
	if sampled[0].Elapsed != 0 {
		t.Fatalf("first frame = %v, want start", sampled[0].Elapsed)
	}
	if last := sampled[len(sampled)-1].Elapsed; last != 23*time.Second {
		t.Fatalf("last frame = %v, want final frame", last)
	}
}

func TestBuildWebM(t *testing.T) {
	if _, err := FFmpegPath(); err != nil {
		t.Skipf("skipping WebM test: %v", err)
	}
	frames := []Frame{
		{Data: testPNG(t, 320, 180), Elapsed: 0},
		{Data: testPNG(t, 320, 180), Elapsed: time.Second},
		{Data: testPNG(t, 320, 180), Elapsed: 2 * time.Second},
	}
	out := filepath.Join(t.TempDir(), "recording.webm")
	result, err := BuildWebM(frames, 2, out)
	if err != nil {
		t.Fatalf("BuildWebM: %v", err)
	}
	if result.Width != 320 || result.Height != 180 {
		t.Fatalf("size = %dx%d, want 320x180", result.Width, result.Height)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	if len(data) < 4 {
		t.Fatalf("output too short: %d bytes", len(data))
	}
	if magic := binary.BigEndian.Uint32(data[:4]); magic != 0x1A45DFA3 {
		t.Fatalf("magic = 0x%08X, want EBML 0x1A45DFA3", magic)
	}
	if len(data) < 100 {
		t.Fatalf("output size = %d bytes, want non-trivial WebM", len(data))
	}
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
