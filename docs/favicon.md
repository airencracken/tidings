# Favicon artwork

The favicon is a close-up of the town crier from `docs/images/town-crier.png`.
`internal/reader/static/favicon.svg` is the source. The raster sizes are checked
in, so building Tidings needs no graphics tools.

To regenerate them, install librsvg's `rsvg-convert` and ImageMagick, then run
from the repository root:

```sh
rsvg-convert --width 16 --height 16 --output internal/reader/static/favicon-16.png internal/reader/static/favicon.svg
rsvg-convert --width 32 --height 32 --output internal/reader/static/favicon-32.png internal/reader/static/favicon.svg
rsvg-convert --width 48 --height 48 --output internal/reader/static/favicon-48.png internal/reader/static/favicon.svg
rsvg-convert --width 180 --height 180 --output internal/reader/static/apple-touch-icon.png internal/reader/static/favicon.svg
magick internal/reader/static/favicon-16.png internal/reader/static/favicon-32.png internal/reader/static/favicon-48.png internal/reader/static/favicon.ico
```

Pages link the icons with a `?v=town-crier` suffix so browsers drop the old bell.
Change the suffix when the artwork changes.
