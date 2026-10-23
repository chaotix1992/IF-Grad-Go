//go:build js && wasm

// Command wasm is the browser build of IF-Grad. It runs inside a web worker
// (see web/worker.js) and exposes the conversion functions on the global
// ifgrad object. Images are processed locally and never uploaded anywhere.
package main

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/png"
	"path"
	"strings"
	"syscall/js"

	"golang.org/x/image/draw"

	"if-grad-go/internal/gradient"
	"if-grad-go/internal/process"
)

// previewSize is the longest edge of preview images in pixels.
const previewSize = 900

// file is a loaded image. Only the encoded bytes are kept at full resolution;
// they are decoded again for conversion, which keeps memory use low.
type file struct {
	name    string
	data    []byte
	preview image.Image
	hists   [4]*process.Histogram // indexed by process.Channel, from the full image
}

// output is a file produced by the current batch, kept for the zip download.
type output struct {
	name string
	data []byte
}

var (
	files   = map[string]*file{}
	results []output
)

func main() {
	api := js.Global().Get("Object").New()
	export(api, "gradients", gradients)
	export(api, "load", load)
	export(api, "remove", remove)
	export(api, "preview", preview)
	export(api, "histogram", histogram)
	export(api, "reset", reset)
	export(api, "convert", convert)
	export(api, "zip", zipResults)
	js.Global().Set("ifgrad", api)
	if ready := js.Global().Get("ifgradReady"); ready.Type() == js.TypeFunction {
		ready.Invoke()
	}
	select {}
}

// export registers fn as obj[name]. The JS caller receives {value} on success
// and {error} on failure; panics are reported as errors instead of killing
// the program.
func export(obj js.Value, name string, fn func(args []js.Value) (any, error)) {
	obj.Set(name, js.FuncOf(func(this js.Value, args []js.Value) (ret any) {
		defer func() {
			if r := recover(); r != nil {
				ret = map[string]any{"error": fmt.Sprint(r)}
			}
		}()
		v, err := fn(args)
		if err != nil {
			return map[string]any{"error": err.Error()}
		}
		return map[string]any{"value": v}
	}))
}

// gradients lists the available gradients with a 256×1 PNG colour bar each.
func gradients([]js.Value) (any, error) {
	list := make([]any, len(gradient.All))
	for i, g := range gradient.All {
		bar, err := encodePNG(g.LUT.Bar(256, 1))
		if err != nil {
			return nil, err
		}
		list[i] = map[string]any{"name": g.Name, "label": g.Label, "bar": jsBytes(bar)}
	}
	return list, nil
}

// load(id, name, bytes) decodes an image and returns its size and a PNG of
// the scaled-down original.
func load(args []js.Value) (any, error) {
	id, name, data := args[0].String(), path.Base(args[1].String()), goBytes(args[2])
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, errors.New("not a readable image")
	}
	f := &file{name: name, data: data, preview: shrink(img)}
	for ch := range f.hists {
		f.hists[ch] = process.Intensity(img, process.Channel(ch)).Histogram()
	}
	original, err := encodePNG(f.preview)
	if err != nil {
		return nil, err
	}
	files[id] = f
	b := img.Bounds()
	return map[string]any{"width": b.Dx(), "height": b.Dy(), "original": jsBytes(original)}, nil
}

// remove(id) forgets a loaded image.
func remove(args []js.Value) (any, error) {
	delete(files, args[0].String())
	return nil, nil
}

// preview(id, options) returns the converted preview as PNG.
func preview(args []js.Value) (any, error) {
	f, err := lookup(args[0])
	if err != nil {
		return nil, err
	}
	opt, err := parseOptions(args[1])
	if err != nil {
		return nil, err
	}
	png, err := encodePNG(process.Convert(f.preview, opt).Image)
	if err != nil {
		return nil, err
	}
	return jsBytes(png), nil
}

// histogram(id, channel) returns the 256 bin counts of the full-resolution
// image, so the UI can show exact mask percentages while the slider moves.
func histogram(args []js.Value) (any, error) {
	f, err := lookup(args[0])
	if err != nil {
		return nil, err
	}
	ch, err := process.ParseChannel(args[1].String())
	if err != nil {
		return nil, err
	}
	bins := make([]any, len(f.hists[ch]))
	for i, n := range f.hists[ch] {
		bins[i] = n
	}
	return bins, nil
}

// reset starts a new batch of conversions.
func reset([]js.Value) (any, error) {
	results = nil
	return nil, nil
}

