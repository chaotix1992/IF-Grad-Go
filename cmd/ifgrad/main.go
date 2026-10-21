// Command ifgrad recolours fluorescence images onto a colour gradient or
// turns them into threshold masks. Without -cli it opens a browser UI.
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"if-grad-go/internal/gradient"
	"if-grad-go/internal/process"
	"if-grad-go/internal/web"
)

func main() {
	cli := flag.Bool("cli", false, "convert the given files/folders instead of starting the UI")
	mode := flag.String("mode", "recolor", "recolor or mask")
	channel := flag.String("channel", "g", "intensity channel: r, g, b or gray")
	min := flag.Int("min", 0, "recolor: minimum intensity (0-255); mask: threshold")
	grad := flag.String("gradient", "classic", "gradient: classic, turbo, viridis or gray")
	format := flag.String("format", "tiff", "output format: tiff or png")
	hist := flag.Bool("hist", false, "also write a histogram .tsv per image")
	out := flag.String("o", "Conversions", "output folder")
	addr := flag.String("addr", "127.0.0.1:0", "UI listen address")
	noBrowser := flag.Bool("no-browser", false, "do not open the browser automatically")
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), "Usage:\n  ifgrad [flags]                      start the browser UI\n  ifgrad -cli [flags] files/folders   convert directly\n\nFlags:\n")
		flag.PrintDefaults()
	}
	flag.Parse()
	log.SetFlags(0)

	if !*cli {
		if err := web.Serve(*addr, *out, !*noBrowser); err != nil {
			log.Fatal(err)
		}
		return
	}

	opt, err := options(*mode, *channel, *min, *grad, *format)
	if err != nil {
		log.Fatal(err)
	}
	opt.Histogram, opt.OutDir = *hist, *out
	paths, err := process.ExpandPaths(flag.Args())
	if err != nil {
		log.Fatal(err)
	}
	if len(paths) == 0 {
		flag.Usage()
		os.Exit(2)
	}

	start := time.Now()
	failed := 0
	_, err = process.Run(paths, opt, func(r process.Result) {
		switch {
		case r.Err != nil:
			failed++
			log.Printf("FAIL %s: %v", r.Input, r.Err)
		case opt.Mode == process.ModeMask:
			log.Printf("ok   %s -> %s  positive %d of %d (%.2f%%)", r.Input, r.Output, r.Positive, r.Total, r.Percent())
		default:
			log.Printf("ok   %s -> %s", r.Input, r.Output)
		}
	})
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("Finished %d file(s) in %s", len(paths), time.Since(start).Round(time.Millisecond))
	if failed > 0 {
		os.Exit(1)
	}
}

func options(mode, channel string, min int, grad, format string) (process.Options, error) {
	var opt process.Options
	var err error
	if opt.Mode, err = process.ParseMode(mode); err != nil {
		return opt, err
	}
	if opt.Channel, err = process.ParseChannel(channel); err != nil {
		return opt, err
	}
	if min < 0 || min > 255 {
		return opt, fmt.Errorf("-min must be 0-255, got %d", min)
	}
	opt.Min = uint8(min)
	var ok bool
	if opt.Gradient, ok = gradient.ByName(grad); !ok {
		return opt, fmt.Errorf("unknown gradient %q", grad)
	}
	opt.Format, err = process.ParseFormat(format)
	return opt, err
}
