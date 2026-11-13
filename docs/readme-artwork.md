# README artwork

`images/town-crier.png` is a smaller copy of
`internal/reader/static/town-crier.png`, made at commit `9a6ab00`. The original
artwork and its [generation prompt](town-crier-prompt.txt) are unchanged.

Regenerate it with:

```sh
magick internal/reader/static/town-crier.png -resize 256x -strip -colors 256 \
  -define png:compression-level=9 docs/images/town-crier.png
```

The README shows the 256 × 270 image at 144 × 152. Keep the file under
100,000 bytes.
