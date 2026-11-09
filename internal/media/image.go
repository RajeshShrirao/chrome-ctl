// Package media transforms raw viewport captures into token-efficient WebP
// artifacts: single screenshots, contact sheets, and WebM recordings.
package media

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"math"

	"github.com/gen2brain/webp"
	xdraw "golang.org/x/image/draw"
)

const (
	// DefaultMaxWidth is the default screenshot width.
	DefaultMaxWidth = 1024
	// MinMaxWidth is the smallest accepted screenshot width.
	MinMaxWidth = 256
	// MaxMaxWidth is the largest accepted screenshot width.
	MaxMaxWidth = 4096
	// DefaultQuality is the default lossy WebP quality.
	DefaultQuality = 65
	// webpMethod is the libwebp quality/speed trade-off (0=fast, 6=slow).
	webpMethod = 4
)

// ImageOptions controls screenshot post-processing.
type ImageOptions struct {
	// MaxWidth caps the output width; 0 means DefaultMaxWidth. The image is
	// never upscaled.
	MaxWidth int
	// Quality is the lossy WebP quality; 0 means DefaultQuality.
	Quality int
	// Gray converts the finished artifact to grayscale.
	Gray bool
	// Exact disables downscaling and keeps original capture dimensions.
	Exact bool
	// Badges are Set-of-Mark boxes in CSS pixels relative to the viewport.
	Badges []Badge
}

// ImageResult is an encoded screenshot plus the metadata reported to the CLI.
type ImageResult struct {
	Data       []byte
	Width      int
	Height     int
	Scale      float64
	BadgeCount int
}

// ProcessPNG decodes a PNG, downscales it to the requested width, draws any
// Set-of-Mark badges, optionally converts it to grayscale, and encodes WebP.
func ProcessPNG(pngBytes []byte, viewportWidth int, opts ImageOptions) (ImageResult, error) {
	if len(pngBytes) == 0 {
		return ImageResult{}, errors.New("empty PNG data")
	}
	src, err := png.Decode(bytes.NewReader(pngBytes))
	if err != nil {
		return ImageResult{}, fmt.Errorf("decode PNG: %w", err)
	}
	srcBounds := src.Bounds()
	srcW, srcH := srcBounds.Dx(), srcBounds.Dy()
	if srcW <= 0 || srcH <= 0 {
		return ImageResult{}, errors.New("PNG has empty bounds")
	}

	finalW, finalH := srcW, srcH
	if !opts.Exact {
		maxWidth := normalizeMaxWidth(opts.MaxWidth)
		if srcW > maxWidth {
			finalW = maxWidth
			finalH = int(math.Round(float64(srcH) * float64(maxWidth) / float64(srcW)))
			if finalH < 1 {
				finalH = 1
			}
		}
	}

	dst := image.NewRGBA(image.Rect(0, 0, finalW, finalH))
	if finalW == srcW && finalH == srcH {
		draw.Draw(dst, dst.Bounds(), src, srcBounds.Min, draw.Src)
	} else {
		xdraw.CatmullRom.Scale(dst, dst.Bounds(), src, srcBounds, xdraw.Src, nil)
	}

	scale := 1.0
	if viewportWidth > 0 {
		scale = float64(finalW) / float64(viewportWidth)
	}
	badgeCount := drawBadges(dst, opts.Badges, scale)

	if opts.Gray {
		grayscale(dst)
	}

	var buf bytes.Buffer
	if err := webp.Encode(&buf, dst, webp.Options{Quality: normalizeQuality(opts.Quality), Method: webpMethod}); err != nil {
		return ImageResult{}, fmt.Errorf("encode WebP: %w", err)
	}

	return ImageResult{
		Data:       buf.Bytes(),
		Width:      finalW,
		Height:     finalH,
		Scale:      scale,
		BadgeCount: badgeCount,
	}, nil
}

func normalizeMaxWidth(v int) int {
	switch {
	case v <= 0:
		return DefaultMaxWidth
	case v < MinMaxWidth:
		return MinMaxWidth
	case v > MaxMaxWidth:
		return MaxMaxWidth
	default:
		return v
	}
}

func normalizeQuality(q int) int {
	switch {
	case q == 0:
		return DefaultQuality
	case q < 1:
		return 1
	case q > 100:
		return 100
	default:
		return q
	}
}

// grayscale applies the luma transform in place so the remainder of the
// pipeline (including WebP encoding) sees a single-channel-looking image.
func grayscale(img *image.RGBA) {
	pix := img.Pix
	for i := 0; i+3 < len(pix); i += 4 {
		y := uint8((299*uint32(pix[i]) + 587*uint32(pix[i+1]) + 114*uint32(pix[i+2])) / 1000)
		pix[i] = y
		pix[i+1] = y
		pix[i+2] = y
	}
}
