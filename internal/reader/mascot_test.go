package reader

import (
	"bytes"
	"image"
	_ "image/png"
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
	_, _, _, alpha := img.At(bounds.Min.X, bounds.Min.Y).RGBA()
	if alpha != 0 {
		t.Fatal("mascot needs a transparent background")
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
