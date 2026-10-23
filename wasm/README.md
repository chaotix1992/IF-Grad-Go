# IF-Grad in the browser

The same conversion code as the desktop program, compiled to WebAssembly so it can be hosted
as a static site (for example GitHub Pages). Images are processed in the visitor's browser and
are never uploaded.

- `main.go` exposes the functions from `internal/process` and `internal/gradient` to JavaScript.
- `web/worker.js` runs the WebAssembly in a web worker, so the page stays responsive during
  conversions.
- `web/index.html` is the UI. It matches the desktop UI, except that there is no output folder:
  results are downloaded one by one or as a zip.

## Build and run locally

```sh
./wasm/build.sh                          # writes web/ifgrad.wasm and web/wasm_exec.js
python3 -m http.server -d wasm/web       # then open http://localhost:8000
```

The page has to be served over HTTP; opening `index.html` as a file does not work.

## Deploy

GitHub Pages serves the `gh-pages` branch, which holds only the built site. It is checked out as a
worktree next to the repository. To set this up once:

```sh
git worktree add --orphan -b gh-pages ../ifgrad-pages
```

Then, under **Settings → Pages → Build and deployment**, choose **Deploy from a branch**, branch
`gh-pages`, folder `/ (root)`.

To publish changes, run:

```sh
./wasm/deploy.sh
```

It builds the site, copies it into `../ifgrad-pages`, commits and pushes `gh-pages`. The site
updates a minute or two later. Pushing `main` alone does not change the site.

The citation shown on the page is written out in `web/index.html`. Update it there when the
version in `CITATION.cff` changes.
