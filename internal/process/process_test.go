package process

import (
	"fmt"
	"image"
	"image/color"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"if-grad-go/internal/gradient"
)

// The ref_*.png files were produced by the per-pixel code of recolor_rb.py
// and mask.py on synthetic.png.
func TestMatchesPython(t *testing.T) {
	src, err := Load("../../testdata/synthetic.png")
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		ref string
		opt Options
	}{
		{"ref_mask_r_100.png", Options{Mode: ModeMask, Channel: Red, Min: 100}},
	}
	for _, ch := range []Channel{Green, Gray} {
		for _, m := range []uint8{0, 42, 127} {
			cases = append(cases, struct {
				ref string
				opt Options
			}{fmt.Sprintf("ref_recolor_%v_%d.png", ch, m), Options{Mode: ModeRecolor, Channel: ch, Min: m, Gradient: gradient.Classic()}})
		}
	}
	for _, c := range cases {
		want, err := Load("../../testdata/" + c.ref)
		if err != nil {
			t.Fatal(err)
		}
		got := Convert(src, c.opt).Image
		if n := countDiff(got, want); n > 0 {
			t.Errorf("%s: %d pixels differ", c.ref, n)
		}
	}
}

func countDiff(a, b image.Image) int {
	n := 0
	r := a.Bounds()
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			if color.RGBAModel.Convert(a.At(x, y)) != color.RGBAModel.Convert(b.At(x, y)) {
				n++
			}
		}
	}
	return n
}

func TestSixteenBit(t *testing.T) {
	img := image.NewGray16(image.Rect(0, 0, 3, 1))
	img.Pix = []byte{0x00, 0x00, 0x80, 0x00, 0xff, 0xff}
	p := Intensity(img, Gray)
	if p.Pix[1] != 0x8000 {
		t.Errorf("16-bit value truncated: %#x", p.Pix[1])
	}
	lut := gradient.Gray().LUT
	out := p.Recolor(0, &lut)
	if got := out.Pix; got[0] != 0 || got[1] != 128 || got[2] != 255 {
		t.Errorf("recolor indices = %v", got)
	}
}

func TestMinEdgeCases(t *testing.T) {
	img := image.NewGray(image.Rect(0, 0, 3, 1))
	img.Pix = []byte{0, 254, 255}
	p := Intensity(img, Gray)
	lut := gradient.Classic().LUT
	out := p.Recolor(255, &lut) // used to divide by zero
	if got := out.Pix; got[0] != 0 || got[1] != 0 || got[2] != 255 {
		t.Errorf("min=255 indices = %v", got)
	}
	_, pos := p.Mask(254)
	if pos != 1 {
		t.Errorf("mask positives = %d, want 1", pos)
	}
}

func TestRunWritesFreshHistograms(t *testing.T) {
	in, out := t.TempDir(), filepath.Join(t.TempDir(), "new", "dir")
	for i, v := range []uint8{10, 200} {
		img := image.NewGray(image.Rect(0, 0, 4, 4))
		for j := range img.Pix {
			img.Pix[j] = v
		}
		if err := save(filepath.Join(in, fmt.Sprintf("img%d.png", i)), PNG, img); err != nil {
			t.Fatal(err)
		}
	}
	os.WriteFile(filepath.Join(in, "notes.txt"), []byte("not an image"), 0o644)

	paths, err := ExpandPaths([]string{in})
	if err != nil || len(paths) != 2 {
		t.Fatalf("ExpandPaths = %v, %v", paths, err)
	}
	opt := Options{Mode: ModeRecolor, Channel: Gray, Gradient: gradient.Classic(), Histogram: true, Format: TIFF, OutDir: out}
	results, err := Run(paths, opt, nil)
	if err != nil {
		t.Fatal(err)
	}
	for i, r := range results {
		if r.Err != nil {
			t.Fatal(r.Err)
		}
		if !strings.HasSuffix(r.Output, ".tif") {
			t.Errorf("output %s should end in .tif", r.Output)
		}
		if _, err := Load(r.Output); err != nil {
			t.Errorf("reading back %s: %v", r.Output, err)
		}
		data, _ := os.ReadFile(r.HistogramFile)
		v := []int{10, 200}[i]
		if !strings.Contains(string(data), fmt.Sprintf("\n%d\t16\n", v)) || strings.Count(string(data), "\t16\n") != 1 {
			t.Errorf("histogram %d is not per-file:\n%s", i, data)
		}
	}
}
