package main

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

func TestDemoLifecycleIsolationAndHTTPRoutes(t *testing.T) {
	production := filepath.Join(t.TempDir(), "reader.db")
	sentinel := []byte("leave your real reading table alone")
	if err := os.WriteFile(production, sentinel, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TIDINGS_DATA_DIR", filepath.Dir(production))
	t.Setenv("TIDINGS_ADDR", "0.0.0.0:1")
	t.Setenv("TIDINGS_BASE_URL", "https://production.example")
	t.Setenv("TIDINGS_SECURE_COOKIES", "true")
	temporary := t.TempDir()
	t.Setenv("TMPDIR", temporary)
	input, output := io.Pipe()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { err := run(ctx, 0, output); output.CloseWithError(err); done <- err }()
	t.Cleanup(func() {
		cancel()
		input.Close()
		select {
		case err := <-done:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(10 * time.Second):
			t.Error("demo did not stop")
		}
		if b, err := os.ReadFile(production); err != nil || string(b) != string(sentinel) {
			t.Fatalf("real data changed: %q %v", b, err)
		}
		if entries, err := os.ReadDir(temporary); err != nil || len(entries) != 0 {
			t.Errorf("demo temp data not removed: %v %v", entries, err)
		}
	})
	ready := make(chan string, 1)
	go func() {
		scanner := bufio.NewScanner(input)
		base := ""
		for scanner.Scan() {
			if strings.HasPrefix(scanner.Text(), "Open ") {
				base = strings.TrimPrefix(scanner.Text(), "Open ")
			}
			if strings.HasPrefix(scanner.Text(), "Press Ctrl-C") {
				ready <- base
				io.Copy(io.Discard, input)
				return
			}
		}
		ready <- ""
	}()
	var base string
	select {
	case base = <-ready:
	case <-time.After(15 * time.Second):
		t.Fatal("demo did not become ready")
	}
	if !strings.HasPrefix(base, "http://127.0.0.1:") {
		t.Fatalf("demo must listen only on loopback: %s", base)
	}
	for _, tc := range []struct {
		name         string
		owner, optin bool
	}{{"demo", true, true}, {"freya", false, true}, {"jules", false, false}} {
		t.Run(tc.name, func(t *testing.T) {
			jar, err := cookiejar.New(nil)
			if err != nil {
				t.Fatal(err)
			}
			transport := http.DefaultTransport.(*http.Transport).Clone()
			transport.DisableKeepAlives = true
			client := &http.Client{Jar: jar, Transport: transport, Timeout: 5 * time.Second}
			t.Cleanup(client.CloseIdleConnections)
			body := get(t, client, base+"/login", 200)
			if !strings.Contains(body, "Disposable demo") || !strings.Contains(body, "town-crier.png") {
				t.Fatal("demo banner or mascot missing")
			}
			csrf := formCSRF(t, body)
			res, err := client.PostForm(base+"/login", url.Values{"username": {tc.name}, "password": {demoPassword}, "csrf": {csrf}})
			if err != nil {
				t.Fatal(err)
			}
			res.Body.Close()
			if res.StatusCode != 200 || res.Request.URL.Path != "/" {
				t.Fatalf("sample login failed: %d %s", res.StatusCode, res.Request.URL)
			}
			for _, route := range []string{"/", "/groups", "/sources", "/account", "/subscriptions.opml", "/api/v1/export", "/static/town-crier.png", "/static/comfy/accounts.css"} {
				get(t, client, base+route, 200)
			}
			body = get(t, client, base+"/", 200)
			if strings.Contains(body, "The village sheet") != tc.optin {
				t.Fatal("household default does not match the sample account")
			}
			// Owner tools live on Admin; members are refused there.
			if tc.owner {
				if body = get(t, client, base+"/admin", 200); !strings.Contains(body, "Create and manage invitations") {
					t.Fatal("demo owner has no admin tools")
				}
			} else {
				get(t, client, base+"/admin", 403)
			}
			body = get(t, client, base+"/?view=group", 200)
			if strings.Contains(body, "Sample: good sources for a shared reading table") != tc.optin {
				t.Fatal("demo household route bypassed opt-in")
			}
			body = get(t, client, base+"/", 200)
			res, err = client.PostForm(base+"/refresh", url.Values{"csrf": {formCSRF(t, body)}})
			if err != nil {
				t.Fatal(err)
			}
			res.Body.Close()
			if res.StatusCode != 200 {
				t.Fatal("demo refresh action failed")
			}
		})
	}
}
func formCSRF(t *testing.T, s string) string {
	t.Helper()
	m := regexp.MustCompile(`name="csrf" value="([a-f0-9]+)"`).FindStringSubmatch(s)
	if len(m) != 2 {
		t.Fatal("missing CSRF token")
	}
	return m[1]
}
func get(t *testing.T, c *http.Client, address string, status int) string {
	t.Helper()
	res, err := c.Get(address)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	b, err := io.ReadAll(res.Body)
	if err != nil || res.StatusCode != status {
		t.Fatalf("GET %s status %d err %v", address, res.StatusCode, err)
	}
	return string(b)
}

type failedWriter struct{}

func (failedWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }
func TestDemoStartupFailuresCleanUp(t *testing.T) {
	temporary := t.TempDir()
	t.Setenv("TMPDIR", temporary)
	for _, port := range []int{-1, 65536} {
		if err := run(context.Background(), port, io.Discard); err == nil {
			t.Fatal("invalid port accepted")
		}
	}
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	if err = run(context.Background(), listener.Addr().(*net.TCPAddr).Port, io.Discard); err == nil || !strings.Contains(err.Error(), "PORT=9000") {
		t.Fatalf("occupied port not reported: %v", err)
	}
	if err = run(context.Background(), 0, failedWriter{}); !errors.Is(err, io.ErrClosedPipe) {
		t.Fatalf("output failure was ignored: %v", err)
	}
	if entries, err := os.ReadDir(temporary); err != nil || len(entries) != 0 {
		t.Fatalf("failed startup left data: %v %v", entries, err)
	}
}
