package media

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math"
	"time"

	"github.com/gen2brain/webp"
	xdraw "golang.org/x/image/draw"
	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"
)

const (
	// GridColumns and GridRows describe the fixed contact-sheet layout.
	GridColumns = 3
	GridRows    = 4
	// GridWidth and GridHeight are the contact-sheet canvas dimensions.
	GridWidth  = 1024
	GridHeight = 768

	gridCellWidth  = 341
	gridCellHeight = 192
	gridTileWidth  = 336
	gridTileHeight = 186
	gridQuality    = 65
)

var (
	gridBackground     = color.NRGBA{R: 15, G: 23, B: 42, A: 255}
	gridTileBackground = color.NRGBA{R: 30, G: 41, B: 59, A: 255}
	gridTimestampBack  = color.NRGBA{R: 15, G: 23, B: 42, A: 220}
	gridTimestampText  = color.NRGBA{R: 250, G: 204, B: 21, A: 255}
)

// ContactSheetResult is an encoded 3x4 storyboard.
type ContactSheetResult struct {
	Data           []byte
	Width          int
	Height         int
	Tiles          int
	GridDimensions string
}

// BuildContactSheet composes up to 12 evenly sampled frames into a 1024x768
// WebP storyboard with elapsed timestamps burned into each tile.
func BuildContactSheet(frames []Frame) (ContactSheetResult, error) {
	if len(frames) == 0 {
		return ContactSheetResult{}, fmt.Errorf("no frames to compose")
	}
	sampled := sampleFrames(frames, GridColumns*GridRows)

	canvas := image.NewRGBA(image.Rect(0, 0, GridWidth, GridHeight))
	draw.Draw(canvas, canvas.Bounds(), image.NewUniform(gridBackground), image.Point{}, draw.Src)

	tiles := 0
	for i, frame := range sampled {
		src, err := png.Decode(bytes.NewReader(frame.Data))
		if err != nil {
			return ContactSheetResult{}, fmt.Errorf("decode frame %d: %w", i+1, err)
		}

		row := i / GridColumns
		col := i % GridColumns
		cellX := col * gridCellWidth
		cellY := row * gridCellHeight

		tileX := cellX + (gridCellWidth-gridTileWidth)/2
		tileY := cellY + (gridCellHeight-gridTileHeight)/2
		tile := image.Rect(tileX, tileY, tileX+gridTileWidth, tileY+gridTileHeight)
		draw.Draw(canvas, tile, image.NewUniform(gridTileBackground), image.Point{}, draw.Src)

		w, h := fitSize(src.Bounds().Dx(), src.Bounds().Dy(), gridTileWidth, gridTileHeight)
		dst := image.Rect(
			tileX+(gridTileWidth-w)/2,
			tileY+(gridTileHeight-h)/2,
			tileX+(gridTileWidth-w)/2+w,
			tileY+(gridTileHeight-h)/2+h,
		)
		xdraw.CatmullRom.Scale(canvas, dst, src, src.Bounds(), draw.Over, nil)

		drawTimestamp(canvas, tile, frame.Elapsed)
		tiles++
	}

	var buf bytes.Buffer
	if err := webp.Encode(&buf, canvas, webp.Options{Quality: gridQuality, Method: webpMethod}); err != nil {
		return ContactSheetResult{}, fmt.Errorf("encode contact sheet: %w", err)
	}

	return ContactSheetResult{
		Data:           buf.Bytes(),
		Width:          GridWidth,
		Height:         GridHeight,
		Tiles:          tiles,
		GridDimensions: fmt.Sprintf("%dx%d", GridColumns, GridRows),
	}, nil
}

// sampleFrames picks at most max frames spread evenly across the recording so
// the storyboard shows the whole flow, including the final state.
func sampleFrames(frames []Frame, max int) []Frame {
	if max <= 0 || len(frames) <= max {
		return frames
	}
	out := make([]Frame, max)
	for i := 0; i < max; i++ {
		idx := int(math.Round(float64(i) * float64(len(frames)-1) / float64(max-1)))
		if idx < 0 {
			idx = 0
		}
		if idx >= len(frames) {
			idx = len(frames) - 1
		}
		out[i] = frames[idx]
	}
	return out
}

func fitSize(w, h, maxW, maxH int) (int, int) {
	if w <= 0 || h <= 0 {
		return 1, 1
	}
	if w*maxH > h*maxW {
		nh := int(math.Round(float64(h) * float64(maxW) / float64(w)))
		if nh < 1 {
			nh = 1
		}
		return maxW, nh
	}
	nw := int(math.Round(float64(w) * float64(maxH) / float64(h)))
	if nw < 1 {
		nw = 1
	}
	return nw, maxH
}

func drawTimestamp(dst *image.RGBA, tile image.Rectangle, elapsed time.Duration) {
	total := int(elapsed.Seconds())
	if total < 0 {
		total = 0
	}
	label := fmt.Sprintf("[%02d:%02d]", total/60, total%60)

	face := basicfont.Face7x13
	ascent := face.Metrics().Ascent.Ceil()
	textW := font.MeasureString(face, label).Ceil()

	boxW := textW + 6
	boxH := 15
	x := tile.Min.X + 4
	y := tile.Max.Y - 4 - boxH
	box := image.Rect(x, y, x+boxW, y+boxH)

	draw.Draw(dst, box, image.NewUniform(gridTimestampBack), image.Point{}, draw.Over)
	d := &font.Drawer{
		Dst:  dst,
		Src:  image.NewUniform(gridTimestampText),
		Face: face,
		Dot:  fixed.P(x+3, y+1+ascent),
	}
	d.DrawString(label)
}
