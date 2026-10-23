package media

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"math"

	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"
)

// Badge is one Set-of-Mark box in CSS pixels relative to the viewport.
type Badge struct {
	ID int
	X  int
	Y  int
	W  int
	H  int
}

const (
	badgeHeight    = 16
	badgePaddingX  = 4
	badgeShiftY    = 14
	badgeMaxShifts = 32
)

var (
	badgeBackground = color.NRGBA{R: 15, G: 23, B: 42, A: 230}
	badgeBorder     = color.NRGBA{R: 255, G: 255, B: 255, A: 204}
	badgeText       = color.NRGBA{R: 250, G: 204, B: 21, A: 255}
)

// drawBadges paints numbered markers at each element's top-left corner and
// returns the number actually rendered. Coordinates are scaled from CSS pixels
// to the final image; boxes entirely outside the image are skipped.
func drawBadges(dst *image.RGBA, badges []Badge, scale float64) int {
	if len(badges) == 0 {
		return 0
	}
	if scale <= 0 {
		scale = 1
	}
	face := basicfont.Face7x13
	metrics := face.Metrics()
	ascent := metrics.Ascent.Ceil()
	descent := metrics.Descent.Ceil()

	var prev image.Rectangle
	havePrev := false
	drawn := 0
	imgW, imgH := dst.Bounds().Dx(), dst.Bounds().Dy()

	for _, b := range badges {
		bx := int(math.Round(float64(b.X) * scale))
		by := int(math.Round(float64(b.Y) * scale))
		bw := int(math.Round(float64(b.W) * scale))
		bh := int(math.Round(float64(b.H) * scale))
		if bx >= imgW || by >= imgH || bx+bw <= 0 || by+bh <= 0 {
			continue
		}

		label := fmt.Sprintf("[%d]", b.ID)
		textW := font.MeasureString(face, label).Ceil()
		rect := image.Rect(bx, by, bx+textW+2*badgePaddingX, by+badgeHeight)

		// Collision avoidance: if the marker mostly covers the one drawn
		// immediately before it, slide downwards until it does not.
		if havePrev {
			for shifts := 0; shifts < badgeMaxShifts && overlapMoreThanHalf(rect, prev); shifts++ {
				rect = rect.Add(image.Pt(0, badgeShiftY))
			}
		}

		fillRect(dst, rect, badgeBackground)
		strokeRect(dst, rect, badgeBorder)

		baseline := rect.Min.Y + (badgeHeight+ascent-descent)/2
		d := &font.Drawer{
			Dst:  dst,
			Src:  image.NewUniform(badgeText),
			Face: face,
			Dot:  fixed.P(rect.Min.X+badgePaddingX, baseline),
		}
		d.DrawString(label)

		prev = rect
		havePrev = true
		drawn++
	}
	return drawn
}

func fillRect(dst *image.RGBA, rect image.Rectangle, c color.Color) {
	draw.Draw(dst, rect, image.NewUniform(c), image.Point{}, draw.Over)
}

func strokeRect(dst *image.RGBA, rect image.Rectangle, c color.Color) {
	if rect.Dx() < 2 || rect.Dy() < 2 {
		return
	}
	fillRect(dst, image.Rect(rect.Min.X, rect.Min.Y, rect.Max.X, rect.Min.Y+1), c)
	fillRect(dst, image.Rect(rect.Min.X, rect.Max.Y-1, rect.Max.X, rect.Max.Y), c)
	fillRect(dst, image.Rect(rect.Min.X, rect.Min.Y+1, rect.Min.X+1, rect.Max.Y-1), c)
	fillRect(dst, image.Rect(rect.Max.X-1, rect.Min.Y+1, rect.Max.X, rect.Max.Y-1), c)
}

func overlapMoreThanHalf(a, b image.Rectangle) bool {
	inter := a.Intersect(b)
	if inter.Empty() {
		return false
	}
	aArea := a.Dx() * a.Dy()
	if aArea <= 0 {
		return false
	}
	return inter.Dx()*inter.Dy()*2 > aArea
}
