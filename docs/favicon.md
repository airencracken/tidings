# Favicon artwork

The favicon is a hand-drawn SVG bell in the existing Tidings palette:
`#fffef9` on `#355c48`. The simple silhouette is intended for 16-pixel browser
tabs; the town crier remains the larger illustrated mascot.

`internal/reader/static/favicon.svg` is the editable source. Raster sizes are
checked in, so building or running Tidings requires no graphics tools. To
regenerate them, install librsvg's `rsvg-convert` and ImageMagick, then run from
the repository root:

```sh
rsvg-convert --width 16 --height 16 --output internal/reader/static/favicon-16.png internal/reader/static/favicon.svg
rsvg-convert --width 32 --height 32 --output internal/reader/static/favicon-32.png internal/reader/static/favicon.svg
rsvg-convert --width 48 --height 48 --output internal/reader/static/favicon-48.png internal/reader/static/favicon.svg
rsvg-convert --width 180 --height 180 --output internal/reader/static/apple-touch-icon.png internal/reader/static/favicon.svg
magick internal/reader/static/favicon-16.png internal/reader/static/favicon-32.png internal/reader/static/favicon-48.png internal/reader/static/favicon.ico
```

Every HTML page declares the SVG, ICO fallback, and touch icon. `/favicon.ico`
also handles browsers' automatic requests, including before sign-in. This exact
public path uses the same embedded asset as `/static/favicon.ico`; private
routes retain their normal access checks.

Tests validate SVG structure, raster dimensions, ICO frame bounds, public GET
and HEAD responses, read-only methods, page declarations, and nearby paths that
must remain private. Browser checks decode the SVG with the site's normal
content security policy.
