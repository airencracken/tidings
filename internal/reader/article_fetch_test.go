package reader

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
)

func TestArticleTLSCompatibilityAndVerification(t *testing.T) {
	var mu sync.Mutex
	challenged := map[string]bool{}
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		blocked := challenged[r.RemoteAddr]
		mu.Unlock()
		if blocked {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		_, _ = io.WriteString(w, fullArticleFixture())
	}))
	server.EnableHTTP2 = true
	server.TLS = &tls.Config{GetConfigForClient: func(hello *tls.ClientHelloInfo) (*tls.Config, error) {
		// Reproduce the publisher's observed challenge for the newly advertised
		// Go 1.26 groups, while requiring the deployed hybrid exchange to remain.
		blocked := slices.Contains(hello.SupportedCurves, tls.SecP256r1MLKEM768) || slices.Contains(hello.SupportedCurves, tls.SecP384r1MLKEM1024) || !slices.Contains(hello.SupportedCurves, tls.X25519MLKEM768)
		mu.Lock()
		challenged[hello.Conn.RemoteAddr().String()] = blocked
		mu.Unlock()
		return nil, nil
	}}
	server.StartTLS()
	defer server.Close()
	newClient := func(trust bool) *Fetcher {
		f := NewFetcher()
		transport := f.Client.Transport.(*http.Transport)
		transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "tcp", server.Listener.Addr().String())
		}
		if trust {
			transport.TLSClientConfig.RootCAs = x509.NewCertPool()
			transport.TLSClientConfig.RootCAs.AddCert(server.Certificate())
		}
		t.Cleanup(f.Client.CloseIdleConnections)
		return f
	}
	// Confirm that this fixture exposes the failure seen on the public article.
	old := newClient(true)
	old.Client.Transport.(*http.Transport).TLSClientConfig.CurvePreferences = nil
	if _, _, err := old.Article(t.Context(), "https://example.com/article"); err == nil {
		t.Fatal("fixture did not reject the former default profile")
	}
	fixed := newClient(true)
	body, _, err := fixed.Article(t.Context(), "https://example.com/article")
	if err != nil || !strings.Contains(body, "original article") {
		t.Fatal("compatible TLS request could not fetch article", body, err)
	}
	// Compatibility must never turn off certificate validation.
	untrusted := newClient(false)
	if _, _, err = untrusted.Article(t.Context(), "https://example.com/article"); err == nil {
		t.Fatal("untrusted publisher certificate accepted")
	}
}

func TestArticleContentNegotiation(t *testing.T) {
	f := &Fetcher{Client: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		status := http.StatusOK
		body := fullArticleFixture()
		// This reproduces the 406 from the SANS article with the old narrow Accept.
		if !strings.Contains(r.Header.Get("Accept"), "*/*") {
			status = http.StatusNotAcceptable
			body = "Not acceptable"
		}
		return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {"text/html; charset=utf-8"}}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})}}
	body, _, err := f.Article(t.Context(), "https://example.org/article")
	if err != nil || !strings.Contains(body, "original article") {
		t.Fatal(body, err)
	}
}

func TestFullArticleFailuresAreSpecificAndPrivate(t *testing.T) {
	for _, tc := range []struct {
		status int
		want   string
	}{
		{401, "requires sign-in"}, {403, "refused this fetch (HTTP 403)"}, {404, "could not find this article"}, {406, "returned HTTP 406"}, {429, "limiting requests"}, {503, "returned HTTP 503"},
	} {
		t.Run(fmt.Sprint(tc.status), func(t *testing.T) {
			h := webHarness(t)
			f := testSource(t, h.store, 2, "failure", false)
			id := entryID(t, h.store, f)
			if _, err := h.store.DB.Exec("UPDATE entries SET url=? WHERE id=?", "https://example.org/article?signature=topsecret", id); err != nil {
				t.Fatal(err)
			}
			var log bytes.Buffer
			previous := slog.Default()
			slog.SetDefault(slog.New(slog.NewJSONHandler(&log, nil)))
			defer slog.SetDefault(previous)
			h.app.Fetcher = &Fetcher{Client: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: tc.status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("private publisher response")), Request: r}, nil
			})}}
			before, err := h.store.Entry(t.Context(), 2, id)
			if err != nil {
				t.Fatal(err)
			}
			w := h.request(2, "POST", fmt.Sprintf("/story/%d/fetch", id), nil)
			if w.Code != 502 || !strings.Contains(w.Body.String(), tc.want) {
				t.Fatal(w.Code, w.Body.String())
			}
			if !strings.Contains(log.String(), fmt.Sprintf(`"upstream_status":%d`, tc.status)) || !strings.Contains(log.String(), `"publisher":"example.org"`) {
				t.Fatal("failure missing from server diagnostics", log.String())
			}
			after, err := h.store.Entry(t.Context(), 2, id)
			if err != nil || after.Body != before.Body || after.FullStory {
				t.Fatal("failure changed story", after, err)
			}
			if strings.Contains(w.Body.String(), "private publisher response") || strings.Contains(log.String(), "private publisher response") || strings.Contains(log.String(), "topsecret") {
				t.Fatal("publisher error body was exposed")
			}
		})
	}
	for _, tc := range []struct {
		err          error
		want, reason string
	}{
		{context.DeadlineExceeded, "too long", "timeout"}, {errArticleType, "file instead", "content-type"}, {errArticleTooLarge, "too large", "size-limit"}, {errArticleEmpty, "story text", "extraction"}, {fmt.Errorf("%w: bad markup", errArticleParse), "story text", "extraction"}, {errors.New("https://example.org/article?signature=secret"), "Could not connect", "connection"},
	} {
		message, reason, _ := articleFailure(tc.err)
		if !strings.Contains(message, tc.want) || reason != tc.reason || strings.Contains(message, "secret") {
			t.Fatal(message, reason)
		}
	}
}
