# Favicon artwork

The favicon uses a close-up of Tidings' existing town crier. The SVG embeds the
small transparent mascot from `docs/images/town-crier.png` and frames his face
and hat for browser tabs. It contains no external image requests. The app header
uses the full embedded mascot in place of the former paper mark.

`internal/reader/static/favicon.svg` is the editable framing source. Raster sizes are
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

Every HTML page declares the SVG, ICO fallback, and touch icon with a mascot
cache revision, so browsers can replace the former bell. `/favicon.ico`
also handles browsers' automatic requests, including before sign-in. This exact
public path uses the same embedded asset as `/static/favicon.ico`; private
routes retain their normal access checks.

Tests validate the embedded mascot, SVG structure, raster dimensions, ICO frame bounds, public GET
and HEAD responses, read-only methods, page declarations, and nearby paths that
must remain private. Browser checks render the SVG to a canvas with the site's normal
content security policy.
