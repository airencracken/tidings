package reader

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"encoding/xml"
	"fmt"
	"image"
	_ "image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"golang.org/x/net/html"
)

func TestFaviconAssets(t *testing.T) {
	for _, size := range []int{16, 32, 48, 180} {
		name := fmt.Sprintf("static/favicon-%d.png", size)
		if size == 180 {
			name = "static/apple-touch-icon.png"
		}
		b, err := assets.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		img, format, err := image.Decode(bytes.NewReader(b))
		if err != nil || format != "png" {
			t.Fatalf("%s is not a PNG: %v", name, err)
		}
		if img.Bounds().Dx() != size || img.Bounds().Dy() != size {
			t.Fatalf("%s has incorrect dimensions: %v", name, img.Bounds())
		}
		_, _, _, alpha := img.At(0, 0).RGBA()
		if alpha != 0 {
			t.Errorf("%s corners must be transparent", name)
		}
		skin, green := 0, 0
		for y := 0; y < size; y++ {
			for x := 0; x < size; x++ {
				r, g, b, a := img.At(x, y).RGBA()
				if a < 0xe000 {
					continue
				}
				if r > g+12*257 && g > b+8*257 && r > b+40*257 {
					skin++
				}
				if g+10*257 >= r && g > b+15*257 && r < 190*257 && g < 190*257 {
					green++
				}
			}
		}
		if skin < size*size/30 || green < size*size/30 {
			t.Errorf("%s must depict the town crier's face and green hat", name)
		}
	}
	b, err := assets.ReadFile("static/favicon.svg")
	if err != nil {
		t.Fatal(err)
	}
	decoder := xml.NewDecoder(bytes.NewReader(b))
	root := false
	portraits := 0
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		switch element := token.(type) {
		case xml.Directive:
			t.Fatal("favicon cannot contain XML directives")
		case xml.StartElement:
			if !root {
				root = true
				if element.Name.Local != "svg" || element.Name.Space != "http://www.w3.org/2000/svg" {
					t.Fatal("invalid SVG root")
				}
			}
			if element.Name.Local == "script" || element.Name.Local == "foreignObject" {
				t.Fatal("favicon must be self-contained artwork")
			}
			for _, attribute := range element.Attr {
				if strings.HasPrefix(attribute.Name.Local, "on") {
					t.Fatal("active SVG content")
				}
				if attribute.Name.Local == "href" {
					if element.Name.Local != "image" || !strings.HasPrefix(attribute.Value, "data:image/png;base64,") {
						t.Fatal("external SVG content")
					}
					source, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(attribute.Value, "data:image/png;base64,"))
					if err != nil {
						t.Fatal(err)
					}
					mascot, err := os.ReadFile("../../docs/images/town-crier.png")
					if err != nil || !bytes.Equal(source, mascot) {
						t.Fatal("favicon must reuse the established mascot", err)
					}
					portraits++
				}
			}
		}
	}
	if portraits != 1 {
		t.Fatal("favicon must contain one mascot portrait")
	}
}

func TestFaviconICOContract(t *testing.T) {
	b, err := assets.ReadFile("static/favicon.ico")
	if err != nil {
		t.Fatal(err)
	}
	if len(b) < 6 || binary.LittleEndian.Uint16(b[:2]) != 0 || binary.LittleEndian.Uint16(b[2:4]) != 1 {
		t.Fatal("invalid ICO header")
	}
	count := int(binary.LittleEndian.Uint16(b[4:6]))
	if count != 3 || len(b) < 6+16*count {
		t.Fatal("ICO must contain 16, 32, and 48 pixel frames")
	}
	seen := map[int]bool{}
	lastEnd := uint64(6 + 16*count)
	for i := range count {
		entry := b[6+16*i : 6+16*(i+1)]
		width, height := int(entry[0]), int(entry[1])
		if width != height || (width != 16 && width != 32 && width != 48) || seen[width] {
			t.Fatalf("invalid ICO frame %dx%d", width, height)
		}
		seen[width] = true
		size := uint64(binary.LittleEndian.Uint32(entry[8:12]))
		offset := uint64(binary.LittleEndian.Uint32(entry[12:16]))
		if size == 0 || offset < lastEnd || offset+size > uint64(len(b)) {
			t.Fatal("ICO frames overlap or exceed file bounds")
		}
		lastEnd = offset + size
	}
}

