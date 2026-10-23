#!/bin/sh
# Builds the site and publishes it to the gh-pages branch, which GitHub Pages
# serves. Expects the gh-pages worktree next to the repository, at
# ../ifgrad-pages (see README.md for how to create it).
set -e
cd "$(dirname "$0")"
pages=../../ifgrad-pages
if [ ! -d "$pages" ]; then
	echo "gh-pages worktree not found at $pages" >&2
	exit 1
fi
rev=$(git rev-parse --short HEAD)
./build.sh
cp web/* "$pages"/
cd "$pages"
git add -A
if git diff --cached --quiet; then
	echo "Nothing changed, nothing to deploy."
	exit 0
fi
git commit -m "Deploy $rev"
git push origin gh-pages
