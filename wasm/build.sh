#!/bin/sh
# Builds the browser version into wasm/web. Serve that folder with any
# static file server (or GitHub Pages).
set -e
cd "$(dirname "$0")"
GOOS=js GOARCH=wasm go build -trimpath -ldflags="-s -w" -o web/ifgrad.wasm .
# wasm_exec.js must come from the same Go version that built ifgrad.wasm.
cp "$(go env GOROOT)/lib/wasm/wasm_exec.js" web/
# Linked from the page's "How to cite" section.
cp ../CITATION.cff web/
