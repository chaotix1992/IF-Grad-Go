package gradient

import (
	"bufio"
	"fmt"
	"image/color"
	"os"
	"testing"
)

// classic_lut.txt was dumped by running the LUT code of recolor_rb.py
// (matplotlib 3.11, colour 0.1.5).
func TestClassicMatchesPython(t *testing.T) {
	f, err := os.Open("../../testdata/classic_lut.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	lut := Classic().LUT
	sc := bufio.NewScanner(f)
	for i := 0; sc.Scan(); i++ {
		var want color.RGBA
		if _, err := fmt.Sscan(sc.Text(), &want.R, &want.G, &want.B); err != nil {
			t.Fatal(err)
		}
		want.A = 255
		if lut[i] != want {
			t.Errorf("lut[%d] = %v, want %v", i, lut[i], want)
		}
	}
}

func TestByName(t *testing.T) {
	for _, g := range All {
		if got, ok := ByName(g.Name); !ok || got.Name != g.Name {
			t.Errorf("ByName(%q) failed", g.Name)
		}
	}
	if _, ok := ByName("nope"); ok {
		t.Error("ByName found unknown gradient")
	}
}
