package reader

import (
	"bytes"
	"image"
	_ "image/png"
	"strings"
	"testing"
)

func TestMascotIsEmbeddedAndTransparent(t *testing.T) {
	b, err := assets.ReadFile("static/town-crier.png")
	if err != nil {
		t.Fatal(err)
	}
	img, format, err := image.Decode(bytes.NewReader(b))
	if err != nil || format != "png" {
		t.Fatalf("mascot PNG: %s %v", format, err)
	}
	bounds := img.Bounds()
	if bounds.Dx() < 256 || bounds.Dy() < 256 {
		t.Fatal("mascot source is too small")
	}
	visible := 0
	var painted image.Rectangle
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			_, _, _, alpha := img.At(x, y).RGBA()
			// Ignore edge noise below 1% opacity when measuring the figure.
			if alpha > 0x0202 {
				visible++
				pixel := image.Rect(x, y, x+1, y+1)
				if painted.Empty() {
					painted = pixel
				} else {
					painted = painted.Union(pixel)
				}
			}
		}
	}
	area := bounds.Dx() * bounds.Dy()
	if visible < area/20 || visible > area*19/20 {
		t.Fatal("mascot must contain a visible character and a transparent background")
	}
	if painted.Min.X <= bounds.Min.X || painted.Min.Y <= bounds.Min.Y || painted.Max.X >= bounds.Max.X || painted.Max.Y >= bounds.Max.Y {
		t.Fatal("mascot needs transparent padding on every edge so the figure is not cropped")
	}
}
func TestComfylibStylesAndDemoBanner(t *testing.T) {
	h := webHarness(t)
	h.app.config.Demo = true
	w := h.request(0, "GET", "/login", nil)
	if w.Code != 200 {
		t.Fatal(w.Body)
	}
	if !bytes.Contains(w.Body.Bytes(), []byte("Disposable demo")) {
		t.Fatal("demo banner missing")
	}
	if !bytes.Contains(w.Body.Bytes(), []byte("/static/comfy/accounts.css")) {
		t.Fatal("shared styles missing")
	}
	if !bytes.Contains(w.Body.Bytes(), []byte("account-page")) {
		t.Fatal("shared account layout not used")
	}
	w = h.request(0, "GET", "/static/comfy/accounts.css", nil)
	if w.Code != 200 || !bytes.Contains(w.Body.Bytes(), []byte(".account-page")) {
		t.Fatal("shared asset route failed")
	}
	h.app.config.Demo = false
	w = h.request(0, "GET", "/login", nil)
	if bytes.Contains(w.Body.Bytes(), []byte("demo-password")) {
		t.Fatal("demo credentials appear in a normal instance")
	}
}

func TestHeaderUsesTownCrier(t *testing.T) {
	h := webHarness(t)
	for _, path := range []string{"/", "/account", "/sources", "/groups"} {
		body := h.request(2, "GET", path, nil).Body.String()
		if !strings.Contains(body, `<img class="brand-mark" src="/static/town-crier.png"`) || strings.Contains(body, `<svg class="brand-mark"`) {
			t.Fatal("header must use the town crier", path)
		}
	}
}
