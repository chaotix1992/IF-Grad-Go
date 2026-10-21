# IF-Grad (Go)

Converts immunofluorescence images by mapping the intensity of one channel onto a colour
gradient, or by thresholding it into a black/white mask. This is a Go rewrite of
`recolor_rb.py` and `mask.py` as a single program with a browser UI and a CLI mode.

## Build

Requires Go 1.26 or newer. The only dependency besides the standard library is
`golang.org/x/image`, which provides TIFF support and preview scaling.

```sh
go build ./cmd/ifgrad                    # ifgrad (Linux/macOS)
GOOS=windows go build ./cmd/ifgrad       # ifgrad.exe
```

## Browser UI

Run `ifgrad` (or double-click it). It starts a local server on 127.0.0.1 and opens your
browser. Nothing leaves your computer.

- Drop images in (PNG, JPEG, GIF, TIFF; 8 or 16 bit).
- Pick the mode (Recolor or Mask), the channel and the minimum intensity / threshold.
  The preview and histogram update live. In mask mode the positive-pixel percentage is shown
  while you move the slider.
- Choose an output folder and format, then click **Convert all**. The results are written to
  the folder and can also be downloaded as a zip.

Flags: `-o` sets the default output folder, `-addr` the listen address and `-no-browser` stops it
from opening a browser automatically.

## CLI

```sh
ifgrad -cli -channel g -min 20 -hist -o Conversions images/
ifgrad -cli -mode mask -channel r -min 100 -format png -o Masks a.tif b.tif
```

Folders are expanded to the images they contain (not recursively). Run `ifgrad -h` for all flags.

## Gradients

| Name      | Description |
|-----------|-------------|
| `classic` | The original IF-Grad gradient: black → blue ramp (64 steps), then matplotlib's `gist_rainbow` reversed from blue to red. Identical to the Python version, entry for entry. |
| `turbo`   | Google's Turbo: same blue → green → yellow → red order, with smoother lightness. |
| `viridis` | Perceptually uniform and colour-blind friendly (9-point approximation). |
| `gray`    | Plain grayscale. |

## Behaviour compared to the Python scripts

Recolouring and masking produce the same pixels as the Python scripts for 8-bit input. The test
references in `testdata/` were generated with the original code. These differences are intentional:

- Each histogram file (`data_<name>.tsv`, columns `value` and `count`) counts only its own image.
  The old script added every earlier image's counts into each file.
- Output files get the correct extension (`.tif` / `.png`). Recoloured images are stored losslessly as paletted images.
- Folders only pick up image files; output folders are created automatically.
- 16-bit images keep their full precision. Before, they were reduced to 8 bit first.
- A minimum intensity of 255 no longer crashes. Saturated pixels get the top colour.
- Masks are written as 8-bit grayscale instead of RGB.

## Tests

```sh
go test ./...
```

## AI Disclaimer
This project has been built with the help of AI to reformat the original project [IF-Grad](https://github.com/chaotix1992/IF-Grad). The logic this project follows is the same as the original one, with an added GUI and executable files instead of CLI for easier usability.