// convert(id, options) converts one image at full resolution, adds the
// output to the current batch and returns it.
func convert(args []js.Value) (any, error) {
	f, err := lookup(args[0])
	if err != nil {
		return nil, err
	}
	opt, err := parseOptions(args[1])
	if err != nil {
		return nil, err
	}
	img, _, err := image.Decode(bytes.NewReader(f.data))
	if err != nil {
		return nil, err
	}
	out := process.Convert(img, opt)

	prefix := "converted_"
	if opt.Mode == process.ModeMask {
		prefix = "masked_"
	}
	base := strings.TrimSuffix(f.name, path.Ext(f.name))
	var buf bytes.Buffer
	if err := opt.Format.Encode(&buf, out.Image); err != nil {
		return nil, err
	}
	name := prefix + base + opt.Format.Ext()
	results = append(results, output{name, buf.Bytes()})
	row := map[string]any{
		"name": f.name, "output": name, "data": jsBytes(buf.Bytes()),
		"positive": out.Positive, "total": out.Total,
		"percent": process.Result{Positive: out.Positive, Total: out.Total}.Percent(),
	}
	if opt.Histogram {
		var h bytes.Buffer
		if err := out.Histogram.WriteTSV(&h); err != nil {
			return nil, err
		}
		hname := "data_" + base + ".tsv"
		results = append(results, output{hname, h.Bytes()})
		row["histogram"], row["histogramData"] = hname, jsBytes(h.Bytes())
	}
	return row, nil
}

// zipResults returns all files of the current batch as a zip archive.
func zipResults([]js.Value) (any, error) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, o := range results {
		w, err := zw.Create(o.name)
		if err != nil {
			return nil, err
		}
		if _, err := w.Write(o.data); err != nil {
			return nil, err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return jsBytes(buf.Bytes()), nil
}

func lookup(id js.Value) (*file, error) {
	f := files[id.String()]
	if f == nil {
		return nil, errors.New("unknown file")
	}
	return f, nil
}

// parseOptions reads conversion settings from a JS object with the fields
// mode, channel, min, gradient and optionally format and histogram.
func parseOptions(v js.Value) (process.Options, error) {
	var opt process.Options
	var err error
	if opt.Mode, err = process.ParseMode(v.Get("mode").String()); err != nil {
		return opt, err
	}
	if opt.Channel, err = process.ParseChannel(v.Get("channel").String()); err != nil {
		return opt, err
	}
	min := v.Get("min")
	if min.Type() != js.TypeNumber || min.Int() < 0 || min.Int() > 255 {
		return opt, fmt.Errorf("min must be 0-255")
	}
	opt.Min = uint8(min.Int())
	var ok bool
	if opt.Gradient, ok = gradient.ByName(v.Get("gradient").String()); !ok && opt.Mode == process.ModeRecolor {
		return opt, fmt.Errorf("unknown gradient %q", v.Get("gradient").String())
	}
	if format := v.Get("format"); format.Truthy() {
		if opt.Format, err = process.ParseFormat(format.String()); err != nil {
			return opt, err
		}
	}
	opt.Histogram = v.Get("histogram").Truthy()
	return opt, nil
}

// shrink scales img so its longest edge is at most previewSize, keeping
// 16-bit data 16-bit.
func shrink(img image.Image) image.Image {
	b := img.Bounds()
	scale := float64(previewSize) / float64(max(b.Dx(), b.Dy()))
	if scale >= 1 {
		return img
	}
	r := image.Rect(0, 0, max(1, int(float64(b.Dx())*scale)), max(1, int(float64(b.Dy())*scale)))
	var dst draw.Image = image.NewNRGBA(r)
	if process.Is16Bit(img) {
		dst = image.NewNRGBA64(r)
	}
	draw.ApproxBiLinear.Scale(dst, r, img, b, draw.Src, nil)
	return dst
}

func encodePNG(img image.Image) ([]byte, error) {
	var buf bytes.Buffer
	enc := png.Encoder{CompressionLevel: png.BestSpeed}
	err := enc.Encode(&buf, img)
	return buf.Bytes(), err
}

func goBytes(v js.Value) []byte {
	b := make([]byte, v.Get("length").Int())
	js.CopyBytesToGo(b, v)
	return b
}

func jsBytes(b []byte) js.Value {
	v := js.Global().Get("Uint8Array").New(len(b))
	js.CopyBytesToJS(v, b)
	return v
}
