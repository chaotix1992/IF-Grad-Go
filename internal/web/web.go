// Package web serves the local browser UI.
package web

import (
	"archive/zip"
	"bytes"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"image"
	"image/png"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"sync"

	"golang.org/x/image/draw"

	"if-grad-go/internal/gradient"
	"if-grad-go/internal/process"
)

//go:embed index.html
var indexHTML string

var indexTmpl = template.Must(template.New("index").Parse(indexHTML))

// previewSize is the longest edge of preview images in pixels.
const previewSize = 900

// file is an uploaded image.
type file struct {
	Name    string `json:"name"`
	Width   int    `json:"width"`
	Height  int    `json:"height"`
	path    string
	img     image.Image
	preview image.Image
}

type server struct {
	mu      sync.Mutex
	dir     string // uploads live in dir/<id>/<original name>
	files   map[string]*file
	nextID  int
	outDir  string
	results []process.Result // last conversion, for the zip download
}

// Serve starts the UI on addr and blocks. If open is set, the default
// browser is pointed at it.
func Serve(addr, outDir string, open bool) error {
	dir, err := os.MkdirTemp("", "ifgrad-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)

	s := &server{dir: dir, files: map[string]*file{}, outDir: outDir}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", s.index)
	mux.HandleFunc("POST /api/files", s.upload)
	mux.HandleFunc("DELETE /api/files/{id}", s.remove)
	mux.HandleFunc("GET /api/files/{id}/original", s.original)
	mux.HandleFunc("GET /api/files/{id}/preview", s.preview)
	mux.HandleFunc("GET /api/files/{id}/histogram", s.histogram)
	mux.HandleFunc("GET /api/gradients/{name}", s.gradientBar)
	mux.HandleFunc("POST /api/convert", s.convert)
	mux.HandleFunc("GET /api/download", s.download)

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	url := "http://" + ln.Addr().String() + "/"
	log.Printf("IF-Grad running at %s (Ctrl+C to quit)", url)
	if open {
		openBrowser(url)
	}
	return http.Serve(ln, mux)
}

func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	if err := cmd.Start(); err != nil {
		log.Printf("could not open browser: %v", err)
	}
}

func (s *server) index(w http.ResponseWriter, r *http.Request) {
	data := struct {
		Gradients []gradient.Gradient
		OutDir    string
	}{gradient.All, s.outDir}
	if err := indexTmpl.Execute(w, data); err != nil {
		log.Print(err)
	}
}

// upload stores the multipart "files" field and returns their ids.
func (s *server) upload(w http.ResponseWriter, r *http.Request) {
	mr, err := r.MultipartReader()
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	type entry struct {
		ID string `json:"id"`
		*file
		Error string `json:"error,omitempty"`
	}
	var added []entry
	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		name := filepath.Base(part.FileName())
		if part.FormName() != "files" || name == "." || name == "/" {
			continue
		}
		s.mu.Lock()
		s.nextID++
		id := strconv.Itoa(s.nextID)
		s.mu.Unlock()

		f, err := s.store(id, name, part)
		if err != nil {
			added = append(added, entry{file: &file{Name: name}, Error: err.Error()})
			continue
		}
		s.mu.Lock()
		s.files[id] = f
		s.mu.Unlock()
		added = append(added, entry{ID: id, file: f})
	}
	writeJSON(w, added)
}

func (s *server) store(id, name string, r io.Reader) (*file, error) {
	dir := filepath.Join(s.dir, id)
	if err := os.Mkdir(dir, 0o700); err != nil {
		return nil, err
	}
	path := filepath.Join(dir, name)
	out, err := os.Create(path)
	if err != nil {
		return nil, err
	}
	_, err = io.Copy(out, r)
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return nil, err
	}
	img, err := process.Load(path)
	if err != nil {
		log.Printf("upload %s: %v", name, err)
		os.RemoveAll(dir)
		return nil, errors.New("not a readable image")
	}
	b := img.Bounds()
	return &file{Name: name, Width: b.Dx(), Height: b.Dy(), path: path, img: img, preview: shrink(img)}, nil
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

func (s *server) lookup(w http.ResponseWriter, r *http.Request) *file {
	s.mu.Lock()
	f := s.files[r.PathValue("id")]
	s.mu.Unlock()
	if f == nil {
		http.NotFound(w, r)
	}
	return f
}

func (s *server) remove(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	s.mu.Lock()
	delete(s.files, id)
	s.mu.Unlock()
	os.RemoveAll(filepath.Join(s.dir, id))
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) original(w http.ResponseWriter, r *http.Request) {
	if f := s.lookup(w, r); f != nil {
		writePNG(w, f.preview)
	}
}

