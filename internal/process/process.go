// Package process converts fluorescence images: recolouring onto a gradient,
// thresholding into masks and collecting intensity histograms.
package process

import (
	"fmt"
	"image"
	"image/color"
	"io"
	"math"

	"if-grad-go/internal/gradient"
)

// Channel selects which intensity of a pixel is evaluated.
type Channel int

const (
	Red Channel = iota
	Green
	Blue
	Gray // ITU-R BT.601 luma, identical to Pillow's "L" conversion for 8-bit input
)

var channelNames = []string{"r", "g", "b", "gray"}

func (c Channel) String() string { return channelNames[c] }

// ParseChannel accepts r, g, b or gray (and the long forms red, green, blue).
func ParseChannel(s string) (Channel, error) {
	switch s {
	case "r", "red":
		return Red, nil
	case "g", "green":
		return Green, nil
	case "b", "blue":
		return Blue, nil
	case "gray", "grey":
		return Gray, nil
	}
	return 0, fmt.Errorf("unknown channel %q (want r, g, b or gray)", s)
}

// Plane holds one intensity per pixel on a 16-bit scale, row-major over Rect.
// 8-bit images are stored as v*257, so v8 == v16/257 exactly.
type Plane struct {
	Rect image.Rectangle
	Pix  []uint16
}

// Intensity extracts the intensity of ch from every pixel of img.
func Intensity(img image.Image, ch Channel) *Plane {
	r := img.Bounds()
	p := &Plane{Rect: r, Pix: make([]uint16, r.Dx()*r.Dy())}
	deep := Is16Bit(img)

	// Paletted images (like the example PNGs) only need one lookup per palette entry.
	if pal, ok := img.(*image.Paletted); ok {
		table := make([]uint16, len(pal.Palette))
		for i, c := range pal.Palette {
			table[i] = value(c, ch, false)
		}
		i := 0
		for y := r.Min.Y; y < r.Max.Y; y++ {
			for _, idx := range pal.Pix[pal.PixOffset(r.Min.X, y):pal.PixOffset(r.Max.X, y)] {
				p.Pix[i] = table[idx]
				i++
			}
		}
		return p
	}

	i := 0
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			p.Pix[i] = value(img.At(x, y), ch, deep)
			i++
		}
	}
	return p
}

// value returns the 16-bit intensity of c. For 8-bit sources the luma is
// computed on 8-bit values with Pillow's integer formula, then scaled.
func value(c color.Color, ch Channel, deep bool) uint16 {
	n := color.NRGBA64Model.Convert(c).(color.NRGBA64)
	switch ch {
	case Red:
		return n.R
	case Green:
		return n.G
	case Blue:
		return n.B
	}
	if deep {
		return uint16((19595*uint32(n.R) + 38470*uint32(n.G) + 7471*uint32(n.B) + 1<<15) >> 16)
	}
	r, g, b := uint32(n.R>>8), uint32(n.G>>8), uint32(n.B>>8)
	return uint16((19595*r+38470*g+7471*b+0x8000)>>16) * 257
}

// Is16Bit reports whether img stores more than 8 bits per channel.
func Is16Bit(img image.Image) bool {
	switch img.ColorModel() {
	case color.RGBA64Model, color.NRGBA64Model, color.Gray16Model:
		return true
	}
	return false
}

// stretchTable maps every 16-bit intensity to a LUT index. Intensities below
// min get index 0; the rest are stretched over the full 0..255 range exactly
// like recolor_rb.py: round((v - min) * 255/(255 - min)).
func stretchTable(min uint8) *[65536]uint8 {
	var t [65536]uint8
	steps := 255 / float64(255-int(min))
	for v16 := range t {
		v := float64(v16) / 257
		switch {
		case v < float64(min):
			t[v16] = 0
		case min == 255: // only saturated pixels remain; give them the top colour
			t[v16] = 255
		default:
			t[v16] = uint8(math.Min(255, math.RoundToEven((v-float64(min))*steps)))
		}
	}
	return &t
}

// Recolor maps the plane onto lut, stretched from min. The result is
// paletted with the LUT as palette, so it stays lossless in PNG and TIFF.
func (p *Plane) Recolor(min uint8, lut *gradient.LUT) *image.Paletted {
	pal := make(color.Palette, len(lut))
	for i, c := range lut {
		pal[i] = c
	}
	out := image.NewPaletted(p.Rect, pal)
	t := stretchTable(min)
	for i, v := range p.Pix {
		out.Pix[i] = t[v]
	}
	return out
}

// Mask marks pixels above threshold black and the rest white, as mask.py did,
// and returns how many pixels are positive (black).
func (p *Plane) Mask(threshold uint8) (img *image.Gray, positive int) {
	img = image.NewGray(p.Rect)
	limit := uint16(threshold) * 257 // v16/257 <= t  <=>  v16 <= 257t
	for i, v := range p.Pix {
		if v <= limit {
			img.Pix[i] = 255
		} else {
			positive++
		}
	}
	return img, positive
}

// Histogram counts pixels per 8-bit intensity level.
type Histogram [256]int

// Histogram bins the plane into 256 levels. 16-bit data is binned by its
// upper byte.
func (p *Plane) Histogram() *Histogram {
	var h Histogram
	for _, v := range p.Pix {
		h[v>>8]++
	}
	return &h
}

// WriteTSV writes the histogram as two tab-separated columns with a header.
func (h *Histogram) WriteTSV(w io.Writer) error {
	if _, err := fmt.Fprintln(w, "value\tcount"); err != nil {
		return err
	}
	for v, n := range h {
		if _, err := fmt.Fprintf(w, "%d\t%d\n", v, n); err != nil {
			return err
		}
	}
	return nil
}
