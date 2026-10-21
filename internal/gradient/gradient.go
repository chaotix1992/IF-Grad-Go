// Package gradient builds the 256-entry colour lookup tables (LUTs) that
// intensities are mapped onto.
package gradient

import (
	"image"
	"image/color"
	"math"
	"sort"
)

// LUT maps an 8-bit intensity to a colour.
type LUT [256]color.RGBA

// Gradient is a named LUT selectable from the UI and CLI.
type Gradient struct {
	Name  string // identifier used in flags and URLs
	Label string // human-readable name
	LUT   LUT
}

// ColorStop is a colour at a position in [0, 1].
type ColorStop struct {
	Pos     float64
	R, G, B float64 // components in [0, 1]
}

// Stops is a piecewise-linear gradient in the style of matplotlib's
// LinearSegmentedColormap.from_list. Positions must be ascending and span 0..1.
type Stops []ColorStop

// LUT samples the gradient at 256 evenly spaced positions. The arithmetic
// mirrors matplotlib's _create_lookup_table so results match it exactly.
func (s Stops) LUT() LUT {
	var lut LUT
	for i := range lut {
		r, g, b := s.sample(i)
		lut[i] = color.RGBA{to8(r), to8(g), to8(b), 255}
	}
	return lut
}

// sample returns the colour at LUT index i of 256 as floats in [0, 1].
func (s Stops) sample(i int) (r, g, b float64) {
	first, last := s[0], s[len(s)-1]
	switch i {
	case 0:
		return first.R, first.G, first.B
	case 255:
		return last.R, last.G, last.B
	}
	x := linspace256(i)
	// Smallest k with s[k].Pos >= x, like numpy.searchsorted(side="left").
	k := sort.Search(len(s), func(k int) bool { return s[k].Pos >= x })
	lo, hi := s[k-1], s[k]
	d := (x - lo.Pos) / (hi.Pos - lo.Pos)
	return lerp(d, lo.R, hi.R), lerp(d, lo.G, hi.G), lerp(d, lo.B, hi.B)
}

// linspace256 reproduces numpy.linspace(0, 1, 256)[i].
func linspace256(i int) float64 {
	if i == 255 {
		return 1
	}
	return float64(i) * (1.0 / 255)
}

// lerp computes d*(b-a)+a. The explicit conversion stops the compiler from
// fusing the multiply-add, which could change the last bit versus numpy.
func lerp(d, a, b float64) float64 {
	return clamp01(float64(d*(b-a)) + a)
}

func clamp01(v float64) float64 {
	return math.Min(1, math.Max(0, v))
}

// to8 converts a [0, 1] component to 0..255 using Python's round() semantics.
func to8(v float64) uint8 {
	return uint8(math.RoundToEven(clamp01(v) * 255))
}

// Bar renders the LUT as a horizontal strip, dark on the left.
func (l *LUT) Bar(w, h int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for x := range w {
		c := l[x*256/w]
		for y := range h {
			img.SetRGBA(x, y, c)
		}
	}
	return img
}

// gistRainbow is matplotlib's "gist_rainbow" colormap definition.
var gistRainbow = Stops{
	{0.000, 1.00, 0.00, 0.16},
	{0.030, 1.00, 0.00, 0.00},
	{0.215, 1.00, 1.00, 0.00},
	{0.400, 0.00, 1.00, 0.00},
	{0.586, 0.00, 1.00, 1.00},
	{0.770, 0.00, 0.00, 1.00},
	{0.954, 1.00, 0.00, 1.00},
	{1.000, 1.00, 0.00, 0.75},
}

// classicRamp is the number of entries of the black-to-blue lead-in.
const classicRamp = 64

// Classic reproduces the LUT of the original recolor_rb.py exactly:
// entries 0..63 ramp from black to (0,0,252), entries 64..255 run through
// gist_rainbow backwards from blue (position ~0.749) to red (position 0).
func Classic() Gradient {
	rainbow := gistRainbow.LUT()
	var lut LUT
	for i := range lut {
		if i < classicRamp {
			lut[i] = color.RGBA{0, 0, uint8(4 * i), 255}
		} else {
			lut[i] = rainbow[255-i]
		}
	}
	return Gradient{Name: "classic", Label: "IF-Grad classic", LUT: lut}
}

// Turbo uses Google's polynomial approximation of the Turbo colormap
// (Apache-2.0). Same dark → blue → green → yellow → red order as classic,
// but with smoother lightness changes.
func Turbo() Gradient {
	var lut LUT
	for i := range lut {
		x := float64(i) / 255
		r := 0.13572138 + x*(4.61539260+x*(-42.66032258+x*(132.13108234+x*(-152.94239396+x*59.28637943))))
		g := 0.09140261 + x*(2.19418839+x*(4.84296658+x*(-14.18503333+x*(4.27729857+x*2.82956604))))
		b := 0.10667330 + x*(12.64194608+x*(-60.58204836+x*(110.36276771+x*(-89.90310912+x*27.34824973))))
		lut[i] = color.RGBA{to8(r), to8(g), to8(b), 255}
	}
	return Gradient{Name: "turbo", Label: "Turbo", LUT: lut}
}

// Viridis interpolates nine samples of matplotlib's viridis. It is a close
// approximation, not the exact 256-entry table.
func Viridis() Gradient {
	hex := []uint32{0x440154, 0x472c7a, 0x3b528b, 0x2c728e, 0x21918c, 0x28ae80, 0x5ec962, 0xaddc30, 0xfde725}
	s := make(Stops, len(hex))
	for i, h := range hex {
		s[i] = ColorStop{
			Pos: float64(i) / float64(len(hex)-1),
			R:   float64(h>>16&0xff) / 255,
			G:   float64(h>>8&0xff) / 255,
			B:   float64(h&0xff) / 255,
		}
	}
	return Gradient{Name: "viridis", Label: "Viridis", LUT: s.LUT()}
}

// Gray maps intensity straight to gray levels.
func Gray() Gradient {
	var lut LUT
	for i := range lut {
		lut[i] = color.RGBA{uint8(i), uint8(i), uint8(i), 255}
	}
	return Gradient{Name: "gray", Label: "Grayscale", LUT: lut}
}

// All lists the available gradients; the first one is the default.
var All = []Gradient{Classic(), Turbo(), Viridis(), Gray()}

// ByName looks a gradient up by its Name.
func ByName(name string) (Gradient, bool) {
	for _, g := range All {
		if g.Name == name {
			return g, true
		}
	}
	return Gradient{}, false
}