func (s *server) preview(w http.ResponseWriter, r *http.Request) {
	f := s.lookup(w, r)
	if f == nil {
		return
	}
	opt, err := parseOptions(r.URL.Query().Get)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writePNG(w, process.Convert(f.preview, opt).Image)
}

// histogram is computed on the full-resolution image so the UI can show
// exact mask percentages while the threshold slider moves.
func (s *server) histogram(w http.ResponseWriter, r *http.Request) {
	f := s.lookup(w, r)
	if f == nil {
		return
	}
	ch, err := process.ParseChannel(r.URL.Query().Get("channel"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, process.Intensity(f.img, ch).Histogram())
}

func (s *server) gradientBar(w http.ResponseWriter, r *http.Request) {
	g, ok := gradient.ByName(r.PathValue("name"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Cache-Control", "max-age=3600")
	writePNG(w, g.LUT.Bar(256, 1))
}

// parseOptions reads conversion settings from form or query values.
func parseOptions(get func(string) string) (process.Options, error) {
	var opt process.Options
	var err error
	if opt.Mode, err = process.ParseMode(get("mode")); err != nil {
		return opt, err
	}
	if opt.Channel, err = process.ParseChannel(get("channel")); err != nil {
		return opt, err
	}
	min, err := strconv.Atoi(get("min"))
	if err != nil || min < 0 || min > 255 {
		return opt, fmt.Errorf("min must be 0-255")
	}
	opt.Min = uint8(min)
	var ok bool
	if opt.Gradient, ok = gradient.ByName(get("gradient")); !ok && opt.Mode == process.ModeRecolor {
		return opt, fmt.Errorf("unknown gradient %q", get("gradient"))
	}
	return opt, nil
}

func (s *server) convert(w http.ResponseWriter, r *http.Request) {
	var req struct {
		IDs       []string `json:"ids"`
		Mode      string   `json:"mode"`
		Channel   string   `json:"channel"`
		Min       string   `json:"min"`
		Gradient  string   `json:"gradient"`
		Format    string   `json:"format"`
		Histogram bool     `json:"histogram"`
		OutDir    string   `json:"outDir"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	opt, err := parseOptions(func(k string) string {
		return map[string]string{"mode": req.Mode, "channel": req.Channel, "min": req.Min, "gradient": req.Gradient}[k]
	})
	if err == nil {
		opt.Format, err = process.ParseFormat(req.Format)
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	opt.Histogram = req.Histogram
	if opt.OutDir, err = filepath.Abs(req.OutDir); err != nil || req.OutDir == "" {
		http.Error(w, "invalid output folder", http.StatusBadRequest)
		return
	}

	var paths []string
	s.mu.Lock()
	for _, id := range req.IDs {
		if f := s.files[id]; f != nil {
			paths = append(paths, f.path)
		}
	}
	s.mu.Unlock()

	results, err := process.Run(paths, opt, nil)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.mu.Lock()
	s.results, s.outDir = results, opt.OutDir
	s.mu.Unlock()

	type row struct {
		Name      string  `json:"name"`
		Output    string  `json:"output,omitempty"`
		Histogram string  `json:"histogram,omitempty"`
		Positive  int     `json:"positive"`
		Total     int     `json:"total"`
		Percent   float64 `json:"percent"`
		Error     string  `json:"error,omitempty"`
	}
	rows := make([]row, len(results))
	for i, res := range results {
		rows[i] = row{Name: filepath.Base(res.Input), Output: res.Output, Histogram: res.HistogramFile,
			Positive: res.Positive, Total: res.Total, Percent: res.Percent()}
		if res.Err != nil {
			rows[i].Error = res.Err.Error()
		}
	}
	writeJSON(w, rows)
}

// download zips the files written by the last conversion.
func (s *server) download(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	results := s.results
	s.mu.Unlock()
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="ifgrad-results.zip"`)
	zw := zip.NewWriter(w)
	for _, res := range results {
		for _, p := range []string{res.Output, res.HistogramFile} {
			if p == "" || res.Err != nil {
				continue
			}
			if err := addToZip(zw, p); err != nil {
				log.Print(err)
			}
		}
	}
	if err := zw.Close(); err != nil {
		log.Print(err)
	}
}

func addToZip(zw *zip.Writer, path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	dst, err := zw.Create(filepath.Base(path))
	if err != nil {
		return err
	}
	_, err = io.Copy(dst, f)
	return err
}

func writePNG(w http.ResponseWriter, img image.Image) {
	var buf bytes.Buffer
	enc := png.Encoder{CompressionLevel: png.BestSpeed}
	if err := enc.Encode(&buf, img); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Write(buf.Bytes())
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Print(err)
	}
}
