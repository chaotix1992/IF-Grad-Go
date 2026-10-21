package process

import (
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"golang.org/x/image/tiff"

	"if-grad-go/internal/gradient"
)

// Format is an output image format.
type Format string

const (
	TIFF Format = "tiff"
	PNG  Format = "png"
)

// ParseFormat accepts tiff/tif or png.
func ParseFormat(s string) (Format, error) {
	switch strings.ToLower(s) {
	case "tiff", "tif":
		return TIFF, nil
	case "png":
		return PNG, nil
	}
	return "", fmt.Errorf("unknown format %q (want tiff or png)", s)
}

// Ext is the file extension written for f.
func (f Format) Ext() string {
	if f == PNG {
		return ".png"
	}
	return ".tif"
}

// Encode writes img in format f.
func (f Format) Encode(w io.Writer, img image.Image) error {
	if f == PNG {
		return png.Encode(w, img)
	}
	return tiff.Encode(w, img, &tiff.Options{Compression: tiff.Deflate})
}

// imageExts are the input extensions picked up when a folder is given.
var imageExts = map[string]bool{
	".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".tif": true, ".tiff": true,
}

// IsImageName reports whether name has a supported image extension.
func IsImageName(name string) bool {
	return imageExts[strings.ToLower(filepath.Ext(name))]
}

// Load decodes the image at path.
func Load(path string) (image.Image, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return img, nil
}

// save encodes img to path in format f.
func save(path string, f Format, img image.Image) error {
	out, err := os.Create(path)
	if err != nil {
		return err
	}
	if err := f.Encode(out, img); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// ExpandPaths replaces every directory in args with the supported images it
// contains (not recursive). Plain files are kept as given.
func ExpandPaths(args []string) ([]string, error) {
	var files []string
	for _, a := range args {
		info, err := os.Stat(a)
		if err != nil {
			return nil, err
		}
		if !info.IsDir() {
			files = append(files, a)
			continue
		}
		entries, err := os.ReadDir(a)
		if err != nil {
			return nil, err
		}
		for _, e := range entries {
			if !e.IsDir() && IsImageName(e.Name()) {
				files = append(files, filepath.Join(a, e.Name()))
			}
		}
	}
	return files, nil
}

// Mode selects what a conversion produces.
type Mode string

const (
	ModeRecolor Mode = "recolor"
	ModeMask    Mode = "mask"
)

// ParseMode accepts recolor or mask.
func ParseMode(s string) (Mode, error) {
	switch Mode(s) {
	case ModeRecolor, ModeMask:
		return Mode(s), nil
	}
	return "", fmt.Errorf("unknown mode %q (want recolor or mask)", s)
}

// Options configures a conversion.
type Options struct {
	Mode      Mode
	Channel   Channel
	Min       uint8 // recolor: start of the gradient; mask: threshold
	Gradient  gradient.Gradient
	Histogram bool // also write a histogram file per input
	Format    Format
	OutDir    string
}

// Output is the in-memory result of converting one image.
type Output struct {
	Image     image.Image
	Positive  int // mask mode: pixels above the threshold
	Total     int
	Histogram *Histogram
}

// Convert applies opt to img.
func Convert(img image.Image, opt Options) Output {
	p := Intensity(img, opt.Channel)
	out := Output{Total: len(p.Pix), Histogram: p.Histogram()}
	if opt.Mode == ModeMask {
		out.Image, out.Positive = p.Mask(opt.Min)
	} else {
		out.Image = p.Recolor(opt.Min, &opt.Gradient.LUT)
	}
	return out
}

// Result describes one converted file.
type Result struct {
	Input         string
	Output        string
	HistogramFile string
	Positive      int
	Total         int
	Err           error
}

// Percent is the share of positive pixels in mask mode.
func (r Result) Percent() float64 {
	if r.Total == 0 {
		return 0
	}
	return float64(r.Positive) / float64(r.Total) * 100
}

// File converts the image at path and writes the results to opt.OutDir.
func File(path string, opt Options) Result {
	res := Result{Input: path}
	img, err := Load(path)
	if err != nil {
		res.Err = err
		return res
	}
	out := Convert(img, opt)
	res.Positive, res.Total = out.Positive, out.Total

	prefix := "converted_"
	if opt.Mode == ModeMask {
		prefix = "masked_"
	}
	base := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	res.Output = filepath.Join(opt.OutDir, prefix+base+opt.Format.Ext())
	if res.Err = save(res.Output, opt.Format, out.Image); res.Err != nil {
		return res
	}
	if opt.Histogram {
		res.HistogramFile = filepath.Join(opt.OutDir, "data_"+base+".tsv")
		res.Err = writeHistogram(res.HistogramFile, out.Histogram)
	}
	return res
}

func writeHistogram(path string, h *Histogram) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	if err := h.WriteTSV(f); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// Run converts all paths in parallel. Results keep the order of paths;
// progress, if non-nil, is called after each file finishes.
func Run(paths []string, opt Options, progress func(Result)) ([]Result, error) {
	if err := os.MkdirAll(opt.OutDir, 0o755); err != nil {
		return nil, err
	}
	results := make([]Result, len(paths))
	jobs := make(chan int)
	var mu sync.Mutex
	var wg sync.WaitGroup
	for range min(runtime.NumCPU(), len(paths)) {
		wg.Go(func() {
			for i := range jobs {
				results[i] = File(paths[i], opt)
				if progress != nil {
					mu.Lock()
					progress(results[i])
					mu.Unlock()
				}
			}
		})
	}
	for i := range paths {
		jobs <- i
	}
	close(jobs)
	wg.Wait()
	return results, nil
}
