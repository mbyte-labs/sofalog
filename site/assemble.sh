#!/usr/bin/env sh
# Assembles the GitHub Pages landing site into the directory named by $1.
# The one definition of what gets published: the page in site/, the README's
# screenshots and logo from docs/ (never duplicated into site/), and the PWA
# icons as the favicon. Used by .github/workflows/pages.yml and for a local
# preview alike.
set -eu

out=${1:?usage: site/assemble.sh <output-dir>}
root=$(cd "$(dirname "$0")/.." && pwd)

mkdir -p "$out/screenshots" "$out/icons"
cp "$root"/site/index.html "$root"/site/site.css "$out"/
mkdir -p "$out/privacy"
cp "$root"/site/privacy/index.html "$out/privacy/"
cp "$root"/docs/screenshots/*.jpg "$out/screenshots/"
cp "$root"/docs/logo.svg "$out/logo.svg"
cp "$root"/frontend/public/icons/icon-192.png "$root"/frontend/public/icons/apple-touch-icon.png "$out/icons/"
# Pages runs Jekyll over the artifact unless told not to; the page is plain HTML.
: > "$out/.nojekyll"