func TestFaviconRoutesArePublicAndReadOnly(t *testing.T) {
	h := webHarness(t)
	for _, tc := range []struct{ path, mime string }{{"/favicon.ico", "image/x-icon"}, {"/static/favicon.svg", "image/svg+xml"}, {"/static/favicon-16.png", "image/png"}, {"/static/favicon-32.png", "image/png"}, {"/static/favicon-48.png", "image/png"}, {"/static/apple-touch-icon.png", "image/png"}} {
		for _, uid := range []int64{0, 2} {
			w := h.request(uid, "GET", tc.path, nil)
			if w.Code != 200 || !strings.HasPrefix(w.Header().Get("Content-Type"), tc.mime) || w.Body.Len() == 0 {
				t.Fatalf("icon request %s user %d: %d %q", tc.path, uid, w.Code, w.Header().Get("Content-Type"))
			}
			if w.Header().Get("Location") != "" || len(w.Result().Cookies()) > 0 {
				t.Fatal("icon request must not redirect or start a session")
			}
		}
	}
	for _, method := range []string{"POST", "PUT", "DELETE"} {
		w := h.request(0, method, "/favicon.ico", nil)
		if w.Code != 405 {
			t.Errorf("%s favicon accepted: %d", method, w.Code)
		}
	}
	for _, path := range []string{"/favicon.ico/private", "/favicon.ico/../account", "/favicon.ico.bak"} {
		w := h.request(0, "GET", path, nil)
		if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/login" {
			t.Fatalf("favicon public exception covers another path: %s", path)
		}
	}
	if err := h.store.Close(); err != nil {
		t.Fatal(err)
	}
	w := h.request(0, "GET", "/favicon.ico", nil)
	if w.Code != 200 {
		t.Fatal("favicon requires database availability")
	}
	r := httptest.NewRequest("HEAD", "/favicon.ico", nil)
	head := httptest.NewRecorder()
	h.app.ServeHTTP(head, r)
	if head.Code != http.StatusOK || head.Body.Len() != 0 || head.Header().Get("Content-Length") == "" {
		t.Fatal("HEAD favicon response is incorrect")
	}
}

func TestEveryPageDeclaresFavicons(t *testing.T) {
	h := webHarness(t)
	for _, tc := range []struct {
		uid  int64
		path string
	}{{0, "/login"}, {0, "/join?code=example"}, {2, "/"}, {2, "/sources"}, {2, "/groups"}, {2, "/account"}} {
		w := h.request(tc.uid, "GET", tc.path, nil)
		if w.Code != 200 {
			t.Fatal(w.Body)
		}
		doc, err := html.Parse(strings.NewReader(w.Body.String()))
		if err != nil {
			t.Fatal(err)
		}
		icons := map[string]string{}
		var walk func(*html.Node)
		walk = func(n *html.Node) {
			if n.Type == html.ElementNode && n.Data == "link" {
				attrs := map[string]string{}
				for _, a := range n.Attr {
					attrs[a.Key] = a.Val
				}
				if attrs["rel"] == "icon" || attrs["rel"] == "apple-touch-icon" {
					link, err := url.Parse(attrs["href"])
					if err != nil {
						t.Fatal(err)
					}
					if link.RawQuery != "v=town-crier" {
						t.Fatal("new mascot icons need a cache revision", link)
					}
					icons[link.Path] = attrs["rel"]
				}
			}
			for child := n.FirstChild; child != nil; child = child.NextSibling {
				walk(child)
			}
		}
		walk(doc)
		for _, path := range []string{"/favicon.ico", "/static/favicon.svg", "/static/apple-touch-icon.png"} {
			if icons[path] == "" {
				t.Errorf("page %s is missing %s", tc.path, path)
			}
		}
	}
}
