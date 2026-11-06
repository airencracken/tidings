# README artwork

`images/town-crier.png` is a small transparent derivative of the embedded
`internal/reader/static/town-crier.png` mascot at commit `9a6ab00`. The original
artwork and [generation prompt](town-crier-prompt.txt) remain unchanged.

Regenerate with:

```sh
magick internal/reader/static/town-crier.png -resize 256x -strip -colors 256 \
  -define png:compression-level=9 docs/images/town-crier.png
```

The 256 × 270 image is displayed at 144 × 152 in the GitHub README. The project
presentation checks keep this download under 100,000 bytes and check the
website, issue, and existing Ko-fi sponsorship links.
